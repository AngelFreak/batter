package lan

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeHost is the box: NICs on it by MAC, and the one moved into Batter.
type fakeHost struct {
	mu          sync.Mutex
	nics        map[string]HostNIC
	inside      *HostNIC
	log         []string
	unavailable error
}

func (h *fakeHost) record(s string) {
	h.log = append(h.log, s)
}

func (h *fakeHost) Available() error { return h.unavailable }

func (h *fakeHost) List(context.Context) ([]HostNIC, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []HostNIC
	for _, n := range h.nics {
		out = append(out, n)
	}
	return out, nil
}

func (h *fakeHost) Find(_ context.Context, mac string) (HostNIC, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	n, ok := h.nics[mac]
	return n, ok, nil
}

func (h *fakeHost) Adopt(_ context.Context, mac string) (HostNIC, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	n, ok := h.nics[mac]
	if !ok || !n.Usable {
		return HostNIC{}, errors.New("refused")
	}
	h.record("adopt " + n.Name)
	delete(h.nics, mac)
	h.inside = &n
	return n, nil
}

func (h *fakeHost) Release(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.record("release " + name)
	n := *h.inside
	n.Name, n.Up = name, false
	h.nics[n.MAC] = n
	h.inside = nil
	return nil
}

func (h *fakeHost) Present(context.Context) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inside != nil
}

// unplug removes the NIC entirely (a USB NIC pulled out); plug puts it on
// the box again.
func (h *fakeHost) unplug() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inside = nil
}

func (h *fakeHost) plug(n HostNIC) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nics[n.MAC] = n
}

func (h *fakeHost) has(entry string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Contains(h.log, entry)
}

var (
	nic2 = HostNIC{Name: "enp2s0", MAC: "02:00:00:00:77:02", Usable: true}
	nic3 = HostNIC{Name: "enx3", MAC: "02:00:00:00:77:03", Usable: true}
	used = HostNIC{Name: "eth0", MAC: "02:00:00:00:77:01", Usable: false, Reason: "it carries the box's own network connection (default route)"}
)

