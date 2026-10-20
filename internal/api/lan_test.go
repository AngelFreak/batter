package api_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/lan"
	"github.com/insomniacslk/dhcp/dhcpv4"
)

// The phone LAN through the real router, device manager, database, DHCP
// handler and controller, against the fake adb's phone. Only the system
// (nft, ip) and the TCP probe of the phone's adb port are simulated.

// recordingSystem stands in for nft/ip/sysctl, keeping the last ruleset
// and the phones' routing rules as `ip rule` would.
type recordingSystem struct {
	mu     sync.Mutex
	rules  string
	routes map[string]bool // "from <ip> iif phonelan lookup <table>"
}

func (s *recordingSystem) run(_ context.Context, stdin, name string, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := strings.Join(args, " ")
	switch {
	case name == "nft":
		s.rules = stdin
	case name == "ip" && cmd == "-4 route show default":
		return []byte("default via 172.20.0.1 dev eth0\n"), nil
	case name == "ip" && cmd == "rule show priority 9000":
		var out strings.Builder
		for r := range s.routes {
			out.WriteString("9000:\t" + r + " \n")
		}
		return []byte(out.String()), nil
	case name == "ip" && strings.HasPrefix(cmd, "rule add from "):
		if s.routes == nil {
			s.routes = map[string]bool{}
		}
		s.routes[strings.TrimSuffix(strings.TrimPrefix(cmd, "rule add "), " priority 9000")] = true
	case name == "ip" && strings.HasPrefix(cmd, "rule del from "):
		delete(s.routes, strings.TrimSuffix(strings.TrimPrefix(cmd, "rule del "), " priority 9000"))
	}
	return nil, nil
}

func (s *recordingSystem) ruleset() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rules
}

func (s *recordingSystem) routed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for r := range s.routes {
		out = append(out, r)
	}
	return out
}

type lanEnv struct {
	*testEnv
	ctl   *lan.Controller
	host  *boxNICs
	sys   *recordingSystem
	state string
}

const boxNICMAC = "02:00:00:00:77:02"

// boxNICs is the box with one free NIC for the phone network.
type boxNICs struct {
	mu     sync.Mutex
	nic    lan.HostNIC
	inside bool
}

func (b *boxNICs) Available() error { return nil }

func (b *boxNICs) List(context.Context) ([]lan.HostNIC, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inside {
		return nil, nil
	}
	return []lan.HostNIC{b.nic}, nil
}

func (b *boxNICs) Find(ctx context.Context, mac string) (lan.HostNIC, bool, error) {
	nics, _ := b.List(ctx)
	for _, n := range nics {
		if n.MAC == mac {
			return n, true, nil
		}
	}
	return lan.HostNIC{}, false, nil
}

func (b *boxNICs) Adopt(context.Context, string) (lan.HostNIC, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inside = true
	return b.nic, nil
}

func (b *boxNICs) Release(context.Context, string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inside = false
	return nil
}

func (b *boxNICs) Present(context.Context) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inside
}

// newLANEnv wires a real lan.Controller into the router. The fake phone's
// adb port answers when its state dir says adbd listens on the LAN.
func newLANEnv(t *testing.T) *lanEnv {
	t.Helper()
	state := useFakePhone(t)
	e := &lanEnv{sys: &recordingSystem{}, state: state}
	n, err := lan.ParseNetwork("10.77.0.1/24", "10.77.0.100-10.77.0.200")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.testEnv = newTestEnv(t, func(s *envSetup) {
		e.host = &boxNICs{nic: lan.HostNIC{Name: "enp2s0", MAC: boxNICMAC, Usable: true}}
		e.ctl = &lan.Controller{
			Net:      n,
			Leases:   &lan.Leases{DB: s.db, Net: n},
			Ports:    &lan.PortSetting{DB: s.db},
			Host:     e.host,
			Firewall: &lan.Firewall{Iface: lan.Iface, Net: n, Run: e.sys.run, Logger: logger},
			ADB:      s.dm.ADB(),
			Profiles: s.vpn,
			Logger:   logger,
			Dial: func(_ context.Context, addr string) error {
				b, _ := os.ReadFile(filepath.Join(state, "lan"))
				if strings.TrimSpace(string(b)) == addr {
					return nil
				}
				if _, err := os.Stat(filepath.Join(state, "unplugged")); err == nil {
					// The adapter is in (USB out) but adbd doesn't listen.
					return &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
				}
				return &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}
			},
		}
		s.lan = e.ctl
	})
	return e
}

