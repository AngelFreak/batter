package lan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os/exec"
	"strings"
)

// Routing rules. Each phone with a profile has `from <ip> iif <lan> lookup
// <its profile's table>` at phonePriority. The catch-all makes anything
// else forwarded from the LAN unreachable; it comes before the main table
// (32766), so a phone without a working route never takes the container's
// own default route. Routing by source address (rather than by a firewall
// mark) also keeps reverse-path checks of replies to the phone working:
// those look the phone's address up as if it came from the LAN.
const (
	phonePriority    = "9000"
	catchAllPriority = "9900"
)

// Phone is one adapter on the LAN as the firewall sees it.
type Phone struct {
	IP  netip.Addr
	MAC string
	// Table is its VPN profile's routing table; 0 = no profile, so nothing
	// is routed or forwarded for it.
	Table int
	// DNS is the profile's DNS server, which the phone's DNS queries are
	// redirected to; invalid = none (queries to Batter are dropped).
	DNS netip.Addr
}

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

// Ruleset is the LAN's nftables table: phones reach only their profile's
// tunnel (and through it their profile's DNS server), and of Batter itself
// only DHCP and replies on connections Batter opened (adb). No IPv6 is
// forwarded or accepted from the LAN. The table is replaced in one
// transaction, so re-applying never opens a window.
func Ruleset(iface string, n Network, phones []Phone) string {
	lan := fmt.Sprintf("%q", iface)
	var allow, dns strings.Builder
	for _, p := range phones {
		if p.Table == 0 {
			continue
		}
		fmt.Fprintf(&allow, "\t\tiifname %s oifname \"wg*\" ip saddr %s ether saddr %s accept\n", lan, p.IP, p.MAC)
		who := fmt.Sprintf("iifname %s ip saddr %s ether saddr %s", lan, p.IP, p.MAC)
		if p.DNS.Is4() {
			fmt.Fprintf(&dns, "\t\t%s meta l4proto { tcp, udp } th dport 53 dnat ip to %s\n", who, p.DNS)
		}
	}
	return "table inet batter_lan {}\n" +
		"delete table inet batter_lan\n" +
		"table inet batter_lan {\n" +
		"\tchain dns {\n" +
		"\t\ttype nat hook prerouting priority dstnat; policy accept;\n" +
		dns.String() +
		"\t}\n" +
		"\tchain input {\n" +
		"\t\ttype filter hook input priority filter; policy accept;\n" +
		"\t\tiifname " + lan + " meta nfproto ipv6 drop\n" +
		"\t\tiifname " + lan + " ct state established,related accept\n" +
		"\t\tiifname " + lan + " udp sport 68 udp dport 67 accept\n" +
		"\t\tiifname " + lan + " drop\n" +
		"\t}\n" +
		"\tchain forward {\n" +
		"\t\ttype filter hook forward priority filter; policy accept;\n" +
		"\t\tiifname " + lan + " meta nfproto ipv6 drop\n" +
		"\t\toifname " + lan + " meta nfproto ipv6 drop\n" +
		"\t\toifname \"wg*\" tcp flags syn / syn,rst tcp option maxseg size set rt mtu\n" +
		allow.String() +
		"\t\tiifname " + lan + " drop\n" +
		"\t\tiifname \"wg*\" oifname " + lan + " ct state established,related accept\n" +
		"\t\toifname " + lan + " drop\n" +
		"\t}\n" +
		"\tchain postrouting {\n" +
		"\t\ttype nat hook postrouting priority srcnat; policy accept;\n" +
		"\t\tip saddr " + n.Prefix.String() + " oifname \"wg*\" masquerade\n" +
		"\t}\n" +
		"}\n"
}

// Firewall installs the LAN's routing fence and nftables table, and
// configures the port.
type Firewall struct {
	Iface  string
	Net    Network
	Run    RunFunc // nil = Exec
	Logger *slog.Logger
}

// Fence installs the routing catch-all, the nft table and the phones'
// routes. They match the port by name, so they hold whether or not the NIC
// is in Batter's namespace yet; Batter fences before it moves a NIC in.
func (f *Firewall) Fence(ctx context.Context, phones []Phone) error {
	if err := f.ensureCatchAll(ctx); err != nil {
		return err
	}
	if _, err := f.cmd(ctx, Ruleset(f.Iface, f.Net, phones), "nft", "-f", "/dev/stdin"); err != nil {
		return err
	}
	return f.syncRoutes(ctx, phones)
}

// Up configures the port once it is in Batter's namespace and fenced: no
// IPv6 (so Batter has no link-local address there), Batter's address, link
// up, and only then forwarding.
func (f *Firewall) Up(ctx context.Context) error {
	if _, err := f.cmd(ctx, "", "sysctl", "-w", "net.ipv6.conf."+f.Iface+".disable_ipv6=1"); err != nil {
		return err
	}
	prefix := netip.PrefixFrom(f.Net.Addr, f.Net.Prefix.Bits()).String()
	if _, err := f.cmd(ctx, "", "ip", "address", "replace", prefix, "dev", f.Iface); err != nil {
		return err
	}
	if _, err := f.cmd(ctx, "", "ip", "link", "set", "dev", f.Iface, "up"); err != nil {
		return err
	}
	_, err := f.cmd(ctx, "", "sysctl", "-w", "net.ipv4.ip_forward=1")
	return err
}

