// Package vpn runs a WireGuard tunnel for tethered devices' traffic only.
//
// The admin pastes a standard wg-quick config. Batter brings up wg0 from it
// and adds policy routing so that only the gnirehtet relay's uid (whose
// sockets carry every tethered device's traffic) uses the tunnel. The relay
// uid's routing table falls back to an unreachable route, so when the tunnel
// is down tethered devices get no internet rather than leaking out directly.
// Everything else (Batter's UI, adb, the host) keeps the normal route.
package vpn

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// Config is a parsed wg-quick config. It holds secrets; use Redacted for
// anything that leaves the server.
type Config struct {
	PrivateKey string
	Addresses  []string // CIDR prefixes for wg0
	DNS        []string // DNS server IPs for tethered devices
	MTU        int      // 0 = default
	ListenPort int      // 0 = random
	Peers      []Peer
}

// Peer is one [Peer] section.
type Peer struct {
	PublicKey           string
	PresharedKey        string
	Endpoint            string
	AllowedIPs          []string
	PersistentKeepalive int
}

// ConfigError reports an invalid config.
type ConfigError struct{ msg string }

func (e *ConfigError) Error() string { return e.msg }

func configErr(line int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if line > 0 {
		msg = fmt.Sprintf("line %d: %s", line, msg)
	}
	return &ConfigError{msg}
}

