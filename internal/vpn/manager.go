package vpn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	iface = "wg0"
	// routeTable holds the relay uid's routes: the tunnel's AllowedIPs, then
	// an unreachable default as the kill switch.
	routeTable = "51820"
	// rulePriority sits ahead of the main table's rule (32766).
	rulePriority = "10000"
	// killSwitchMetric ranks the unreachable default below any tunnel route.
	killSwitchMetric = "4294967295"
)

// applyTimeout bounds applying or removing a config.
const applyTimeout = 30 * time.Second

// ErrKillSwitch means the relay uid could not be fenced off, so tethered
// traffic would bypass the tunnel. The relay must not run in that state.
var ErrKillSwitch = errors.New("vpn kill switch not installed")

// RunFunc runs a command, feeding it stdin, and returns its combined output.
type RunFunc func(ctx context.Context, stdin, name string, args ...string) ([]byte, error)

// Exec runs commands for real.
func Exec(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

// Manager stores the WireGuard config and keeps wg0 and the relay uid's
// routing in line with it.
type Manager struct {
	Path   string // config file (in DATA_DIR), written 0600
	UID    uint32 // uid whose traffic goes through the tunnel
	Run    RunFunc
	Logger *slog.Logger
	// CheckExit reports the public IP that UID's traffic leaves from; nil
	// if unavailable.
	CheckExit func(ctx context.Context) (string, error)

	mu       sync.Mutex
	loaded   bool
	stored   *stored // nil = not configured
	cfg      *Config
	applyErr error
}

type stored struct {
	Enabled bool   `json:"enabled"`
	Config  string `json:"config"` // wg-quick text as pasted
}

// Info describes the VPN for the API. It never contains secrets.
type Info struct {
	Configured bool            `json:"configured"`
	Enabled    bool            `json:"enabled"`
	Config     *RedactedConfig `json:"config,omitempty"`
	Status     *Status         `json:"status,omitempty"`
	ApplyError string          `json:"apply_error,omitempty"`
}

// Status is wg0's live state.
type Status struct {
	Up    bool         `json:"up"`
	Peers []PeerStatus `json:"peers"`
}

// PeerStatus is one peer's live state.
type PeerStatus struct {
	PublicKey       string     `json:"public_key"`
	Endpoint        string     `json:"endpoint,omitempty"`
	LatestHandshake *time.Time `json:"latest_handshake,omitempty"`
	RxBytes         int64      `json:"rx_bytes"`
	TxBytes         int64      `json:"tx_bytes"`
}

// Start loads the stored config and applies it. It fails if the config
// can't be read, or with ErrKillSwitch (wrapped) if the VPN is enabled but
// the relay uid couldn't be fenced off; either way the relay must not start.
// A tunnel that won't come up is only logged and reported by Info: the kill
// switch keeps tethered traffic in.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return err
	}
	if m.stored == nil {
		return nil
	}
	if err := m.apply(ctx); errors.Is(err, ErrKillSwitch) {
		return err
	}
	return nil
}

// Set stores a new config (or keeps the stored one if text is empty) and
// whether it's enabled, then applies it. Invalid configs return a
// *ConfigError and change nothing. Apply failures don't fail Set; Info
// reports them.
func (m *Manager) Set(ctx context.Context, text string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A new config replaces an unreadable stored one.
	if err := m.load(); err != nil && text == "" {
		return err
	}
	cfg := m.cfg
	if text != "" {
		var err error
		if cfg, err = Parse(text); err != nil {
			return err
		}
	} else if m.stored == nil {
		return &ConfigError{"no WireGuard config saved; paste one first"}
	} else {
		text = m.stored.Config
	}

	s := &stored{Enabled: enabled, Config: text}
	if err := m.save(s); err != nil {
		return err
	}
	m.stored, m.cfg, m.loaded = s, cfg, true
	// Finish applying even if the caller goes away: a half-applied config
	// is worse than a slow response.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), applyTimeout)
	defer cancel()
	_ = m.apply(ctx) // recorded in applyErr
	return nil
}

// Delete forgets the config and removes the tunnel and its routing, so
// tethered devices use the host's normal internet again.
func (m *Manager) Delete(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.Remove(m.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove wireguard config: %w", err)
	}
	m.stored, m.cfg, m.loaded = nil, nil, true
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), applyTimeout)
	defer cancel()
	m.teardown(ctx)
	m.applyErr = nil
	return nil
}

// DNS returns the config's DNS servers while the VPN is enabled, else nil.
func (m *Manager) DNS() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stored == nil || !m.stored.Enabled || m.cfg == nil {
		return nil
	}
	return m.cfg.DNS
}

