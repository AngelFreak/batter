package lan

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os/exec"
	"strings"
)

// catchAllPriority is the LAN's last-resort routing rule: anything
// forwarded from the LAN that no profile's mark rule (9000+slot) claimed is
// unreachable. It comes before the main table (32766), so a phone without a
// working profile never takes the container's own default route.
const catchAllPriority = "9900"

// Phone is one adapter on the LAN as the firewall sees it.
type Phone struct {
	IP  netip.Addr
	MAC string
	// Mark routes the phone's packets to its VPN profile's table; 0 = no
	// profile, so nothing is forwarded for it.
	Mark uint32
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
	var mark, dns strings.Builder
	for _, p := range phones {
		if p.Mark == 0 {
			continue
		}
		who := fmt.Sprintf("iifname %s ip saddr %s ether saddr %s", lan, p.IP, p.MAC)
		fmt.Fprintf(&mark, "\t\t%s meta mark set %d\n", who, p.Mark)
		if p.DNS.Is4() {
			fmt.Fprintf(&dns, "\t\t%s meta l4proto { tcp, udp } th dport 53 dnat ip to %s\n", who, p.DNS)
		}
	}
	return "table inet batter_lan {}\n" +
		"delete table inet batter_lan\n" +
		"table inet batter_lan {\n" +
		"\tchain mark {\n" +
		"\t\ttype filter hook prerouting priority mangle; policy accept;\n" +
		mark.String() +
		"\t}\n" +
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
		"\t\tiifname " + lan + " oifname \"wg*\" meta mark != 0 accept\n" +
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

// Firewall installs the LAN's routing fence and nftables table.
type Firewall struct {
	Iface  string
	Net    Network
	Run    RunFunc // nil = Exec
	Logger *slog.Logger
}

// Apply fences the LAN for phones and then turns on forwarding. If any of
// it fails the LAN interface is taken down, so the phones get nothing at
// all rather than a half-configured router.
func (f *Firewall) Apply(ctx context.Context, phones []Phone) error {
	err := f.apply(ctx, phones)
	if err != nil {
		if _, downErr := f.cmd(ctx, "", "ip", "link", "set", f.Iface, "down"); downErr != nil {
			f.Logger.Error("lan: couldn't take the phone LAN down after a failure", "error", downErr)
		}
		return fmt.Errorf("phone LAN firewall not installed (LAN taken down): %w", err)
	}
	return nil
}

func (f *Firewall) apply(ctx context.Context, phones []Phone) error {
	out, err := f.cmd(ctx, "", "ip", "-4", "route", "show", "default")
	if err != nil {
		return err
	}
	if usesDevice(string(out), f.Iface) {
		return fmt.Errorf("the container's default route goes through the phone LAN (%s): set gw_priority on the default network (Docker 28 or later)", strings.TrimSpace(string(out)))
	}
	if err := f.ensureCatchAll(ctx); err != nil {
		return err
	}
	if _, err := f.cmd(ctx, Ruleset(f.Iface, f.Net, phones), "nft", "-f", "/dev/stdin"); err != nil {
		return err
	}
	if _, err := f.cmd(ctx, "", "ip", "link", "set", f.Iface, "up"); err != nil {
		return err
	}
	_, err = f.cmd(ctx, "", "sysctl", "-w", "net.ipv4.ip_forward=1")
	return err
}

// usesDevice reports whether any route in `ip route` output goes out dev.
func usesDevice(routes, dev string) bool {
	f := strings.Fields(routes)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "dev" && f[i+1] == dev {
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
	if strings.Contains(string(out), "iif "+f.Iface+" unreachable") {
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