// Parse parses and validates a wg-quick config. Hook commands (PostUp etc.)
// and routing options are rejected: Batter manages wg0's routing itself.
func Parse(text string) (*Config, error) {
	c := &Config{}
	var section string
	var peer *Peer
	seenInterface := false

	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			switch section {
			case "interface":
				if seenInterface {
					return nil, configErr(n, "more than one [Interface] section")
				}
				seenInterface = true
			case "peer":
				c.Peers = append(c.Peers, Peer{})
				peer = &c.Peers[len(c.Peers)-1]
			default:
				return nil, configErr(n, "unknown section [%s]", section)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, configErr(n, "expected Key = Value")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		var err error
		switch section {
		case "interface":
			err = c.setInterface(key, value)
		case "peer":
			err = peer.set(key, value)
		default:
			err = fmt.Errorf("%q is outside any section", key)
		}
		if err != nil {
			return nil, configErr(n, "%v", err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, configErr(0, "%v", err)
	}
	return c, c.validate()
}

func (c *Config) setInterface(key, value string) error {
	switch key {
	case "privatekey":
		if err := checkKey(value); err != nil {
			return fmt.Errorf("PrivateKey: %w", err)
		}
		c.PrivateKey = value
	case "address":
		for _, a := range splitList(value) {
			p, err := parsePrefixOrAddr(a)
			if err != nil {
				return fmt.Errorf("Address: %w", err)
			}
			c.Addresses = append(c.Addresses, p.String())
		}
	case "dns":
		for _, d := range splitList(value) {
			ip, err := netip.ParseAddr(d)
			if err != nil {
				return fmt.Errorf("DNS: %q is not an IP address (search domains aren't supported)", d)
			}
			c.DNS = append(c.DNS, ip.String())
		}
	case "mtu":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1280 || n > 9000 {
			return fmt.Errorf("MTU: want 1280-9000, got %q", value)
		}
		c.MTU = n
	case "listenport":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("ListenPort: invalid port %q", value)
		}
		c.ListenPort = n
	case "preup", "postup", "predown", "postdown":
		return fmt.Errorf("%s hook commands aren't supported; remove them", key)
	default:
		return fmt.Errorf("unsupported [Interface] key %q", key)
	}
	return nil
}

func (p *Peer) set(key, value string) error {
	switch key {
	case "publickey":
		if err := checkKey(value); err != nil {
			return fmt.Errorf("PublicKey: %w", err)
		}
		p.PublicKey = value
	case "presharedkey":
		if err := checkKey(value); err != nil {
			return fmt.Errorf("PresharedKey: %w", err)
		}
		p.PresharedKey = value
	case "endpoint":
		host, port, err := net.SplitHostPort(value)
		if err != nil || host == "" {
			return fmt.Errorf("Endpoint: want host:port, got %q", value)
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("Endpoint: invalid port %q", port)
		}
		p.Endpoint = value
	case "allowedips":
		for _, a := range splitList(value) {
			pfx, err := parsePrefixOrAddr(a)
			if err != nil {
				return fmt.Errorf("AllowedIPs: %w", err)
			}
			p.AllowedIPs = append(p.AllowedIPs, pfx.Masked().String())
		}
	case "persistentkeepalive":
		if value == "off" {
			return nil
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 65535 {
			return fmt.Errorf("PersistentKeepalive: invalid %q", value)
		}
		p.PersistentKeepalive = n
	default:
		return fmt.Errorf("unsupported [Peer] key %q", key)
	}
	return nil
}

func (c *Config) validate() error {
	if c.PrivateKey == "" {
		return configErr(0, "[Interface] PrivateKey is required")
	}
	if len(c.Addresses) == 0 {
		return configErr(0, "[Interface] Address is required")
	}
	if len(c.Peers) == 0 {
		return configErr(0, "at least one [Peer] is required")
	}
	for i, p := range c.Peers {
		switch {
		case p.PublicKey == "":
			return configErr(0, "peer %d: PublicKey is required", i+1)
		case p.Endpoint == "":
			return configErr(0, "peer %d: Endpoint is required", i+1)
		case len(p.AllowedIPs) == 0:
			return configErr(0, "peer %d: AllowedIPs is required", i+1)
		}
	}
	return nil
}

// Setconf renders the config for `wg setconf`, which takes only the keys
// WireGuard itself knows (not wg-quick's Address, DNS, MTU).
func (c *Config) Setconf() string {
	var b strings.Builder
	b.WriteString("[Interface]\nPrivateKey = " + c.PrivateKey + "\n")
	if c.ListenPort != 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", c.ListenPort)
	}
	for _, p := range c.Peers {
		b.WriteString("\n[Peer]\nPublicKey = " + p.PublicKey + "\n")
		if p.PresharedKey != "" {
			b.WriteString("PresharedKey = " + p.PresharedKey + "\n")
		}
		b.WriteString("Endpoint = " + p.Endpoint + "\n")
		b.WriteString("AllowedIPs = " + strings.Join(p.AllowedIPs, ", ") + "\n")
		if p.PersistentKeepalive != 0 {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", p.PersistentKeepalive)
		}
	}
	return b.String()
}

// RedactedConfig is the config without its secrets, safe to return from
// the API.
type RedactedConfig struct {
	PublicKey string         `json:"public_key"` // derived from PrivateKey
	Addresses []string       `json:"addresses"`
	DNS       []string       `json:"dns"`
	MTU       int            `json:"mtu,omitempty"`
	Peers     []RedactedPeer `json:"peers"`
}

// RedactedPeer is a peer without its preshared key.
type RedactedPeer struct {
	PublicKey           string   `json:"public_key"`
	HasPresharedKey     bool     `json:"has_preshared_key"`
	Endpoint            string   `json:"endpoint"`
	AllowedIPs          []string `json:"allowed_ips"`
	PersistentKeepalive int      `json:"persistent_keepalive,omitempty"`
}

// Redacted returns the config without PrivateKey and PresharedKeys.
func (c *Config) Redacted() RedactedConfig {
	r := RedactedConfig{
		PublicKey: publicKey(c.PrivateKey),
		Addresses: c.Addresses,
		DNS:       c.DNS,
		MTU:       c.MTU,
	}
	for _, p := range c.Peers {
		r.Peers = append(r.Peers, RedactedPeer{
			PublicKey:           p.PublicKey,
			HasPresharedKey:     p.PresharedKey != "",
			Endpoint:            p.Endpoint,
			AllowedIPs:          p.AllowedIPs,
			PersistentKeepalive: p.PersistentKeepalive,
		})
	}
	return r
}

// publicKey derives a WireGuard public key from a (validated) private key.
func publicKey(priv string) string {
	k, err := base64.StdEncoding.DecodeString(priv)
	if err != nil {
		return ""
	}
	pub, err := curve25519.X25519(k, curve25519.Basepoint)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(pub)
}

func checkKey(s string) error {
	k, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(k) != 32 {
		return fmt.Errorf("not a WireGuard key (base64 of 32 bytes)")
	}
	return nil
}

// parsePrefixOrAddr accepts "10.0.0.2/24" or a bare "10.0.0.2" (a host
// prefix), as wg-quick does.
func parsePrefixOrAddr(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