func status(t *testing.T, c *Controller) Status {
	t.Helper()
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Off: Batter runs no command at all and the box is untouched.
func TestOffIsANoOp(t *testing.T) {
	c, sys, host := newControllerOn(t, &fakeADB{}, fixedProfiles{})
	host.plug(nic2)
	c.Pass(context.Background())
	if len(sys.log) != 0 || len(host.log) != 0 {
		t.Fatalf("off ran: %v, box: %v", sys.log, host.log)
	}
	if st := status(t, c); st.State != StateOff {
		t.Fatalf("state %q", st.State)
	}
	if err := c.Reload(context.Background()); !errors.Is(err, ErrOff) {
		t.Fatalf("Reload with the network off: %v", err)
	}
}

// The firewall goes in before the NIC leaves the box, and forwarding only
// once the port is up in Batter's namespace.
func TestTakingThePortFencesBeforeMovingAndForwardsAfter(t *testing.T) {
	c, sys, host := newControllerOn(t, &fakeADB{}, fixedProfiles{}, nic2)
	c.Pass(context.Background())
	defer c.stopDHCP()
	if !host.has("adopt enp2s0") {
		t.Fatalf("not adopted: %v", host.log)
	}
	if st := status(t, c); st.State != StateActive || st.Port == nil || st.Port.Name != "enp2s0" {
		t.Fatalf("status %+v", st)
	}
	// The fake box logs separately; the fence must already be in when the
	// port arrives, so it's the first thing run, and forwarding the last.
	if !strings.HasPrefix(sys.log[0], "ip rule show priority 9900") {
		t.Fatalf("first command %q", sys.log[0])
	}
	sys.index(t, "nft -f /dev/stdin")
	if sys.index(t, "ip link set dev phonelan up") > sys.index(t, "sysctl -w net.ipv4.ip_forward=1") {
		t.Fatal("forwarding before the port is configured")
	}
}

// A fence failure leaves the NIC on the box.
func TestNoFenceNoPort(t *testing.T) {
	c, sys, host := newControllerOn(t, &fakeADB{}, fixedProfiles{}, nic2)
	sys.fail = "nft"
	c.Pass(context.Background())
	if len(host.log) != 0 {
		t.Fatalf("port taken without a firewall: %v", host.log)
	}
	if st := status(t, c); st.State != StateError || !strings.Contains(st.Error, "firewall") {
		t.Fatalf("status %+v", st)
	}
}

// Off again: the NIC goes back to the box down under its name, and nothing
// of the phone network is left in Batter's namespace.
func TestTurningOffGivesThePortBackAndLeavesNothing(t *testing.T) {
	c, sys, host := newControllerOn(t, &fakeADB{}, fixedProfiles{}, nic2)
	ctx := context.Background()
	c.Pass(ctx)
	sys.mu.Lock()
	sys.log = nil
	sys.mu.Unlock()
	st, err := c.SetPort(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateOff || st.Port != nil {
		t.Fatalf("status %+v", st)
	}
	if !host.has("release enp2s0") || host.nics[nic2.MAC].Up || host.inside != nil {
		t.Fatalf("not given back down: %v %+v", host.log, host.nics)
	}
	sys.index(t, "sysctl -w net.ipv4.ip_forward=0")
	sys.index(t, "nft -f /dev/stdin") // table deleted
	sys.index(t, "ip rule show priority 9900")
}

func TestSwitchingPortsGivesTheOldOneBack(t *testing.T) {
	c, _, host := newControllerOn(t, &fakeADB{}, fixedProfiles{}, nic2, nic3)
	ctx := context.Background()
	c.Pass(ctx)
	defer c.stopDHCP()
	st, err := c.SetPort(ctx, nic3.MAC)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateActive || st.Port.MAC != nic3.MAC {
		t.Fatalf("status %+v", st)
	}
	want := []string{"adopt enp2s0", "release enp2s0", "adopt enx3"}
	if !slices.Equal(host.log, want) {
		t.Fatalf("box saw %v, want %v", host.log, want)
	}
	if _, back := host.nics[nic2.MAC]; !back {
		t.Fatal("old port not on the box")
	}
}

// A USB NIC unplugged and plugged back in is taken again.
func TestARepluggedPortIsTakenAgain(t *testing.T) {
	c, _, host := newControllerOn(t, &fakeADB{}, fixedProfiles{}, nic2)
	ctx := context.Background()
	c.Pass(ctx)
	defer c.stopDHCP()
	host.unplug()
	c.Pass(ctx)
	if st := status(t, c); st.State != StateMissing {
		t.Fatalf("unplugged: status %+v", st)
	}
	replugged := nic2
	replugged.Name = "enx0011" // USB NICs may come back under another name
	host.plug(replugged)
	c.Pass(ctx)
	if st := status(t, c); st.State != StateActive || st.Port.Name != "enx0011" {
		t.Fatalf("replugged: status %+v (box %v)", st, host.log)
	}
}

func TestUnusablePortsAreRefused(t *testing.T) {
	c, _, host := newControllerOn(t, &fakeADB{}, fixedProfiles{})
	host.plug(used)
	ctx := context.Background()
	_, err := c.SetPort(ctx, used.MAC)
	var pe *PortError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "default route") {
		t.Fatalf("SetPort(eth0) = %v", err)
	}
	if _, err := c.SetPort(ctx, "02:00:00:00:00:99"); !errors.As(err, &pe) {
		t.Fatalf("SetPort(unknown) = %v", err)
	}
	if p, _ := c.Ports.Get(ctx); p != nil {
		t.Fatalf("stored %+v", p)
	}
}

// If the box had the NIC up, something on the box manages it: say so.
func TestWarnsWhenTheBoxManagesThePort(t *testing.T) {
	up := nic2
	up.Up = true
	c, _, _ := newControllerOn(t, &fakeADB{}, fixedProfiles{}, up)
	c.Pass(context.Background())
	defer c.stopDHCP()
	if st := status(t, c); st.State != StateActive || !strings.Contains(st.Warning, "manages it") {
		t.Fatalf("status %+v", st)
	}
}

func TestUnavailableWithoutTheBoxsNamespace(t *testing.T) {
	c, sys, host := newControllerOn(t, &fakeADB{}, fixedProfiles{}, nic2)
	host.unavailable = ErrNoHostNet
	c.Pass(context.Background())
	if len(sys.log) != 0 || len(host.log) != 0 {
		t.Fatalf("ran %v %v", sys.log, host.log)
	}
	if st := status(t, c); st.State != StateUnavailable || st.Unavailable == "" {
		t.Fatalf("status %+v", st)
	}
}

// Devices on the network that aren't phones (the switch itself) get an
// address but nothing else, and are listed as unidentified clients.
func TestNonPhoneClientsGetNothing(t *testing.T) {
	c, sys, _ := newControllerOn(t, &fakeADB{}, fixedProfiles{"PHONE1": {Table: 51820}}, nic2)
	ctx := context.Background()
	c.Pass(ctx)
	defer c.stopDHCP()
	if _, err := c.Leases.Allocate(ctx, "02:aa:00:00:00:01", "usw-flex-mini"); err != nil {
		t.Fatal(err)
	}
	c.Pass(ctx)
	if rules := sys.stdin["nft -f /dev/stdin"]; strings.Contains(rules, "02:aa:00:00:00:01") {
		t.Fatalf("switch allowed somewhere:\n%s", rules)
	}
	st := status(t, c)
	if len(st.Clients) != 1 || st.Clients[0].Hostname != "usw-flex-mini" || st.Clients[0].Serial != "" {
		t.Fatalf("clients %+v", st.Clients)
	}
}

// A port already in Batter's namespace at start (left by an earlier run)
// is given back and taken again, so its state is Batter's own.
func TestAPortLeftInsideIsTakenAfresh(t *testing.T) {
	c, _, host := newControllerOn(t, &fakeADB{}, fixedProfiles{}, nic2)
	ctx := context.Background()
	if _, err := host.Adopt(ctx, nic2.MAC); err != nil { // the earlier run's
		t.Fatal(err)
	}
	host.log = nil
	c.Pass(ctx)
	defer c.stopDHCP()
	if want := []string{"release enp2s0", "adopt enp2s0"}; !slices.Equal(host.log, want) {
		t.Fatalf("box saw %v, want %v", host.log, want)
	}
	if st := status(t, c); st.State != StateActive {
		t.Fatalf("status %+v", st)
	}
}