// Info returns the redacted config and live status.
func (m *Manager) Info(ctx context.Context) Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return Info{ApplyError: err.Error()}
	}
	if m.stored == nil {
		return Info{}
	}
	r := m.cfg.Redacted()
	info := Info{Configured: true, Enabled: m.stored.Enabled, Config: &r}
	if m.applyErr != nil {
		info.ApplyError = m.applyErr.Error()
	}
	if m.stored.Enabled {
		info.Status = m.status(ctx)
	}
	return info
}

// load reads the stored config once.
func (m *Manager) load() error {
	if m.loaded {
		return nil
	}
	b, err := os.ReadFile(m.Path)
	if os.IsNotExist(err) {
		m.loaded = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("read wireguard config: %w", err)
	}
	var s stored
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("read wireguard config %s: %w", m.Path, err)
	}
	cfg, err := Parse(s.Config)
	if err != nil {
		return fmt.Errorf("stored wireguard config: %w", err)
	}
	m.stored, m.cfg, m.loaded = &s, cfg, true
	return nil
}

// save writes the config atomically, readable by Batter only.
func (m *Manager) save(s *stored) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		return fmt.Errorf("save wireguard config: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.Path), ".wireguard-*")
	if err != nil {
		return fmt.Errorf("save wireguard config: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("save wireguard config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save wireguard config: %w", err)
	}
	// CreateTemp makes the file 0600.
	if err := os.Rename(tmp.Name(), m.Path); err != nil {
		return fmt.Errorf("save wireguard config: %w", err)
	}
	return nil
}

// apply makes the system match m.stored and records the outcome.
func (m *Manager) apply(ctx context.Context) error {
	if !m.stored.Enabled {
		m.teardown(ctx)
		m.applyErr = nil
		return nil
	}
	if err := m.installKillSwitch(ctx); err != nil {
		m.applyErr = err
		m.Logger.Error("vpn: kill switch not installed; tethered traffic is not protected", "error", err)
		return err
	}
	if err := m.bringUp(ctx); err != nil {
		m.applyErr = err
		m.Logger.Error("vpn: tunnel not up; tethered devices have no internet until it is", "error", err)
		return err
	}
	m.applyErr = nil
	m.Logger.Info("vpn: tunnel up", "addresses", m.cfg.Addresses)
	return nil
}

// installKillSwitch sends the relay uid to its own table, whose last resort
// is unreachable. It's idempotent and never removes anything, so re-applying
// a config doesn't open a window where the relay's traffic goes direct.
func (m *Manager) installKillSwitch(ctx context.Context) error {
	uid := strconv.FormatUint(uint64(m.UID), 10)
	for _, v6 := range []bool{false, true} {
		err := m.ensureRule(ctx, v6, uid)
		if err == nil {
			_, err = m.ip(ctx, v6, "route", "replace", "unreachable", "default",
				"table", routeTable, "metric", killSwitchMetric)
		}
		if err != nil {
			if v6 {
				// No IPv6 in the container means no IPv6 to leak.
				m.Logger.Warn("vpn: IPv6 kill switch not installed", "error", err)
				continue
			}
			return fmt.Errorf("%w: %v", ErrKillSwitch, err)
		}
	}
	return nil
}

func (m *Manager) ensureRule(ctx context.Context, v6 bool, uid string) error {
	out, err := m.ip(ctx, v6, "rule", "show", "priority", rulePriority)
	if err != nil {
		return err
	}
	if strings.Contains(string(out), "uidrange "+uid+"-"+uid) {
		return nil
	}
	_, err = m.ip(ctx, v6, "rule", "add", "uidrange", uid+"-"+uid,
		"lookup", routeTable, "priority", rulePriority)
	return err
}

// bringUp (re)creates wg0 and routes the tunnel's AllowedIPs in the relay
// uid's table. Deleting the old wg0 drops its routes, leaving the kill
// switch in charge until the new one is up.
func (m *Manager) bringUp(ctx context.Context) error {
	_, _ = m.run(ctx, "", "ip", "link", "del", iface)
	steps := [][]string{
		{"ip", "link", "add", iface, "type", "wireguard"},
		{"wg", "setconf", iface, "/dev/stdin"},
	}
	for _, s := range steps {
		stdin := ""
		if s[0] == "wg" {
			stdin = m.cfg.Setconf()
		}
		if _, err := m.run(ctx, stdin, s[0], s[1:]...); err != nil {
			return err
		}
	}
	for _, a := range m.cfg.Addresses {
		if _, err := m.run(ctx, "", "ip", "address", "add", a, "dev", iface); err != nil && !isV6(a) {
			return err
		} else if err != nil {
			m.Logger.Warn("vpn: IPv6 address not added", "address", a, "error", err)
		}
	}
	mtu := m.cfg.MTU
	if mtu == 0 {
		mtu = 1420
	}
	if _, err := m.run(ctx, "", "ip", "link", "set", iface, "mtu", strconv.Itoa(mtu), "up"); err != nil {
		return err
	}
	m.relaxReversePathFilter(ctx)
	for _, p := range m.cfg.Peers {
		for _, dst := range p.AllowedIPs {
			_, err := m.ip(ctx, isV6(dst), "route", "replace", dst, "dev", iface, "table", routeTable)
			if err != nil && !isV6(dst) {
				return err
			} else if err != nil {
				m.Logger.Warn("vpn: IPv6 route not added", "dst", dst, "error", err)
			}
		}
	}
	return nil
}

// relaxReversePathFilter makes replies arriving on wg0 acceptable. Strict
// reverse-path filtering checks the source against the main table (the
// check doesn't carry the relay's uid), which routes the internet via eth0,
// so it would drop every reply. Loose mode only needs some route back.
func (m *Manager) relaxReversePathFilter(ctx context.Context) {
	if out, err := m.run(ctx, "", "sysctl", "-n", "net.ipv4.conf.all.rp_filter"); err == nil && strings.TrimSpace(string(out)) == "1" {
		if _, err := m.run(ctx, "", "sysctl", "-w", "net.ipv4.conf.all.rp_filter=2"); err != nil {
			m.Logger.Warn("vpn: couldn't relax rp_filter", "error", err)
		}
	}
	if _, err := m.run(ctx, "", "sysctl", "-w", "net.ipv4.conf."+iface+".rp_filter=2"); err != nil {
		m.Logger.Warn("vpn: couldn't relax rp_filter on "+iface, "error", err)
	}
}

// teardown removes the tunnel and the relay uid's routing; missing pieces
// are fine.
func (m *Manager) teardown(ctx context.Context) {
	uid := strconv.FormatUint(uint64(m.UID), 10)
	for _, v6 := range []bool{false, true} {
		// Delete every copy, in case one was ever added twice.
		for range 10 {
			if _, err := m.ip(ctx, v6, "rule", "del", "uidrange", uid+"-"+uid, "lookup", routeTable); err != nil {
				break
			}
		}
		_, _ = m.ip(ctx, v6, "route", "flush", "table", routeTable)
	}
	_, _ = m.run(ctx, "", "ip", "link", "del", iface)
}

// status parses `wg show wg0 dump`. Its first line carries the private key
// and is skipped.
func (m *Manager) status(ctx context.Context) *Status {
	st := &Status{Peers: []PeerStatus{}}
	if out, err := m.run(ctx, "", "ip", "-o", "link", "show", iface); err == nil {
		if i := strings.IndexByte(string(out), '<'); i >= 0 {
			flags, _, _ := strings.Cut(string(out[i+1:]), ">")
			st.Up = strings.Contains(","+flags+",", ",UP,")
		}
	}
	out, err := m.run(ctx, "", "wg", "show", iface, "dump")
	if err != nil {
		return st
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines[min(1, len(lines)):] {
		f := strings.Split(line, "\t")
		if len(f) < 8 {
			continue
		}
		p := PeerStatus{PublicKey: f[0]}
		if f[2] != "(none)" {
			p.Endpoint = f[2]
		}
		if ts, _ := strconv.ParseInt(f[4], 10, 64); ts > 0 {
			t := time.Unix(ts, 0).UTC()
			p.LatestHandshake = &t
		}
		p.RxBytes, _ = strconv.ParseInt(f[5], 10, 64)
		p.TxBytes, _ = strconv.ParseInt(f[6], 10, 64)
		st.Peers = append(st.Peers, p)
	}
	return st
}

func (m *Manager) run(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	run := m.Run
	if run == nil {
		run = Exec
	}
	out, err := run(ctx, stdin, name, args...)
	if err != nil {
		// Never echo stdin: for wg setconf it's the private key.
		return out, fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

// ip runs `ip` for IPv4 (its default) or, with v6, IPv6.
func (m *Manager) ip(ctx context.Context, v6 bool, args ...string) ([]byte, error) {
	if v6 {
		args = append([]string{"-6"}, args...)
	}
	return m.run(ctx, "", "ip", args...)
}

func isV6(prefix string) bool {
	return strings.Contains(prefix, ":")
}
