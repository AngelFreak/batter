package lan

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeSystem records commands; ones containing fail fail.
type fakeSystem struct {
	mu    sync.Mutex
	log   []string
	stdin map[string]string
	fail  string
	out   map[string]string // command prefix -> output
}

func (f *fakeSystem) run(_ context.Context, stdin, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := name + " " + strings.Join(args, " ")
	f.log = append(f.log, cmd)
	if stdin != "" {
		if f.stdin == nil {
			f.stdin = map[string]string{}
		}
		f.stdin[cmd] = stdin
	}
	if f.fail != "" && strings.Contains(cmd, f.fail) {
		return []byte("Error: Operation not permitted"), errors.New("exit status 1")
	}
	for prefix, out := range f.out {
		if strings.HasPrefix(cmd, prefix) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

func (f *fakeSystem) index(t *testing.T, prefix string) int {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.log, func(c string) bool { return strings.HasPrefix(c, prefix) })
	if i < 0 {
		t.Fatalf("no %q; ran:\n  %s", prefix, strings.Join(f.log, "\n  "))
	}
	return i
}

func (f *fakeSystem) has(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.log, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

var (
	phoneA = Phone{IP: netip.MustParseAddr("10.77.0.100"), MAC: "02:00:00:00:00:0a", Table: 51820, DNS: netip.MustParseAddr("10.99.0.1")}
	phoneB = Phone{IP: netip.MustParseAddr("10.77.0.101"), MAC: "02:00:00:00:00:0b", Table: 51821}
	// No profile: no route and nothing forwarded.
	phoneC = Phone{IP: netip.MustParseAddr("10.77.0.102"), MAC: "02:00:00:00:00:0c"}
)

func newFirewall(t *testing.T, sys *fakeSystem) *Firewall {
	t.Helper()
	return &Firewall{Iface: "eth1", Net: testNet(t, ""), Run: sys.run, Logger: quiet}
}

func TestRulesetConfinesThePhoneLAN(t *testing.T) {
	rules := Ruleset("eth1", testNet(t, ""), []Phone{phoneA, phoneB, phoneC})
	for _, want := range []string{
		// Replaced in one transaction: never a window without rules.
		"table inet batter_lan {}\ndelete table inet batter_lan\ntable inet batter_lan {",
		// DNS goes to the profile's server (through its tunnel).
		`iifname "eth1" ip saddr 10.77.0.100 ether saddr 02:00:00:00:00:0a meta l4proto { tcp, udp } th dport 53 dnat ip to 10.99.0.1`,
		// Forwarding: each phone with a profile into tunnels only, by
		// address and adapter (another device can't borrow its address);
		// replies back.
		`iifname "eth1" oifname "wg*" ip saddr 10.77.0.100 ether saddr 02:00:00:00:00:0a accept`,
		`iifname "eth1" oifname "wg*" ip saddr 10.77.0.101 ether saddr 02:00:00:00:00:0b accept`,
		`iifname "wg*" oifname "eth1" ct state established,related accept`,
		`iifname "eth1" drop`,
		`oifname "eth1" drop`,
		// Clamp TCP to the tunnel's MTU.
		`oifname "wg*" tcp flags syn / syn,rst tcp option maxseg size set rt mtu`,
		// Batter itself: DHCP and replies on its own (adb) connections only.
		`iifname "eth1" ct state established,related accept`,
		`iifname "eth1" udp sport 68 udp dport 67 accept`,
		// No IPv6 at all from or to the LAN.
		`iifname "eth1" meta nfproto ipv6 drop`,
		`oifname "eth1" meta nfproto ipv6 drop`,
		`ip saddr 10.77.0.0/24 oifname "wg*" masquerade`,
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("ruleset lacks %q:\n%s", want, rules)
		}
	}
	for _, unwanted := range []string{"10.77.0.102", "02:00:00:00:00:0c", "10.77.0.101 ether saddr 02:00:00:00:00:0b meta l4proto"} {
		if strings.Contains(rules, unwanted) {
			t.Errorf("ruleset mentions %q (no profile / no DNS):\n%s", unwanted, rules)
		}
	}
	// The input chain's catch-all comes after the DHCP and reply accepts.
	in := rules[strings.Index(rules, "chain input"):]
	if strings.Index(in, `iifname "eth1" drop`) < strings.Index(in, "dport 67 accept") {
		t.Errorf("input drops before accepting DHCP:\n%s", in)
	}
}

// The fence (routing catch-all, nft table, phones' routes) goes in without
// the port: it matches the port by name, so it holds the moment the NIC
// arrives. It never touches forwarding.
func TestFenceInstallsTheRulesWithoutTheLink(t *testing.T) {
	sys := &fakeSystem{}
	fw := newFirewall(t, sys)
	if err := fw.Fence(context.Background(), []Phone{phoneA}); err != nil {
		t.Fatal(err)
	}
	catchAll := sys.index(t, "ip rule add iif eth1 unreachable priority 9900")
	nft := sys.index(t, "nft -f /dev/stdin")
	route := sys.index(t, "ip rule add from 10.77.0.100 iif eth1 lookup 51820 priority 9000")
	if catchAll > route || nft > route {
		t.Fatalf("phone routed before the LAN is fenced:\n  %s", strings.Join(sys.log, "\n  "))
	}
	if got := sys.stdin["nft -f /dev/stdin"]; got != Ruleset("eth1", fw.Net, []Phone{phoneA}) {
		t.Fatalf("nft fed:\n%s", got)
	}
	if sys.has("sysctl") || sys.has("ip link") || sys.has("ip address") {
		t.Fatalf("fence touched the link or forwarding:\n  %s", strings.Join(sys.log, "\n  "))
	}
}

func TestFenceKeepsAnExistingCatchAll(t *testing.T) {
	sys := &fakeSystem{out: map[string]string{
		"ip rule show priority 9900": "9900:	from all iif eth1 unreachable\n",
	}}
	if err := newFirewall(t, sys).Fence(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if sys.has("ip rule add") {
		t.Fatal("catch-all added twice")
	}
}

// Each phone's traffic is routed to its profile's table by source address.
// Stale rules (a phone switched profile, or lost it) go before new ones are
// added, so a switch never has two routes; in between, the catch-all holds.
func TestFenceRoutesPhonesBySourceAndDropsStaleRoutes(t *testing.T) {
	sys := &fakeSystem{out: map[string]string{
		"ip rule show priority 9000": "9000:\tfrom 10.77.0.101 iif eth1 lookup 51821 \n" + // phoneB: right
			"9000:\tfrom 10.77.0.100 iif eth1 lookup 51821 \n" + // phoneA: old profile
			"9000:\tfrom 10.77.0.102 iif eth1 lookup 51820 \n", // phoneC: profile taken away
	}}
	if err := newFirewall(t, sys).Fence(context.Background(), []Phone{phoneA, phoneB, phoneC}); err != nil {
		t.Fatal(err)
	}
	delOld := sys.index(t, "ip rule del from 10.77.0.100 iif eth1 lookup 51821 priority 9000")
	sys.index(t, "ip rule del from 10.77.0.102 iif eth1 lookup 51820 priority 9000")
	if add := sys.index(t, "ip rule add from 10.77.0.100 iif eth1 lookup 51820 priority 9000"); add < delOld {
		t.Fatal("new route added before the old one went")
	}
	if sys.has("ip rule del from 10.77.0.101") || sys.has("ip rule add from 10.77.0.101") || sys.has("ip rule add from 10.77.0.102") {
		t.Fatalf("touched a correct or unassigned phone's route:\n  %s", strings.Join(sys.log, "\n  "))
	}
}

func TestFenceReportsFailures(t *testing.T) {
	for _, fail := range []string{"nft", "ip rule add iif", "ip rule add from"} {
		sys := &fakeSystem{fail: fail}
		if err := newFirewall(t, sys).Fence(context.Background(), []Phone{phoneA}); err == nil {
			t.Fatalf("%s failing: no error", fail)
		}
	}
}

// Bringing the port up: no IPv6 on it (Batter has no link-local address
// for phones to reach), Batter's address, link up, and only then
// forwarding.
func TestUpConfiguresThePortBeforeForwarding(t *testing.T) {
	sys := &fakeSystem{}
	if err := newFirewall(t, sys).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	noV6 := sys.index(t, "sysctl -w net.ipv6.conf.eth1.disable_ipv6=1")
	addr := sys.index(t, "ip address replace 10.77.0.1/24 dev eth1")
	up := sys.index(t, "ip link set dev eth1 up")
	fwd := sys.index(t, "sysctl -w net.ipv4.ip_forward=1")
	if noV6 > up || addr > up || up > fwd {
		t.Fatalf("order:\n  %s", strings.Join(sys.log, "\n  "))
	}
}

// Turning the phone network off leaves nothing behind in Batter's
// namespace: no table, no rules, no forwarding.
func TestRemoveLeavesNothingBehind(t *testing.T) {
	sys := &fakeSystem{out: map[string]string{
		"ip rule show priority 9000": "9000:\tfrom 10.77.0.100 iif eth1 lookup 51820 \n",
		"ip rule show priority 9900": "9900:\tfrom all iif eth1 unreachable\n",
	}}
	if err := newFirewall(t, sys).Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	sys.index(t, "sysctl -w net.ipv4.ip_forward=0")
	sys.index(t, "ip rule del from 10.77.0.100 iif eth1 lookup 51820 priority 9000")
	sys.index(t, "ip rule del iif eth1 unreachable priority 9900")
	if got := sys.stdin["nft -f /dev/stdin"]; got != "table inet batter_lan {}\ndelete table inet batter_lan\n" {
		t.Fatalf("nft fed %q", got)
	}
	if sys.index(t, "sysctl -w net.ipv4.ip_forward=0") > sys.index(t, "nft -f /dev/stdin") {
		t.Fatal("firewall removed while still forwarding")
	}
}

// The guard (run before the web app starts) fences the port if it is
// already in Batter's namespace (it isn't, after a normal container start).
func TestGuardFencesAPortAlreadyPresent(t *testing.T) {
	sys := &fakeSystem{}
	if err := Guard(context.Background(), sys.run, quiet); err != nil {
		t.Fatal(err)
	}
	n, _ := ParseNetwork(DefaultAddr, DefaultPool)
	if got := sys.stdin["nft -f /dev/stdin"]; got != Ruleset(Iface, n, nil) {
		t.Fatalf("guard installed:\n%s", got)
	}
	absent := &fakeSystem{fail: "ip link show"}
	if err := Guard(context.Background(), absent.run, quiet); err != nil {
		t.Fatal(err)
	}
	if absent.has("nft") {
		t.Fatal("guard fenced a port that isn't there")
	}
}
