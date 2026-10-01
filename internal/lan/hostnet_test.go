package lan

import (
	"context"
	"strings"
	"testing"
)

// The box as `ip -j` shows it (trimmed to the fields Batter reads).
const (
	boxLinks = `[
 {"ifindex":1,"ifname":"lo","flags":["LOOPBACK","UP","LOWER_UP"],"link_type":"loopback","address":"00:00:00:00:00:00"},
 {"ifindex":2,"ifname":"eth0","flags":["BROADCAST","MULTICAST","UP","LOWER_UP"],"link_type":"ether","address":"00:11:22:33:44:01","parentbus":"pci","parentdev":"0000:01:00.0"},
 {"ifindex":3,"ifname":"enp2s0","flags":["BROADCAST","MULTICAST"],"link_type":"ether","address":"00:11:22:33:44:02","parentbus":"pci","parentdev":"0000:02:00.0"},
 {"ifindex":4,"ifname":"enx001","flags":["BROADCAST","MULTICAST","UP","LOWER_UP"],"link_type":"ether","address":"00:11:22:33:44:03","parentbus":"usb","parentdev":"2-1:1.0"},
 {"ifindex":5,"ifname":"wlp1s0","flags":["BROADCAST","MULTICAST","UP","LOWER_UP"],"link_type":"ether","address":"00:11:22:33:44:04","parentbus":"pci","parentdev":"0000:03:00.0"},
 {"ifindex":6,"ifname":"docker0","flags":["BROADCAST","MULTICAST","UP","LOWER_UP"],"link_type":"ether","address":"02:42:00:00:00:01","linkinfo":{"info_kind":"bridge"}},
 {"ifindex":7,"ifname":"veth1a2b3c","flags":["BROADCAST","MULTICAST","UP","LOWER_UP"],"link_type":"ether","address":"02:42:00:00:00:02","master":"docker0","linkinfo":{"info_kind":"veth"}},
 {"ifindex":8,"ifname":"wt0","flags":["POINTOPOINT","NOARP","UP","LOWER_UP"],"link_type":"none","linkinfo":{"info_kind":"wireguard"}},
 {"ifindex":9,"ifname":"enp3s0","flags":["BROADCAST","MULTICAST","UP","LOWER_UP"],"link_type":"ether","address":"00:11:22:33:44:05","parentbus":"pci","parentdev":"0000:04:00.0"},
 {"ifindex":10,"ifname":"enp4s0","flags":["BROADCAST","MULTICAST","UP","LOWER_UP","SLAVE"],"link_type":"ether","address":"00:11:22:33:44:06","master":"bond0","parentbus":"pci","parentdev":"0000:05:00.0"},
 {"ifindex":11,"ifname":"bond0","flags":["BROADCAST","MULTICAST","MASTER","UP"],"link_type":"ether","address":"00:11:22:33:44:06","linkinfo":{"info_kind":"bond"}},
 {"ifindex":12,"ifname":"nic2","flags":["BROADCAST","MULTICAST"],"link_type":"ether","address":"02:00:00:00:77:02","linkinfo":{"info_kind":"veth"}},
 {"ifindex":13,"ifname":"enp5s0","flags":["BROADCAST","MULTICAST","UP","LOWER_UP"],"link_type":"ether","address":"00:11:22:33:44:07","parentbus":"pci","parentdev":"0000:06:00.0"}
]`
	boxAddrs = `[
 {"ifname":"lo","addr_info":[{"family":"inet","local":"127.0.0.1","prefixlen":8,"scope":"host"}]},
 {"ifname":"eth0","addr_info":[{"family":"inet","local":"192.168.1.76","prefixlen":24,"scope":"global"},{"family":"inet6","local":"fe80::1","prefixlen":64,"scope":"link"}]},
 {"ifname":"enp2s0","addr_info":[]},
 {"ifname":"enx001","addr_info":[{"family":"inet6","local":"fe80::3","prefixlen":64,"scope":"link"}]},
 {"ifname":"enp3s0","addr_info":[{"family":"inet","local":"10.10.0.5","prefixlen":24,"scope":"global"}]},
 {"ifname":"enp5s0","addr_info":[{"family":"inet6","local":"fe80::7","prefixlen":64,"scope":"link"}]}
]`
	boxRoutes4 = `[
 {"type":"unicast","dst":"default","gateway":"192.168.1.1","dev":"eth0"},
 {"dst":"192.168.1.0/24","dev":"eth0","prefsrc":"192.168.1.76"},
 {"dst":"10.10.0.0/24","dev":"enp3s0"},
 {"type":"local","dst":"192.168.1.76","dev":"eth0","table":"local"}
]`
	boxRoutes6 = `[
 {"dst":"fe80::/64","dev":"eth0"},
 {"dst":"fe80::/64","dev":"enx001"},
 {"dst":"2001:db8::/64","dev":"enp5s0"},
 {"type":"multicast","dst":"ff00::/8","dev":"enx001","table":"local"},
 {"type":"local","dst":"fe80::3","dev":"enx001","table":"local"}
]`
	boxSysfs = "lo  0\neth0 igb 0\nenp2s0 r8169 0\nenx001 r8152 0\nwlp1s0 iwlwifi 1\ndocker0  0\nenp3s0 e1000e 0\nenp4s0 igb 0\nenp5s0 igb 0\nnic2  0\n"
)

