package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeNet records commands. fail makes commands containing the substring
// fail; dump is what `wg show wg0 dump` prints.
type fakeNet struct {
	cmds  []string
	stdin map[string]string
	fail  string
	dump  string
}

func (f *fakeNet) run(_ context.Context, stdin, name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.cmds = append(f.cmds, cmd)
	if stdin != "" {
		if f.stdin == nil {
			f.stdin = map[string]string{}
		}
		f.stdin[cmd] = stdin
	}
	if f.fail != "" && strings.Contains(cmd, f.fail) {
		return []byte("RTNETLINK answers: Operation not permitted"), errors.New("exit status 2")
	}
	if cmd == "wg show wg0 dump" {
		return []byte(f.dump), nil
	}
	if strings.HasPrefix(cmd, "ip -o link show") {
		return []byte("5: wg0: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1420 qdisc noqueue state UNKNOWN\n"), nil
	}
	return nil, nil
}

func (f *fakeNet) index(t *testing.T, prefix string) int {
	t.Helper()
	i := slices.IndexFunc(f.cmds, func(c string) bool { return strings.HasPrefix(c, prefix) })
	if i < 0 {
		t.Fatalf("no %q command; ran:\n  %s", prefix, strings.Join(f.cmds, "\n  "))
	}
	return i
}