// plugAdapter has the phone's adapter get its address over DHCP, as the
// phone does when the adapter is plugged in.
func (e *lanEnv) plugAdapter(t *testing.T, mac string) net.IP {
	t.Helper()
	hw, _ := net.ParseMAC(mac)
	srv := e.ctl.DHCPServer()
	conn := &sentConn{}
	bcast := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	disc, _ := dhcpv4.NewDiscovery(hw)
	srv.Handle(conn, bcast, disc)
	offer := conn.last(t)
	req, _ := dhcpv4.NewRequestFromOffer(offer)
	srv.Handle(conn, bcast, req)
	if ack := conn.last(t); ack.MessageType() != dhcpv4.MessageTypeAck {
		t.Fatalf("DHCP: %s", ack.MessageType())
	}
	return offer.YourIPAddr
}

type sentConn struct {
	net.PacketConn
	mu   sync.Mutex
	msgs []*dhcpv4.DHCPv4
}

func (c *sentConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	m, err := dhcpv4.FromBytes(b)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	c.msgs = append(c.msgs, m)
	c.mu.Unlock()
	return len(b), nil
}

func (c *sentConn) last(t *testing.T) *dhcpv4.DHCPv4 {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.msgs) == 0 {
		t.Fatal("no DHCP reply")
	}
	return c.msgs[len(c.msgs)-1]
}

