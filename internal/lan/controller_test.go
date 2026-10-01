package lan

import (
	"context"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/XpertaDK/batter/internal/device"
	"github.com/XpertaDK/batter/internal/vpn"
)

// fakeADB is adb with phones at addresses: phones[addr] = serial.
type fakeADB struct {
	mu         sync.Mutex
	phones     map[string]string
	connected  map[string]bool
	transports map[string]string // serial -> transport, as set by the controller
	serialNos  int
}

func (f *fakeADB) ListTransports(context.Context) ([]device.ADBDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []device.ADBDevice
	for addr := range f.connected {
		if _, ok := f.phones[addr]; ok {
			out = append(out, device.ADBDevice{Serial: addr, Transport: addr, State: "device"})
		}
	}
	return out, nil
}

func (f *fakeADB) Connect(_ context.Context, addr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.phones[addr]; !ok {
		return syscall.ECONNREFUSED
	}
	if f.connected == nil {
		f.connected = map[string]bool{}
	}
	f.connected[addr] = true
	return nil
}

func (f *fakeADB) Disconnect(_ context.Context, addr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.connected, addr)
	return nil
}

func (f *fakeADB) SerialNo(_ context.Context, addr string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serialNos++
	return f.phones[addr], nil
}

func (f *fakeADB) SetTransport(serial, transport string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.transports == nil {
		f.transports = map[string]string{}
	}
	f.transports[serial] = transport
}

func (f *fakeADB) ClearTransport(serial string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.transports, serial)
}

func (f *fakeADB) TCPIP(context.Context, string, int) error { return nil }
func (f *fakeADB) Shell(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

func (f *fakeADB) transport(serial string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transports[serial]
}

type fixedProfiles map[string]vpn.Assignment

func (p fixedProfiles) Assignments(context.Context) (map[string]vpn.Assignment, error) {
	return p, nil
}

// newController returns a controller whose phone network is on, its port
// (nic2) waiting on the fake box.
func newController(t *testing.T, adb *fakeADB, profiles fixedProfiles) (*Controller, *fakeSystem) {
	t.Helper()
	c, sys, _ := newControllerOn(t, adb, profiles, nic2)
	return c, sys
}

func newControllerOn(t *testing.T, adb *fakeADB, profiles fixedProfiles, nics ...HostNIC) (*Controller, *fakeSystem, *fakeHost) {
	t.Helper()
	db := testDB(t)
	n := testNet(t, "10.77.0.100-10.77.0.200")
	sys := &fakeSystem{}
	host := &fakeHost{nics: map[string]HostNIC{}}
	for _, nic := range nics {
		host.nics[nic.MAC] = nic
	}
	ports := &PortSetting{DB: db}
	if len(nics) > 0 {
		if err := ports.Set(context.Background(), Port{MAC: nics[0].MAC, Name: nics[0].Name}); err != nil {
			t.Fatal(err)
		}
	}
	c := &Controller{
		Net:      n,
		Leases:   &Leases{DB: db, Net: n},
		Ports:    ports,
		Host:     host,
		Firewall: &Firewall{Iface: Iface, Net: n, Run: sys.run, Logger: quiet},
		ADB:      adb,
		Profiles: profiles,
		Logger:   quiet,
		Dial: func(_ context.Context, addr string) error {
			adb.mu.Lock()
			defer adb.mu.Unlock()
			if _, ok := adb.phones[addr]; ok {
				return nil
			}
			return &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}
		},
	}
	return c, sys, host
}

// An adapter moved to another phone: the address is re-identified and the
// lease (and with it the routing) follows the phone actually behind it.
func TestAnAdapterMovedToAnotherPhoneIsReidentified(t *testing.T) {
	adb := &fakeADB{phones: map[string]string{"10.77.0.100:5555": "PHONE1"}}
	c, sys := newController(t, adb, fixedProfiles{"PHONE1": {Table: 51820}, "PHONE2": {Table: 51821}})
	ctx := context.Background()
	lease, err := c.Leases.Allocate(ctx, "02:00:00:00:00:0a", "")
	if err != nil {
		t.Fatal(err)
	}
	c.Pass(ctx)
	if got := adb.transport("PHONE1"); got != "10.77.0.100:5555" {
		t.Fatalf("PHONE1 transport %q", got)
	}
	sys.index(t, "ip rule add from 10.77.0.100 iif phonelan lookup 51820")

	// The adapter now sits on PHONE2 (its adb dropped and reconnects).
	_ = adb.Disconnect(ctx, "10.77.0.100:5555")
	adb.mu.Lock()
	adb.phones["10.77.0.100:5555"] = "PHONE2"
	adb.mu.Unlock()
	c.Pass(ctx)
	if got := adb.transport("PHONE2"); got != "10.77.0.100:5555" {
		t.Fatalf("PHONE2 transport %q", got)
	}
	if got := adb.transport("PHONE1"); got != "" {
		t.Fatalf("PHONE1 still mapped to %q", got)
	}
	l, _, _ := c.Leases.Get(ctx, lease.MAC)
	if l.Serial != "PHONE2" {
		t.Fatalf("lease bound to %q", l.Serial)
	}
	// (The fake `ip rule show` lists nothing, so only the add shows.)
	sys.index(t, "ip rule add from 10.77.0.100 iif phonelan lookup 51821")
}

// A phone already connected isn't asked for its serial again every pass.
func TestConnectedPhonesAreIdentifiedOnce(t *testing.T) {
	adb := &fakeADB{phones: map[string]string{"10.77.0.100:5555": "PHONE1"}}
	c, _ := newController(t, adb, fixedProfiles{})
	ctx := context.Background()
	if _, err := c.Leases.Allocate(ctx, "02:00:00:00:00:0a", ""); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		c.Pass(ctx)
	}
	if adb.serialNos != 1 {
		t.Fatalf("asked for the serial %d times", adb.serialNos)
	}
}

// Adapters not seen for a long time aren't probed (each probe of an absent
// address costs a timeout).
func TestStaleLeasesAreNotProbed(t *testing.T) {
	adb := &fakeADB{phones: map[string]string{"10.77.0.100:5555": "PHONE1"}}
	c, _ := newController(t, adb, fixedProfiles{})
	ctx := context.Background()
	if _, err := c.Leases.Allocate(ctx, "02:00:00:00:00:0a", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Leases.DB.Exec(ctx, "UPDATE lan_leases SET last_seen_at = now() - interval '1 day'"); err != nil {
		t.Fatal(err)
	}
	c.Pass(ctx)
	if adb.transport("PHONE1") != "" || adb.serialNos != 0 {
		t.Fatal("stale lease probed")
	}
}

// An unchanged ruleset isn't reloaded every pass; Reload always applies.
func TestFirewallIsReappliedOnlyWhenNeeded(t *testing.T) {
	c, sys := newController(t, &fakeADB{}, fixedProfiles{})
	ctx := context.Background()
	c.Pass(ctx)
	c.Pass(ctx)
	count := func() int {
		n := 0
		for _, l := range sys.log {
			if strings.HasPrefix(l, "nft") {
				n++
			}
		}
		return n
	}
	// Taking the port fences it, and the pass applies once more.
	if n := count(); n != 2 {
		t.Fatalf("%d nft loads for an unchanged ruleset", n)
	}
	if err := c.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 3 {
		t.Fatal("Reload didn't apply")
	}
}