// Down takes the port down (fail closed after a fence failure).
func (f *Firewall) Down(ctx context.Context) error {
	_, err := f.cmd(ctx, "", "ip", "link", "set", "dev", f.Iface, "down")
	return err
}

// Remove turns forwarding off and deletes the table and every LAN rule:
// with the phone network off, Batter's namespace is as without it.
func (f *Firewall) Remove(ctx context.Context) error {
	var errs []error
	if _, err := f.cmd(ctx, "", "sysctl", "-w", "net.ipv4.ip_forward=0"); err != nil {
		errs = append(errs, err)
	}
	if _, err := f.cmd(ctx, "table inet batter_lan {}\ndelete table inet batter_lan\n", "nft", "-f", "/dev/stdin"); err != nil {
		errs = append(errs, err)
	}
	if err := f.syncRoutes(ctx, nil); err != nil {
		errs = append(errs, err)
	}
	if out, err := f.cmd(ctx, "", "ip", "rule", "show", "priority", catchAllPriority); err != nil {
		errs = append(errs, err)
	} else if hasCatchAll(string(out), f.Iface) {
		if _, err := f.cmd(ctx, "", "ip", "rule", "del", "iif", f.Iface, "unreachable", "priority", catchAllPriority); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// syncRoutes makes the phones' routing rules match phones: stale ones are
// removed before new ones are added, so a phone switching profile is never
// routed two ways (the catch-all holds it in between).
func (f *Firewall) syncRoutes(ctx context.Context, phones []Phone) error {
	want := map[string]bool{}
	for _, p := range phones {
		if p.Table != 0 {
			want[fmt.Sprintf("from %s iif %s lookup %d", p.IP, f.Iface, p.Table)] = true
		}
	}
	out, err := f.cmd(ctx, "", "ip", "rule", "show", "priority", phonePriority)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		_, rule, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		rule = normalizeRule(rule)
		have[rule] = true
		if !want[rule] {
			args := append([]string{"rule", "del"}, strings.Fields(rule)...)
			if _, err := f.cmd(ctx, "", "ip", append(args, "priority", phonePriority)...); err != nil {
				return err
			}
		}
	}
	for rule := range want {
		if !have[rule] {
			args := append([]string{"rule", "add"}, strings.Fields(rule)...)
			if _, err := f.cmd(ctx, "", "ip", append(args, "priority", phonePriority)...); err != nil {
				return err
			}
		}
	}
	return nil
}

// normalizeRule turns an `ip rule show` line's rule into the words `ip rule
// add` takes. A rule on an interface that isn't in the namespace (the port
// before Batter takes it) shows "[detached]".
func normalizeRule(rule string) string {
	var words []string
	for _, w := range strings.Fields(rule) {
		if w != "[detached]" {
			words = append(words, w)
		}
	}
	return strings.Join(words, " ")
}

func hasCatchAll(rules, iface string) bool {
	for _, line := range strings.Split(rules, "\n") {
		if strings.Contains(normalizeRule(line), "iif "+iface+" unreachable") {
			return true
		}
	}
	return false
}

func (f *Firewall) ensureCatchAll(ctx context.Context) error {
	out, err := f.cmd(ctx, "", "ip", "rule", "show", "priority", catchAllPriority)
	if err != nil {
		return err
	}
	if hasCatchAll(string(out), f.Iface) {
		return nil
	}
	_, err = f.cmd(ctx, "", "ip", "rule", "add", "iif", f.Iface, "unreachable", "priority", catchAllPriority)
	return err
}

func (f *Firewall) cmd(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	run := f.Run
	if run == nil {
		run = Exec
	}
	out, err := run(ctx, stdin, name, args...)
	if err != nil {
		return out, fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

// GuardSubcommand is the batter argument that runs Guard; the container
// runs it before starting the web app.
const GuardSubcommand = "lan-guard"

// Guard fences the phone network's port with no phones allowed anywhere if
// the port is already in Batter's namespace when the container starts (it
// normally isn't: a stopped container's NIC returns to the box). Batter
// itself fences before it moves a NIC in.
func Guard(ctx context.Context, run RunFunc, logger *slog.Logger) error {
	n, err := ParseNetwork(DefaultAddr, DefaultPool)
	if err != nil {
		return err
	}
	fw := &Firewall{Iface: Iface, Net: n, Run: run, Logger: logger}
	if _, err := fw.cmd(ctx, "", "ip", "link", "show", "dev", Iface); err != nil {
		return nil // no port: nothing to fence
	}
	return fw.Fence(ctx, nil)
}
