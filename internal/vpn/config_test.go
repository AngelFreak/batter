package vpn

import (
	"encoding/json"
	"strings"
	"testing"
)

// Throwaway keys, not used anywhere.
const (
	testPriv = "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	testPub  = "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	testPSK  = "FpCyhws9cxwWoV4xELtfJvjJN+zQVRPISllRWgeopVE="
)

const sample = `# Provider config
[Interface]
PrivateKey = ` + testPriv + `
Address = 10.64.0.2/32, fc00:bbbb::2/128
DNS = 10.64.0.1
MTU = 1380

[Peer]
PublicKey = ` + testPub + `
PresharedKey = ` + testPSK + `
Endpoint = vpn.example.net:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`

func TestParseWgQuickConfig(t *testing.T) {
	c, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	if c.PrivateKey != testPriv || c.MTU != 1380 {
		t.Fatalf("interface: %+v", c)
	}
	if got := strings.Join(c.Addresses, ","); got != "10.64.0.2/32,fc00:bbbb::2/128" {
		t.Fatalf("addresses: %s", got)
	}
	if got := strings.Join(c.DNS, ","); got != "10.64.0.1" {
		t.Fatalf("dns: %s", got)
	}
	if len(c.Peers) != 1 {
		t.Fatalf("peers: %d", len(c.Peers))
	}
	p := c.Peers[0]
	if p.PublicKey != testPub || p.PresharedKey != testPSK || p.Endpoint != "vpn.example.net:51820" ||
		strings.Join(p.AllowedIPs, ",") != "0.0.0.0/0,::/0" || p.PersistentKeepalive != 25 {
		t.Fatalf("peer: %+v", p)
	}
}

func TestParseDefaultsBareAddressToHostPrefix(t *testing.T) {
	c, err := Parse(strings.Replace(sample, "10.64.0.2/32, fc00:bbbb::2/128", "10.64.0.2", 1))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addresses[0] != "10.64.0.2/32" {
		t.Fatalf("address = %s, want 10.64.0.2/32", c.Addresses[0])
	}
}

func TestParseRejectsInvalidConfigs(t *testing.T) {
	cases := map[string]string{
		"hook commands":   strings.Replace(sample, "MTU = 1380", "PostUp = iptables -F", 1),
		"unknown key":     strings.Replace(sample, "MTU = 1380", "Colour = blue", 1),
		"bad private key": strings.Replace(sample, testPriv, "notbase64", 1),
		"no address":      strings.Replace(sample, "Address = 10.64.0.2/32, fc00:bbbb::2/128\n", "", 1),
		"no peer":         sample[:strings.Index(sample, "[Peer]")],
		"no endpoint":     strings.Replace(sample, "Endpoint = vpn.example.net:51820\n", "", 1),
		"bad endpoint":    strings.Replace(sample, "vpn.example.net:51820", "vpn.example.net", 1),
		"bad allowed ips": strings.Replace(sample, "0.0.0.0/0, ::/0", "everything", 1),
		"no allowed ips":  strings.Replace(sample, "AllowedIPs = 0.0.0.0/0, ::/0\n", "", 1),
		"dns search":      strings.Replace(sample, "DNS = 10.64.0.1", "DNS = 10.64.0.1, corp.example", 1),
		"two interfaces":  sample + "\n[Interface]\nPrivateKey = " + testPriv + "\n",
		"key outside":     "PrivateKey = " + testPriv + "\n" + sample,
		"not key=value":   strings.Replace(sample, "MTU = 1380", "MTU 1380", 1),
	}
	for name, conf := range cases {
		if _, err := Parse(conf); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSetconfOmitsWgQuickOnlyKeys(t *testing.T) {
	c, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Setconf()
	for _, want := range []string{"PrivateKey = " + testPriv, "PublicKey = " + testPub, "PresharedKey = " + testPSK,
		"Endpoint = vpn.example.net:51820", "AllowedIPs = 0.0.0.0/0, ::/0", "PersistentKeepalive = 25"} {
		if !strings.Contains(got, want) {
			t.Errorf("setconf lacks %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"Address", "DNS", "MTU"} {
		if strings.Contains(got, bad) {
			t.Errorf("setconf has wg-quick-only key %s (wg setconf rejects it):\n%s", bad, got)
		}
	}
}

func TestRedactedViewHasNoSecrets(t *testing.T) {
	c, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(c.Redacted())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, testPriv) || strings.Contains(s, testPSK) {
		t.Fatalf("redacted view leaks a secret: %s", s)
	}
	for _, want := range []string{testPub, "vpn.example.net:51820", "10.64.0.2/32", `"has_preshared_key":true`} {
		if !strings.Contains(s, want) {
			t.Errorf("redacted view lacks %s: %s", want, s)
		}
	}
	// The interface's own public key is what the VPN server needs to know.
	if r := c.Redacted(); r.PublicKey == "" || r.PublicKey == testPriv {
		t.Fatalf("public key = %q", r.PublicKey)
	}
}
