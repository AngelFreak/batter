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
	phoneA = Phone{IP: netip.MustParseAddr("10.77.0.100"), MAC: "02:00:00:00:00:0a", Mark: 51820, DNS: netip.MustParseAddr("10.99.0.1")}
	phoneB = Phone{IP: netip.MustParseAddr("10.77.0.101"), MAC: "02:00:00:00:00:0b", Mark: 51821}
	// No profile: no mark, so nothing routes it.
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
		// Each phone with a profile is marked for its profile's table, by
		// address and adapter (another device can't borrow its address).
		`iifname "eth1" ip saddr 10.77.0.100 ether saddr 02:00:00:00:00:0a meta mark set 51820`,
		`iifname "eth1" ip saddr 10.77.0.101 ether saddr 02:00:00:00:00:0b meta mark set 51821`,
		// DNS goes to the profile's server (through its tunnel).
		`iifname "eth1" ip saddr 10.77.0.100 ether saddr 02:00:00:00:00:0a meta l4proto { tcp, udp } th dport 53 dnat ip to 10.99.0.1`,
		// Forwarding: marked phones into tunnels only; replies back.
		`iifname "eth1" oifname "wg*" meta mark != 0 accept`,
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

func TestApplyFencesTheLANBeforeForwarding(t *testing.T) {
	sys := &fakeSystem{out: map[string]string{"ip -4 route show default": "default via 172.20.0.1 dev eth0\n"}}
	fw := newFirewall(t, sys)
	if err := fw.Apply(context.Background(), []Phone{phoneA}); err != nil {
		t.Fatal(err)
	}
	catchAll := sys.index(t, "ip rule add iif eth1 unreachable priority 9900")
	nft := sys.index(t, "nft -f /dev/stdin")
	forward := sys.index(t, "sysctl -w net.ipv4.ip_forward=1")
	if catchAll > forward || nft > forward {
		t.Fatalf("forwarding on before the LAN is fenced:\n  %s", strings.Join(sys.log, "\n  "))
	}
	if got := sys.stdin["nft -f /dev/stdin"]; got != Ruleset("eth1", fw.Net, []Phone{phoneA}) {
		t.Fatalf("nft fed:\n%s", got)
	}
	if sys.has("ip link set eth1 down") {
		t.Fatal("LAN taken down after a good apply")
	}
}

func TestApplyKeepsAnExistingCatchAll(t *testing.T) {
	sys := &fakeSystem{out: map[string]string{
		"ip -4 route show default":   "default via 172.20.0.1 dev eth0\n",
		"ip rule show priority 9900": "9900:	from all iif eth1 unreachable\n",
	}}
	if err := newFirewall(t, sys).Apply(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if sys.has("ip rule add") {
		t.Fatal("catch-all added twice")
	}
}

// If the rules can't go in, the phones get nothing: the LAN goes down.
func TestApplyFailureTakesTheLANDown(t *testing.T) {
	for _, fail := range []string{"nft", "ip rule add"} {
		sys := &fakeSystem{fail: fail, out: map[string]string{"ip -4 route show default": "default via 172.20.0.1 dev eth0\n"}}
		if err := newFirewall(t, sys).Apply(context.Background(), []Phone{phoneA}); err == nil {
			t.Fatalf("%s failing: no error", fail)
		}
		sys.index(t, "ip link set eth1 down")
		if sys.has("sysctl -w net.ipv4.ip_forward=1") {
			t.Fatalf("%s failing: forwarding turned on", fail)
		}
	}
}

// With two networks Docker may pick the phone LAN for the default route
// (Docker < 28 ignores gw_priority); Batter's own traffic would then try
// to leave through the phones' switch. Refuse it.
func TestApplyRefusesTheDefaultRouteOnTheLAN(t *testing.T) {
	sys := &fakeSystem{out: map[string]string{"ip -4 route show default": "default via 10.77.0.254 dev eth1\n"}}
	err := newFirewall(t, sys).Apply(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "default route") {
		t.Fatalf("err = %v", err)
	}
	sys.index(t, "ip link set eth1 down")
}

// A LAN taken down after a failure comes back once the rules are in.
func TestApplyBringsTheLANBackUpOnceFenced(t *testing.T) {
	sys := &fakeSystem{out: map[string]string{"ip -4 route show default": "default via 172.20.0.1 dev eth0\n"}}
	if err := newFirewall(t, sys).Apply(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if sys.index(t, "ip link set eth1 up") < sys.index(t, "nft -f /dev/stdin") {
		t.Fatal("LAN brought up before its firewall")
	}
}

// The guard (run before the web app starts) fences the LAN with no phones
// allowed anywhere, on the interface carrying Batter's LAN address.
func TestGuardFencesTheLANBeforeAnythingListens(t *testing.T) {
	sys := &fakeSystem{out: map[string]string{"ip -4 route show default": "default via 172.20.0.1 dev eth0\n"}}
	if err := Guard(context.Background(), "127.0.0.1/8", "", sys.run, quiet); err != nil {
		t.Fatal(err)
	}
	n, _ := ParseNetwork("127.0.0.1/8", "")
	if got := sys.stdin["nft -f /dev/stdin"]; got != Ruleset("lo", n, nil) {
		t.Fatalf("guard installed:\n%s", got)
	}
	if err := Guard(context.Background(), "10.77.0.1", "", sys.run, quiet); err == nil {
		t.Fatal("invalid address accepted")
	}
}