// fakeBox answers the box-side commands Batter runs.
func fakeBox(sys *fakeSystem) {
	if sys.out == nil {
		sys.out = map[string]string{}
	}
	ns := "nsenter --net=/proc/1/ns/net "
	sys.out[ns+"ip -j -d link show"] = boxLinks
	sys.out[ns+"ip -j addr show"] = boxAddrs
	sys.out[ns+"ip -j -4 route show table all"] = boxRoutes4
	sys.out[ns+"ip -j -6 route show table all"] = boxRoutes6
	sys.out[ns+"unshare -m sh -c"] = boxSysfs
}

func newHostNet(sys *fakeSystem) *HostNet {
	return &HostNet{Run: sys.run, NS: "/proc/1/ns/net", PID: 4242}
}

func byName(nics []HostNIC) map[string]HostNIC {
	m := map[string]HostNIC{}
	for _, n := range nics {
		m[n.Name] = n
	}
	return m
}

// The picker lists the box's physical wired NICs only, each with whether it
// can be the phone network's port and, if not, why.
func TestListOffersOnlyFreePhysicalWiredNICs(t *testing.T) {
	sys := &fakeSystem{}
	fakeBox(sys)
	nics, err := newHostNet(sys).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := byName(nics)
	var names []string
	for _, n := range nics {
		names = append(names, n.Name)
	}
	// Never offered: loopback, Wi-Fi, bridges, Docker's veths, VPN
	// interfaces, bonds, virtual devices.
	for _, hidden := range []string{"lo", "wlp1s0", "docker0", "veth1a2b3c", "wt0", "bond0", "nic2"} {
		if _, ok := got[hidden]; ok {
			t.Errorf("%s offered; offered: %v", hidden, names)
		}
	}
	for name, want := range map[string]struct {
		usable bool
		reason string
	}{
		"enp2s0": {true, ""},
		"enx001": {true, ""}, // link-local IPv6 only: fine
		"eth0":   {false, "default route"},
		"enp3s0": {false, "addresses"},
		"enp4s0": {false, "bond0"},
		"enp5s0": {false, "routes"},
	} {
		n, ok := got[name]
		if !ok {
			t.Errorf("%s not offered; offered: %v", name, names)
			continue
		}
		if n.Usable != want.usable || !strings.Contains(n.Reason, want.reason) {
			t.Errorf("%s: usable=%v reason=%q, want usable=%v reason containing %q", name, n.Usable, n.Reason, want.usable, want.reason)
		}
	}
	if n := got["enx001"]; n.MAC != "00:11:22:33:44:03" || n.Driver != "r8152" || n.Bus != "usb" || !n.Up {
		t.Errorf("enx001 details: %+v", n)
	}
	if n := got["enp2s0"]; n.Up || n.Driver != "r8169" {
		t.Errorf("enp2s0 details: %+v", n)
	}
}

