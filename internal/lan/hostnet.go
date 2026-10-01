package lan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Iface is the phone network's port inside Batter's network namespace: the
// box NIC chosen for it, moved in and renamed. The firewall fences this
// name before the NIC arrives.
const Iface = "phonelan"

// HostNIC is one of the box's network interfaces, as offered for the phone
// network.
type HostNIC struct {
	Name    string `json:"name"`
	MAC     string `json:"mac"`
	Up      bool   `json:"up"`
	Carrier bool   `json:"carrier"`
	Driver  string `json:"driver,omitempty"`
	Bus     string `json:"bus,omitempty"`
	// Usable says whether it may become the phone network's port; Reason
	// says why not.
	Usable bool   `json:"usable"`
	Reason string `json:"reason,omitempty"`
	// InUse marks the port Batter has taken (it's in Batter's namespace,
	// not on the box).
	InUse bool `json:"in_use,omitempty"`
}

// HostNet lists the box's NICs and moves the phone network's port between
// the box's network namespace and Batter's. It reaches the box's namespace
// through NS, the box's init process's (the container runs with pid: host),
// and runs only `ip` there: nothing is ever added to the box's networking.
type HostNet struct {
	Run RunFunc // nil = Exec
	// NS is the box's network namespace: /proc/1/ns/net under pid: host.
	NS string
	// PID is Batter's process id as the box sees it (os.Getpid() under
	// pid: host); a NIC moved to it lands in Batter's namespace.
	PID int
	// AllowVeth lets a veth stand in for a physical NIC (integration tests
	// only; set by BATTER_LAN_ALLOW_VETH=1).
	AllowVeth bool
}

// ErrNoHostNet means Batter can't see the box's network namespace (the
// container lacks pid: host, or Batter isn't in a container).
var ErrNoHostNet = errors.New("the box's network interfaces aren't visible to Batter (the container needs pid: host; see docs/DEPLOY.md)")

// Available reports whether NS is reachable and is not Batter's own
// namespace (which it would be without pid: host, where /proc/1 is the
// container's own init).
func (h *HostNet) Available() error {
	box, err := nsInode(h.NS)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoHostNet, err)
	}
	self, err := nsInode("/proc/self/ns/net")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoHostNet, err)
	}
	if box == self {
		return ErrNoHostNet
	}
	return nil
}

func nsInode(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return st.Ino, nil
}

// excludedPrefixes are interface names never offered: container, VPN and
// virtual-switch plumbing.
var excludedPrefixes = []string{
	"docker", "br-", "veth", "virbr", "vnet", "lxc", "cali", "cni", "flannel", "kube",
	"wg", "wt", "netbird", "tailscale", "zt", "tun", "tap", "vxlan", Iface,
}

type ipLink struct {
	Name      string   `json:"ifname"`
	Flags     []string `json:"flags"`
	LinkType  string   `json:"link_type"`
	Address   string   `json:"address"`
	Master    string   `json:"master"`
	ParentBus string   `json:"parentbus"`
	LinkInfo  *struct {
		Kind string `json:"info_kind"`
	} `json:"linkinfo"`
}

type ipAddrs struct {
	Name  string `json:"ifname"`
	Addrs []struct {
		Family string `json:"family"`
		Local  string `json:"local"`
		Prefix int    `json:"prefixlen"`
		Scope  string `json:"scope"`
	} `json:"addr_info"`
}

type ipRoute struct {
	Type  string `json:"type"`
	Dst   string `json:"dst"`
	Dev   string `json:"dev"`
	Table string `json:"table"`
}

// List returns the box's physical wired NICs, each marked usable or not.
func (h *HostNet) List(ctx context.Context) ([]HostNIC, error) {
	var links []ipLink
	if err := h.boxJSON(ctx, &links, "ip", "-j", "-d", "link", "show"); err != nil {
		return nil, err
	}
	var addrs []ipAddrs
	if err := h.boxJSON(ctx, &addrs, "ip", "-j", "addr", "show"); err != nil {
		return nil, err
	}
	var routes []ipRoute
	for _, family := range []string{"-4", "-6"} {
		var rs []ipRoute
		if err := h.boxJSON(ctx, &rs, "ip", "-j", family, "route", "show", "table", "all"); err != nil {
			return nil, err
		}
		routes = append(routes, rs...)
	}
	sysfs := h.sysfs(ctx)

	var nics []HostNIC
	for _, l := range links {
		info := sysfs[l.Name]
		if !h.offered(l, info) {
			continue
		}
		n := HostNIC{
			Name:    l.Name,
			MAC:     strings.ToLower(l.Address),
			Up:      hasFlag(l.Flags, "UP"),
			Carrier: hasFlag(l.Flags, "LOWER_UP"),
			Driver:  info.driver,
			Bus:     l.ParentBus,
		}
		n.Reason = refusal(l, addrs, routes)
		n.Usable = n.Reason == ""
		nics = append(nics, n)
	}
	return nics, nil
}

// offered reports whether a box interface is a physical wired NIC (or, in
// tests, a veth standing in for one).
func (h *HostNet) offered(l ipLink, info sysfsInfo) bool {
	if hasFlag(l.Flags, "LOOPBACK") || l.LinkType != "ether" || info.wireless {
		return false
	}
	for _, p := range excludedPrefixes {
		if strings.HasPrefix(l.Name, p) {
			return false
		}
	}
	kind := ""
	if l.LinkInfo != nil {
		kind = l.LinkInfo.Kind
	}
	if h.AllowVeth && kind == "veth" {
		return true
	}
	return kind == "" && l.ParentBus != ""
}