func (e *lanEnv) device(t *testing.T) string {
	t.Helper()
	w := e.do(t, "admin", "GET", "/api/v1/devices/"+fakeSerial, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get device: %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func (e *lanEnv) setState(t *testing.T, name, content string, present bool) {
	t.Helper()
	path := filepath.Join(e.state, name)
	if !present {
		_ = os.Remove(path)
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The whole provisioning path: a USB phone is registered, switched to adb
// over TCP from the API, moved to its adapter, found on the LAN and bound
// to its lease; from then on it's driven over the LAN, its profile routes
// its adapter's traffic, a reboot surfaces as "needs USB re-provision",
// and back on USB it's driven over USB again.
func TestPhoneMovesFromUSBToTheLANAndBack(t *testing.T) {
	e := newLANEnv(t)
	ctx := context.Background()
	const mac = "02:00:00:00:00:0a"

	// The admin turns the phone network on, on the box's free NIC.
	if w := e.do(t, "admin", "PUT", "/api/v1/phone-network", `{"mac":"`+boxNICMAC+`"}`); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `"state":"active"`) {
		t.Fatalf("turn the phone network on: %d %s", w.Code, w.Body.String())
	}
	defer func() { _, _ = e.ctl.SetPort(context.Background(), "") }()
	if w := e.do(t, "admin", "POST", "/api/v1/devices", `{"serial":"`+fakeSerial+`"}`); w.Code != http.StatusCreated {
		t.Fatalf("register: %d", w.Code)
	}
	for _, user := range []string{"viewer", "manager"} {
		if w := e.do(t, user, "POST", "/api/v1/devices/ethernet/"+fakeSerial, ""); w.Code != http.StatusForbidden {
			t.Fatalf("%s may switch a phone to the LAN: %d", user, w.Code)
		}
	}
	if w := e.do(t, "operator", "POST", "/api/v1/devices/ethernet/"+fakeSerial, ""); w.Code != http.StatusOK {
		t.Fatalf("provision: %d %s", w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(filepath.Join(e.state, "tcpip")); string(b) != "5555" {
		t.Fatalf("adb not switched to TCP 5555 (got %q)", b)
	}
	calls := adbCalls(t, e.state)
	if !strings.Contains(strings.Join(calls, "\n"), "-s "+fakeSerial+" shell pm uninstall com.genymobile.gnirehtet") {
		t.Errorf("gnirehtet's client not removed while on USB: %q", calls)
	}

	// The user unplugs USB and plugs in the adapter.
	ip := e.plugAdapter(t, mac)
	if ip.String() != "10.77.0.100" {
		t.Fatalf("adapter got %s", ip)
	}
	moveFakePhoneToLAN(t, e.state)
	e.ctl.Pass(ctx)

	if dev := e.device(t); !strings.Contains(dev, `"status":"connected"`) || !strings.Contains(dev, `"connection":"lan"`) ||
		!strings.Contains(dev, `"lan_address":"10.77.0.100"`) {
		t.Fatalf("phone not found on the LAN: %s", dev)
	}
	// Driven over the LAN: a live session's adb calls go to ip:5555.
	before := len(adbCalls(t, e.state))
	srv := e.startFakeSession(t)
	conn := e.dialWS(t, srv, "admin", "video")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("no video over the LAN: %v", err)
	}
	conn.Close()
	for _, c := range adbCalls(t, e.state)[before:] {
		if !strings.HasPrefix(c, "-s "+fakeLANAddr+" ") {
			t.Errorf("adb call not over the LAN: %q", c)
		}
	}

	// No profile: its adapter is fenced, nothing routed or forwarded.
	if rules := e.sys.ruleset(); strings.Contains(rules, `oifname "wg*" ip saddr`) || len(e.sys.routed()) != 0 {
		t.Fatalf("phone without a profile is routed: %v\n%s", e.sys.routed(), rules)
	}
	profile := e.createProfile(t, "Sweden")
	w := e.do(t, "admin", "PUT", "/api/v1/devices/"+fakeSerial+"/tether", `{"profile_id":"`+profile+`"}`)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "apply_error") {
		t.Fatalf("assign: %d %s", w.Code, w.Body.String())
	}
	want := `iifname "phonelan" oifname "wg*" ip saddr 10.77.0.100 ether saddr ` + mac + ` accept`
	if rules := e.sys.ruleset(); !strings.Contains(rules, want) || !strings.Contains(rules, "dnat ip to 10.64.0.1") {
		t.Fatalf("assignment didn't let the phone's adapter into its profile's tunnel:\n%s", rules)
	}
	if got := e.sys.routed(); len(got) != 1 || got[0] != "from 10.77.0.100 iif phonelan lookup 51820" {
		t.Fatalf("phone's routes: %v", got)
	}
	if w := e.do(t, "admin", "PUT", "/api/v1/devices/"+fakeSerial+"/tether", `{"profile_id":null}`); w.Code != http.StatusOK {
		t.Fatalf("unassign: %d", w.Code)
	}
	if rules := e.sys.ruleset(); strings.Contains(rules, `oifname "wg*" ip saddr`) || len(e.sys.routed()) != 0 {
		t.Fatalf("unassigned phone still routed: %v\n%s", e.sys.routed(), rules)
	}

	// The phone reboots: its adapter is back, adbd no longer on TCP.
	e.setState(t, "lan", "", false)
	e.ctl.Pass(ctx)
	if dev := e.device(t); !strings.Contains(dev, `"needs_reprovision":true`) || !strings.Contains(dev, `"status":"disconnected"`) {
		t.Fatalf("rebooted phone not reported as needing re-provisioning: %s", dev)
	}

	// Plugged into USB again (adapter out): driven over USB.
	e.setState(t, "unplugged", "", false)
	e.ctl.Pass(ctx)
	dev := e.device(t)
	if !strings.Contains(dev, `"connection":"usb"`) || !strings.Contains(dev, `"status":"connected"`) {
		t.Fatalf("phone back on USB: %s", dev)
	}
	before = len(adbCalls(t, e.state))
	if w := e.do(t, "admin", "POST", "/api/v1/devices/"+fakeSerial+"/wake", ""); w.Code != http.StatusOK {
		t.Fatalf("wake: %d", w.Code)
	}
	for _, c := range adbCalls(t, e.state)[before:] {
		if !strings.HasPrefix(c, "-s "+fakeSerial+" ") {
			t.Errorf("adb call not over USB: %q", c)
		}
	}
}