// Tests stand a veth in for a physical NIC; production never offers one.
func TestVethStandsInForANICOnlyWhenAllowed(t *testing.T) {
	sys := &fakeSystem{}
	fakeBox(sys)
	h := newHostNet(sys)
	h.AllowVeth = true
	nics, err := h.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := byName(nics)
	if n, ok := got["nic2"]; !ok || !n.Usable {
		t.Fatalf("nic2 with AllowVeth: %+v", n)
	}
	if _, ok := got["veth1a2b3c"]; ok {
		t.Fatal("Docker's veth offered")
	}
}

// Adopting moves the NIC, by MAC, from the box into Batter's namespace in
// one step, renamed to phonelan (the name the firewall already fences).
func TestAdoptMovesTheNICByMACIntoBattersNamespace(t *testing.T) {
	sys := &fakeSystem{}
	fakeBox(sys)
	nic, err := newHostNet(sys).Adopt(context.Background(), "00:11:22:33:44:02")
	if err != nil {
		t.Fatal(err)
	}
	if nic.Name != "enp2s0" {
		t.Fatalf("adopted %+v", nic)
	}
	sys.index(t, "nsenter --net=/proc/1/ns/net ip link set dev enp2s0 netns 4242 name phonelan")
}

func TestAdoptRefusesUnusableAndUnknownNICs(t *testing.T) {
	for mac, want := range map[string]string{
		"00:11:22:33:44:01": "default route", // eth0
		"00:11:22:33:44:05": "addresses",     // enp3s0
		"02:42:00:00:00:02": "not found",     // Docker's veth: never offered
		"00:11:22:33:44:99": "not found",
	} {
		sys := &fakeSystem{}
		fakeBox(sys)
		_, err := newHostNet(sys).Adopt(context.Background(), mac)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("adopt %s: %v, want %q", mac, err, want)
		}
		if sys.has("nsenter --net=/proc/1/ns/net ip link set") {
			t.Errorf("adopt %s moved something", mac)
		}
	}
}

// Releasing hands the NIC back to the box down, unaddressed and under its
// own name.
func TestReleaseReturnsTheNICDownUnderItsName(t *testing.T) {
	sys := &fakeSystem{}
	if err := newHostNet(sys).Release(context.Background(), "enp2s0"); err != nil {
		t.Fatal(err)
	}
	down := sys.index(t, "ip link set dev phonelan down")
	flush := sys.index(t, "ip address flush dev phonelan")
	move := sys.index(t, "ip link set dev phonelan netns /proc/1/ns/net name enp2s0")
	if down > move || flush > move {
		t.Fatalf("moved before it was down and unaddressed:\n  %s", strings.Join(sys.log, "\n  "))
	}
}

// If the box took the name meanwhile, the NIC still goes back (as phonelan).
func TestReleaseFallsBackToKeepingTheName(t *testing.T) {
	sys := &fakeSystem{fail: "name enp2s0"}
	if err := newHostNet(sys).Release(context.Background(), "enp2s0"); err != nil {
		t.Fatal(err)
	}
	sys.index(t, "ip link set dev phonelan netns /proc/1/ns/net")
	last := sys.log[len(sys.log)-1]
	if last != "ip link set dev phonelan netns /proc/1/ns/net" {
		t.Fatalf("last command %q", last)
	}
}

// Without the box's namespace (no pid: host, or not in a container) the
// feature is unavailable rather than acting on Batter's own interfaces.
func TestHostNetUnavailableWhenNamespaceIsOurOwn(t *testing.T) {
	h := &HostNet{NS: "/proc/self/ns/net"}
	if err := h.Available(); err == nil {
		t.Fatal("our own namespace accepted as the box's")
	}
	h = &HostNet{NS: "/nonexistent/ns"}
	if err := h.Available(); err == nil {
		t.Fatal("missing namespace accepted")
	}
}

func TestListFailsWhenTheBoxCantBeRead(t *testing.T) {
	sys := &fakeSystem{fail: "ip -j -d link"}
	fakeBox(sys)
	if _, err := newHostNet(sys).List(context.Background()); err == nil {
		t.Fatal("no error")
	}
}