// refusal says why a NIC can't be the phone network's port: anything the
// box uses it for would be cut off, or would put the box on the phones'
// network.
func refusal(l ipLink, addrs []ipAddrs, routes []ipRoute) string {
	if l.Master != "" {
		return fmt.Sprintf("it's part of %s on the box", l.Master)
	}
	for _, r := range routes {
		if r.Dev == l.Name && r.Dst == "default" {
			return "it carries the box's own network connection (default route)"
		}
	}
	for _, a := range addrs {
		if a.Name != l.Name {
			continue
		}
		var used []string
		for _, ai := range a.Addrs {
			if ai.Family == "inet6" && ai.Scope == "link" {
				continue // every up interface has one; it goes with the move
			}
			used = append(used, ai.Local+"/"+strconv.Itoa(ai.Prefix))
		}
		if len(used) > 0 {
			return "it has addresses on the box (" + strings.Join(used, ", ") + "); remove them, and whatever configures them, first"
		}
	}
	for _, r := range routes {
		if r.Dev != l.Name || r.Table == "local" || r.Type == "multicast" || r.Dst == "fe80::/64" {
			continue
		}
		return "the box has routes through it (" + r.Dst + ")"
	}
	return ""
}

type sysfsInfo struct {
	driver   string
	wireless bool
}

// sysfs reads each box interface's driver and whether it's wireless. The
// container's /sys shows its own interfaces, so a private mount namespace
// mounts a sysfs as seen from the box's network namespace. Best effort:
// without it drivers are blank.
func (h *HostNet) sysfs(ctx context.Context) map[string]sysfsInfo {
	const script = `mount -t sysfs sysfs /sys && cd /sys/class/net && for i in *; do ` +
		`d=$(readlink "$i/device/driver"); w=0; ` +
		`if [ -e "$i/wireless" ] || [ -e "$i/phy80211" ]; then w=1; fi; ` +
		`echo "$i ${d##*/} $w"; done`
	out, err := h.box(ctx, "unshare", "-m", "sh", "-c", script)
	info := map[string]sysfsInfo{}
	if err != nil {
		return info
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, " ")
		if len(f) == 3 && f[0] != "" {
			info[f[0]] = sysfsInfo{driver: f[1], wireless: f[2] == "1"}
		}
	}
	return info
}

// Find returns the box NIC with mac, if it's offered.
func (h *HostNet) Find(ctx context.Context, mac string) (HostNIC, bool, error) {
	nics, err := h.List(ctx)
	if err != nil {
		return HostNIC{}, false, err
	}
	for _, n := range nics {
		if n.MAC == strings.ToLower(mac) {
			return n, true, nil
		}
	}
	return HostNIC{}, false, nil
}

// Adopt moves the box NIC with mac into Batter's namespace as Iface. The
// move takes the NIC down; it only comes up once Batter configures it.
func (h *HostNet) Adopt(ctx context.Context, mac string) (HostNIC, error) {
	n, ok, err := h.Find(ctx, mac)
	if err != nil {
		return HostNIC{}, err
	}
	if !ok {
		return HostNIC{}, fmt.Errorf("network port %s not found on the box", mac)
	}
	if !n.Usable {
		return HostNIC{}, fmt.Errorf("%s can't be the phone network's port: %s", n.Name, n.Reason)
	}
	if _, err := h.box(ctx, "ip", "link", "set", "dev", n.Name, "netns", strconv.Itoa(h.PID), "name", Iface); err != nil {
		return HostNIC{}, err
	}
	return n, nil
}

// Release gives the phone network's port back to the box: down, without
// Batter's address, under its own name (or as Iface if the box has reused
// the name meanwhile).
func (h *HostNet) Release(ctx context.Context, name string) error {
	if _, err := h.cmd(ctx, "", "ip", "link", "set", "dev", Iface, "down"); err != nil {
		return err
	}
	if _, err := h.cmd(ctx, "", "ip", "address", "flush", "dev", Iface); err != nil {
		return err
	}
	if name != "" {
		if _, err := h.cmd(ctx, "", "ip", "link", "set", "dev", Iface, "netns", h.NS, "name", name); err == nil {
			return nil
		}
	}
	_, err := h.cmd(ctx, "", "ip", "link", "set", "dev", Iface, "netns", h.NS)
	return err
}

// IsUp reports whether the box has the NIC with mac up (something on the
// box manages it).
func (h *HostNet) IsUp(ctx context.Context, mac string) (up, found bool, err error) {
	n, ok, err := h.Find(ctx, mac)
	return n.Up, ok, err
}

// box runs a command in the box's network namespace.
func (h *HostNet) box(ctx context.Context, name string, args ...string) ([]byte, error) {
	return h.cmd(ctx, "", "nsenter", append([]string{"--net=" + h.NS, name}, args...)...)
}

func (h *HostNet) boxJSON(ctx context.Context, v any, name string, args ...string) error {
	out, err := h.box(ctx, name, args...)
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return nil
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	return nil
}

func (h *HostNet) cmd(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	run := h.Run
	if run == nil {
		run = Exec
	}
	out, err := run(ctx, stdin, name, args...)
	if err != nil {
		return out, fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func hasFlag(flags []string, f string) bool {
	for _, x := range flags {
		if x == f {
			return true
		}
	}
	return false
}

// AllowVethFromEnv reads BATTER_LAN_ALLOW_VETH (integration tests).
func AllowVethFromEnv() bool { return os.Getenv("BATTER_LAN_ALLOW_VETH") == "1" }
