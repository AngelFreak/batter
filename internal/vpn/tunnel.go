package vpn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MaxProfiles caps the number of profiles (slots).
const MaxProfiles = 32

// killSwitchMetric ranks the unreachable default below any tunnel route.
const killSwitchMetric = "4294967295"

// ErrKillSwitch means a slot's traffic could not be fenced off, so it would
// bypass the tunnel. The tunnel stays down until it can be.
var ErrKillSwitch = errors.New("vpn kill switch not installed")

// checkerUID is slot 0's exit-IP checker uid (the gnirehtet relays' old
// uid range, kept so existing hosts' rules stay meaningful).
const checkerUID = 31416

// Slot identifies one profile's tunnel and routing. Everything is derived
// from it, so a profile keeps its mark, uid and interface for life.
type Slot int

// UID is the slot's exit-IP checker uid; its traffic is routed like the
// slot's phones' (see ExitIPChecker).
func (s Slot) UID() uint32 { return checkerUID + uint32(s) }

// Mark is the firewall mark the LAN firewall puts on the slot's phones'
// packets; it routes them to the slot's table. It equals the table number.
func (s Slot) Mark() uint32 { return 51820 + uint32(s) }

func (s Slot) iface() string { return "wg" + strconv.Itoa(int(s)) }

// table holds the slot's routes: the tunnel's AllowedIPs, then an
// unreachable default as the kill switch.
func (s Slot) table() string { return strconv.Itoa(51820 + int(s)) }

// priority puts the checker uid's rule ahead of the main table's (32766).
func (s Slot) priority() string { return strconv.Itoa(10000 + int(s)) }

// markPriority puts the phones' mark rule ahead of the LAN's catch-all
// (lan.catchAllPriority, 9900) and the main table.
func (s Slot) markPriority() string { return strconv.Itoa(9000 + int(s)) }

func (s Slot) mark() string { return strconv.FormatUint(uint64(s.Mark()), 10) }

func (s Slot) uidrange() string {
	uid := strconv.FormatUint(uint64(s.UID()), 10)
	return uid + "-" + uid
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

// Status is a tunnel's live state.
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

// checkerFirewall is the nftables ruleset confining every exit-IP checker
// uid (all slots) to WireGuard interfaces. Policy routing alone isn't
// enough: the kernel's local table is consulted before the uid rules, so a
// checker could reach the container's own addresses (Batter's backend and
// web app, 127.0.0.0/8) without any tunnel, and report a misleading
// result. The table is replaced in one transaction, so re-applying never
// opens a window. It also removes the gnirehtet version's batter_relay
// table.
func checkerFirewall() string {
	uids := fmt.Sprintf("%d-%d", Slot(0).UID(), Slot(MaxProfiles-1).UID())
	return "table inet batter_relay {}\n" +
		"delete table inet batter_relay\n" +
		"table inet batter_exitcheck {}\n" +
		"delete table inet batter_exitcheck\n" +
		"table inet batter_exitcheck {\n" +
		"\tchain output {\n" +
		"\t\ttype filter hook output priority 0; policy accept;\n" +
		"\t\tmeta skuid " + uids + " ct state established,related accept\n" +
		"\t\tmeta skuid " + uids + " oifname \"wg*\" accept\n" +
		"\t\tmeta skuid " + uids + " reject\n" +
		"\t}\n" +
		"}\n"
}

// installFirewall (re)loads checkerFirewall.
func (t *tunnels) installFirewall(ctx context.Context) error {
	if _, err := t.cmd(ctx, checkerFirewall(), "nft", "-f", "/dev/stdin"); err != nil {
		return fmt.Errorf("%w: exit-check firewall: %v", ErrKillSwitch, err)
	}
	return nil
}

// tunnels drives the system's tunnels and policy routing, one set per slot.
type tunnels struct {
	run    RunFunc
	logger *slog.Logger
}

// installKillSwitch sends the slot's phones (by mark) and checker uid to
// its own table, whose last resort is unreachable. It's idempotent and
// never removes anything, so re-applying a config doesn't open a window
// where the slot's traffic goes direct.
func (t *tunnels) installKillSwitch(ctx context.Context, s Slot) error {
	for _, v6 := range []bool{false, true} {
		err := t.ensureRule(ctx, v6, s)
		if err == nil {
			_, err = t.ip(ctx, v6, "route", "replace", "unreachable", "default",
				"table", s.table(), "metric", killSwitchMetric)
		}
		if err != nil {
			if v6 {
				// No IPv6 in the container means no IPv6 to leak.
				t.logger.Warn("vpn: IPv6 kill switch not installed", "slot", s, "error", err)
				continue
			}
			return fmt.Errorf("%w: %v", ErrKillSwitch, err)
		}
	}
	return nil
}

func (t *tunnels) ensureRule(ctx context.Context, v6 bool, s Slot) error {
	out, err := t.ip(ctx, v6, "rule", "show", "priority", s.markPriority())
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), fmt.Sprintf("fwmark %#x ", s.Mark())) {
		_, err = t.ip(ctx, v6, "rule", "add", "fwmark", s.mark(),
			"lookup", s.table(), "priority", s.markPriority())
		if err != nil {
			return err
		}
	}
	out, err = t.ip(ctx, v6, "rule", "show", "priority", s.priority())
	if err != nil {
		return err
	}
	if strings.Contains(string(out), "uidrange "+s.uidrange()) {
		return nil
	}
	_, err = t.ip(ctx, v6, "rule", "add", "uidrange", s.uidrange(),
		"lookup", s.table(), "priority", s.priority())
	return err
}