func newManager(t *testing.T, net *fakeNet) *Manager {
	t.Helper()
	return &Manager{
		Path:   filepath.Join(t.TempDir(), "wireguard.json"),
		UID:    31416,
		Run:    net.run,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestEnableInstallsKillSwitchBeforeTunnel(t *testing.T) {
	net := &fakeNet{}
	m := newManager(t, net)
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	if info := m.Info(context.Background()); info.ApplyError != "" {
		t.Fatalf("apply error: %s", info.ApplyError)
	}

	rule := net.index(t, "ip rule add uidrange 31416-31416 lookup 51820")
	unreachable := net.index(t, "ip route replace unreachable default table 51820")
	link := net.index(t, "ip link add wg0 type wireguard")
	if rule > link || unreachable > link {
		t.Fatalf("tunnel created before the kill switch; ran:\n  %s", strings.Join(net.cmds, "\n  "))
	}
	if got := net.stdin["wg setconf wg0 /dev/stdin"]; !strings.Contains(got, "PrivateKey = "+testPriv) {
		t.Fatalf("wg setconf not fed the config: %q", got)
	}
	net.index(t, "ip address add 10.64.0.2/32 dev wg0")
	net.index(t, "ip route replace 0.0.0.0/0 dev wg0 table 51820")
	net.index(t, "ip -6 route replace ::/0 dev wg0 table 51820")
	// Only the relay uid's table routes into the tunnel.
	for _, c := range net.cmds {
		if strings.Contains(c, "route") && strings.Contains(c, "dev wg0") && !strings.Contains(c, "table 51820") {
			t.Fatalf("route outside the relay's table: %s", c)
		}
	}
}

func TestReapplyKeepsKillSwitchInPlace(t *testing.T) {
	net := &fakeNet{}
	m := newManager(t, net)
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	net.cmds = nil
	if err := m.Set(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	for _, c := range net.cmds {
		if strings.HasPrefix(c, "ip rule del") || strings.Contains(c, "route flush") {
			t.Fatalf("re-applying removed the kill switch (leak window): %s", c)
		}
	}
	net.index(t, "ip link add wg0 type wireguard")
}

func TestDisableRemovesRoutingAndTunnel(t *testing.T) {
	net := &fakeNet{}
	m := newManager(t, net)
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	net.cmds = nil
	if err := m.Set(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	net.index(t, "ip rule del uidrange 31416-31416 lookup 51820")
	net.index(t, "ip route flush table 51820")
	net.index(t, "ip link del wg0")
	if slices.ContainsFunc(net.cmds, func(c string) bool { return strings.Contains(c, " add ") }) {
		t.Fatalf("disable added something: %v", net.cmds)
	}
	if m.DNS() != nil {
		t.Fatalf("DNS = %v with the VPN off, want default", m.DNS())
	}
}

func TestConfigStoredPrivatelyAndReappliedOnStart(t *testing.T) {
	m := newManager(t, &fakeNet{})
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(m.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode %v, want 0600", st.Mode().Perm())
	}

	net := &fakeNet{}
	restarted := &Manager{Path: m.Path, UID: 31416, Run: net.run, Logger: m.Logger}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	net.index(t, "ip rule add uidrange 31416-31416 lookup 51820")
	net.index(t, "ip link add wg0 type wireguard")
	if got := strings.Join(restarted.DNS(), ","); got != "10.64.0.1" {
		t.Fatalf("DNS after restart = %q", got)
	}
}

func TestStartWithoutConfigLeavesRoutingAlone(t *testing.T) {
	net := &fakeNet{}
	if err := newManager(t, net).Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(net.cmds, func(c string) bool { return strings.Contains(c, " add ") }) {
		t.Fatalf("set up routing with no config: %v", net.cmds)
	}
}

func TestStartReportsKillSwitchFailure(t *testing.T) {
	m := newManager(t, &fakeNet{})
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	failing := &fakeNet{fail: "ip rule add"}
	restarted := &Manager{Path: m.Path, UID: 31416, Run: failing.run, Logger: m.Logger}
	if err := restarted.Start(context.Background()); !errors.Is(err, ErrKillSwitch) {
		t.Fatalf("Start = %v, want ErrKillSwitch (the relay must not run unprotected)", err)
	}

	// A tunnel that won't come up is fine to run behind: the kill switch holds.
	tunnelDown := &fakeNet{fail: "wg setconf"}
	restarted = &Manager{Path: m.Path, UID: 31416, Run: tunnelDown.run, Logger: m.Logger}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v with only the tunnel failing", err)
	}
}

func TestStartFailsOnUnreadableConfig(t *testing.T) {
	m := newManager(t, &fakeNet{})
	if err := os.WriteFile(m.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err == nil {
		t.Fatal("Start ignored a corrupt config (it may have been enabled)")
	}
}

func TestTunnelFailureIsReportedButKillSwitchStays(t *testing.T) {
	net := &fakeNet{fail: "wg setconf"}
	m := newManager(t, net)
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	if info := m.Info(context.Background()); !strings.Contains(info.ApplyError, "wg setconf") {
		t.Fatalf("apply error = %q", info.ApplyError)
	}
	if slices.ContainsFunc(net.cmds, func(c string) bool { return strings.HasPrefix(c, "ip rule del") }) {
		t.Fatalf("kill switch removed after a tunnel failure: %v", net.cmds)
	}
}

func TestIPv6FailuresAreTolerated(t *testing.T) {
	// Containers often have IPv6 disabled; the IPv4 tunnel must still work.
	net := &fakeNet{fail: "-6"}
	m := newManager(t, net)
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	if info := m.Info(context.Background()); info.ApplyError != "" {
		t.Fatalf("apply error: %s", info.ApplyError)
	}
	net.index(t, "ip route replace 0.0.0.0/0 dev wg0 table 51820")
}

func TestInvalidConfigIsNotStoredOrApplied(t *testing.T) {
	net := &fakeNet{}
	m := newManager(t, net)
	err := m.Set(context.Background(), "[Interface]\nPostUp = rm -rf /\n", true)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Set = %v, want ConfigError", err)
	}
	if _, err := os.Stat(m.Path); !os.IsNotExist(err) {
		t.Fatalf("invalid config stored")
	}
	if len(net.cmds) != 0 {
		t.Fatalf("ran commands for an invalid config: %v", net.cmds)
	}
	if err := m.Set(context.Background(), "", true); err == nil {
		t.Fatal("enabling with no stored config succeeded")
	}
}

func TestDeleteRemovesConfigAndRouting(t *testing.T) {
	net := &fakeNet{}
	m := newManager(t, net)
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Path); !os.IsNotExist(err) {
		t.Fatal("config file still present")
	}
	net.index(t, "ip rule del uidrange 31416-31416 lookup 51820")
	if info := m.Info(context.Background()); info.Configured {
		t.Fatal("still configured")
	}
}

func TestInfoReportsStatusWithoutSecrets(t *testing.T) {
	net := &fakeNet{dump: testPriv + "\t" + testPub + "\t51820\toff\n" +
		testPub + "\t" + testPSK + "\t203.0.113.7:51820\t0.0.0.0/0\t1759312800\t1234\t5678\t25\n"}
	m := newManager(t, net)
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatal(err)
	}
	info := m.Info(context.Background())
	if info.Status == nil || !info.Status.Up || len(info.Status.Peers) != 1 {
		t.Fatalf("status: %+v", info.Status)
	}
	p := info.Status.Peers[0]
	if p.Endpoint != "203.0.113.7:51820" || p.RxBytes != 1234 || p.TxBytes != 5678 ||
		p.LatestHandshake == nil || p.LatestHandshake.Unix() != 1759312800 {
		t.Fatalf("peer status: %+v", p)
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), testPriv) || strings.Contains(string(b), testPSK) {
		t.Fatalf("info leaks a secret: %s", b)
	}
}

func TestNewConfigReplacesUnreadableOne(t *testing.T) {
	m := newManager(t, &fakeNet{})
	if err := os.WriteFile(m.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(context.Background(), "", true); err == nil {
		t.Fatal("toggled a config that can't be read")
	}
	if err := m.Set(context.Background(), sample, true); err != nil {
		t.Fatalf("couldn't replace the unreadable config: %v", err)
	}
	if info := m.Info(context.Background()); !info.Configured || !info.Enabled {
		t.Fatalf("info after replace: %+v", info)
	}
}