// bringUp (re)creates the slot's interface and routes the tunnel's
// AllowedIPs in its table. Deleting the old interface drops its routes,
// leaving the kill switch in charge until the new one is up.
func (t *tunnels) bringUp(ctx context.Context, s Slot, cfg *Config) error {
	iface := s.iface()
	t.takeDown(ctx, s)
	if _, err := t.cmd(ctx, "", "ip", "link", "add", iface, "type", "wireguard"); err != nil {
		return err
	}
	if _, err := t.cmd(ctx, cfg.Setconf(), "wg", "setconf", iface, "/dev/stdin"); err != nil {
		return err
	}
	for _, a := range cfg.Addresses {
		if _, err := t.cmd(ctx, "", "ip", "address", "add", a, "dev", iface); err != nil && !isV6(a) {
			return err
		} else if err != nil {
			t.logger.Warn("vpn: IPv6 address not added", "iface", iface, "address", a, "error", err)
		}
	}
	mtu := cfg.MTU
	if mtu == 0 {
		mtu = 1420
	}
	if _, err := t.cmd(ctx, "", "ip", "link", "set", iface, "mtu", strconv.Itoa(mtu), "up"); err != nil {
		return err
	}
	t.relaxReversePathFilter(ctx, iface)
	for _, p := range cfg.Peers {
		for _, dst := range p.AllowedIPs {
			_, err := t.ip(ctx, isV6(dst), "route", "replace", dst, "dev", iface, "table", s.table())
			if err != nil && !isV6(dst) {
				return err
			} else if err != nil {
				t.logger.Warn("vpn: IPv6 route not added", "iface", iface, "dst", dst, "error", err)
			}
		}
	}
	return nil
}

// takeDown removes the slot's interface (and with it the tunnel routes);
// the kill switch stays.
func (t *tunnels) takeDown(ctx context.Context, s Slot) {
	_, _ = t.cmd(ctx, "", "ip", "link", "del", s.iface())
}

// relaxReversePathFilter makes replies arriving on iface acceptable. Strict
// reverse-path filtering checks the source against the main table (the
// check doesn't carry the mark or uid), which routes the internet via eth0,
// so it would drop every reply. Loose mode only needs some route back.
func (t *tunnels) relaxReversePathFilter(ctx context.Context, iface string) {
	if out, err := t.cmd(ctx, "", "sysctl", "-n", "net.ipv4.conf.all.rp_filter"); err == nil && strings.TrimSpace(string(out)) == "1" {
		if _, err := t.cmd(ctx, "", "sysctl", "-w", "net.ipv4.conf.all.rp_filter=2"); err != nil {
			t.logger.Warn("vpn: couldn't relax rp_filter", "error", err)
		}
	}
	if _, err := t.cmd(ctx, "", "sysctl", "-w", "net.ipv4.conf."+iface+".rp_filter=2"); err != nil {
		t.logger.Warn("vpn: couldn't relax rp_filter on "+iface, "error", err)
	}
}

// teardown removes the slot's tunnel and routing entirely; missing pieces
// are fine.
func (t *tunnels) teardown(ctx context.Context, s Slot) {
	for _, v6 := range []bool{false, true} {
		// Delete every copy, in case one was ever added twice.
		for range 10 {
			if _, err := t.ip(ctx, v6, "rule", "del", "uidrange", s.uidrange(), "lookup", s.table()); err != nil {
				break
			}
		}
		for range 10 {
			if _, err := t.ip(ctx, v6, "rule", "del", "fwmark", s.mark(), "lookup", s.table()); err != nil {
				break
			}
		}
		_, _ = t.ip(ctx, v6, "route", "flush", "table", s.table())
	}
	t.takeDown(ctx, s)
}

// status parses `wg show <iface> dump`. Its first line carries the private
// key and is skipped.
func (t *tunnels) status(ctx context.Context, s Slot) *Status {
	st := &Status{Peers: []PeerStatus{}}
	if out, err := t.cmd(ctx, "", "ip", "-o", "link", "show", s.iface()); err == nil {
		if i := strings.IndexByte(string(out), '<'); i >= 0 {
			flags, _, _ := strings.Cut(string(out[i+1:]), ">")
			st.Up = strings.Contains(","+flags+",", ",UP,")
		}
	}
	out, err := t.cmd(ctx, "", "wg", "show", s.iface(), "dump")
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
			tm := time.Unix(ts, 0).UTC()
			p.LatestHandshake = &tm
		}
		p.RxBytes, _ = strconv.ParseInt(f[5], 10, 64)
		p.TxBytes, _ = strconv.ParseInt(f[6], 10, 64)
		st.Peers = append(st.Peers, p)
	}
	return st
}

func (t *tunnels) cmd(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	run := t.run
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
func (t *tunnels) ip(ctx context.Context, v6 bool, args ...string) ([]byte, error) {
	if v6 {
		args = append([]string{"-6"}, args...)
	}
	return t.cmd(ctx, "", "ip", args...)
}

func isV6(prefix string) bool {
	return strings.Contains(prefix, ":")
}
