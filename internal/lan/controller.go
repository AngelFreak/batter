package lan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"github.com/XpertaDK/batter/internal/device"
	"github.com/XpertaDK/batter/internal/vpn"
)

// ADBPort is where phones' adbd listens once switched to TCP (adb tcpip).
const ADBPort = 5555

// DefaultInterval is how often the controller looks for phones and checks
// the firewall.
const DefaultInterval = 5 * time.Second

// ADB is what the controller needs of adb (*device.ADB).
type ADB interface {
	ListTransports(ctx context.Context) ([]device.ADBDevice, error)
	Connect(ctx context.Context, addr string) error
	Disconnect(ctx context.Context, addr string) error
	SerialNo(ctx context.Context, transport string) (string, error)
	SetTransport(serial, transport string)
	ClearTransport(serial string)
	TCPIP(ctx context.Context, serial string, port int) error
	Shell(ctx context.Context, serial string, args ...string) ([]byte, error)
}

// Profiles reports each phone's VPN routing (*vpn.Service).
type Profiles interface {
	Assignments(ctx context.Context) (map[string]vpn.Assignment, error)
}

// Controller runs the phone LAN: it moves the chosen box NIC into Batter's
// namespace (see port.go), serves DHCP on it, finds the phone behind each
// adapter over adb, points the phone's adb calls at its LAN address, and
// keeps the firewall in line with leases and VPN assignments.
type Controller struct {
	Net      Network
	Leases   *Leases
	Ports    *PortSetting
	Host     Host
	Firewall *Firewall
	ADB      ADB
	Profiles Profiles
	Logger   *slog.Logger
	Interval time.Duration // 0 = DefaultInterval
	// Dial probes a phone's adb port; nil = a TCP dial. Tests replace it.
	Dial func(ctx context.Context, addr string) error

	trigger chan struct{}
	once    sync.Once
	pass    sync.Mutex // one pass at a time

	mu        sync.Mutex
	reprov    map[string]netip.Addr // serial -> address of a phone needing USB re-provisioning
	connected map[string]string     // transport -> serial, as verified by asking the phone
	onLAN     map[string]bool       // serials whose adb calls go over the LAN
	rules     string                // the phones the firewall was last applied for
	appliedAt time.Time
	fwErr     error

	port     *adopted // the NIC in Batter's namespace, if any
	state    string   // see Status
	portErr  string
	warning  string
	dhcpStop context.CancelFunc
}

// reapplyEvery re-applies an unchanged ruleset now and then, repairing it
// if something else removed it.
const reapplyEvery = time.Minute

// Run reconciles until ctx is done, then gives the port back to the box
// (the setting stays, so the next start takes it again).
func (c *Controller) Run(ctx context.Context) {
	c.Pass(ctx)
	ticker := time.NewTicker(c.interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.pass.Lock()
			defer c.pass.Unlock()
			releaseCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if c.currentPort() != nil {
				c.release(releaseCtx)
			}
			return
		case <-ticker.C:
		case <-c.triggered():
		}
		c.Pass(ctx)
	}
}

// DHCPServer returns the LAN's DHCP server; each lease it hands out
// triggers a pass, so a new phone is found right away.
func (c *Controller) DHCPServer() *DHCP {
	return &DHCP{Net: c.Net, Leases: c.Leases, OnLease: func(Lease) { c.Trigger() }, Logger: c.Logger}
}

// Trigger asks for a pass soon, without waiting for it.
func (c *Controller) Trigger() {
	select {
	case c.triggered() <- struct{}{}:
	default:
	}
}

func (c *Controller) triggered() chan struct{} {
	c.once.Do(func() { c.trigger = make(chan struct{}, 1) })
	return c.trigger
}

// ErrOff means the phone network is off: no port is chosen.
var ErrOff = errors.New("the phone network is off; choose its port under Admin → Phone network")

// Reload re-applies the firewall now, for a changed assignment or profile.
func (c *Controller) Reload(ctx context.Context) error {
	c.pass.Lock()
	defer c.pass.Unlock()
	if c.currentPort() == nil {
		if want, err := c.Ports.Get(ctx); err != nil {
			return err
		} else if want == nil {
			return ErrOff
		}
	}
	return c.applyFirewall(ctx, true)
}

// Pass brings the port in line with the setting, finds phones on the LAN
// and re-applies the firewall.
func (c *Controller) Pass(ctx context.Context) {
	c.pass.Lock()
	defer c.pass.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if !c.syncPort(ctx) {
		return
	}
	if err := c.discover(ctx); err != nil {
		c.Logger.Warn("lan: phone discovery failed", "error", err)
	}
	_ = c.applyFirewall(ctx, false)
}

// NeedsReprovision reports whether serial's phone is on the LAN (its
// adapter answers at addr) but its adb isn't listening there, as after a
// reboot: adb over TCP then has to be switched on again over USB.
func (c *Controller) NeedsReprovision(serial string) (addr netip.Addr, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	addr, ok = c.reprov[serial]
	return addr, ok
}

// Provision switches the USB-connected phone's adb to TCP so it can be
// reached once it's moved to its ethernet adapter, and removes gnirehtet's
// client left from reverse tethering (its VPN would capture the phone's
// traffic and go nowhere).
func (c *Controller) Provision(ctx context.Context, serial string) error {
	if _, err := c.ADB.Shell(ctx, serial, "pm", "uninstall", "com.genymobile.gnirehtet"); err != nil {
		c.Logger.Debug("lan: gnirehtet client not removed", "serial", serial, "error", err)
	}
	if err := c.ADB.TCPIP(ctx, serial, ADBPort); err != nil {
		return fmt.Errorf("switch adb to TCP: %w", err)
	}
	c.Logger.Info("lan: adb over TCP switched on; move the phone to its ethernet adapter", "serial", serial)
	return nil
}

// recent is how long after its last DHCP request an adapter is still
// looked for: two lease times, so a present adapter (renewing at half a
// lease) is always recent.
func (c *Controller) recent() time.Duration {
	return 2 * DefaultLeaseTime
}

// discover matches each recently seen adapter to the phone behind it.
func (c *Controller) discover(ctx context.Context) error {
	leases, err := c.Leases.List(ctx)
	if err != nil {
		return err
	}
	transports, err := c.ADB.ListTransports(ctx)
	if err != nil {
		return err
	}
	state := map[string]string{}
	for _, t := range transports {
		state[t.Transport] = t.State
	}

	results := make([]probeResult, len(leases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i, l := range leases {
		if time.Since(l.LastSeen) > c.recent() {
			results[i] = probeResult{lease: l, absent: true}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = c.probe(ctx, l, state[c.addr(l)])
		}()
	}
	wg.Wait()

	found := map[string]bool{}
	reprov := map[string]netip.Addr{}
	for _, r := range results {
		l := r.lease
		switch {
		case r.serial != "":
			if r.serial != l.Serial {
				if err := c.Leases.Bind(ctx, l.MAC, r.serial); err != nil {
					c.Logger.Error("lan: couldn't record the phone behind an adapter", "mac", l.MAC, "serial", r.serial, "error", err)
					continue
				}
				c.Logger.Info("lan: phone found on the LAN", "serial", r.serial, "ip", l.IP, "mac", l.MAC)
			}
			c.ADB.SetTransport(r.serial, c.addr(l))
			found[r.serial] = true
		case l.Serial != "" && r.refused:
			reprov[l.Serial] = l.IP
		}
	}
	// Phones no longer on the LAN go back to USB (if they're there).
	c.mu.Lock()
	for serial := range c.onLAN {
		if !found[serial] {
			c.ADB.ClearTransport(serial)
		}
	}
	c.onLAN = found
	c.mu.Unlock()

	c.mu.Lock()
	for serial := range reprov {
		if found[serial] {
			delete(reprov, serial) // on another adapter now
			continue
		}
		if _, known := c.reprov[serial]; !known {
			c.Logger.Warn("lan: phone is on the LAN but adb over TCP is off (rebooted?); re-provision it over USB", "serial", serial)
		}
	}
	c.reprov = reprov
	c.mu.Unlock()
	return nil
}

type probeResult struct {
	lease   Lease
	serial  string // the phone behind the adapter, when adb reaches it
	refused bool   // the adapter answers but nothing listens on the adb port
	absent  bool
}

// probe finds out what's behind a lease's address: a phone adb reaches
// (connecting if needed), a phone whose adb is off, or nothing.
func (c *Controller) probe(ctx context.Context, l Lease, state string) probeResult {
	addr := c.addr(l)
	r := probeResult{lease: l}
	if state != "device" {
		if state != "" {
			// offline/unauthorized: start over rather than wait on it.
			_ = c.ADB.Disconnect(ctx, addr)
			c.forget(addr)
		}
		if err := c.dial(ctx, addr); err != nil {
			r.refused = errors.Is(err, syscall.ECONNREFUSED)
			r.absent = !r.refused
			return r
		}
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := c.ADB.Connect(cctx, addr)
		cancel()
		if err != nil {
			c.Logger.Debug("lan: adb connect failed", "addr", addr, "error", err)
			return r
		}
		c.forget(addr)
	}
	// Ask the phone who it is once per connection: an address could now
	// belong to another phone (adapter moved).
	c.mu.Lock()
	serial, known := c.connected[addr]
	c.mu.Unlock()
	if !known {
		var err error
		if serial, err = c.ADB.SerialNo(ctx, addr); err != nil {
			c.Logger.Warn("lan: couldn't identify the phone", "addr", addr, "error", err)
			return r
		}
		c.mu.Lock()
		if c.connected == nil {
			c.connected = map[string]string{}
		}
		c.connected[addr] = serial
		c.mu.Unlock()
	}
	r.serial = serial
	return r
}

func (c *Controller) forget(addr string) {
	c.mu.Lock()
	delete(c.connected, addr)
	c.mu.Unlock()
}

func (c *Controller) dial(ctx context.Context, addr string) error {
	if c.Dial != nil {
		return c.Dial(ctx, addr)
	}
	d := net.Dialer{Timeout: 1500 * time.Millisecond}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Controller) addr(l Lease) string {
	return netip.AddrPortFrom(l.IP, ADBPort).String()
}

// applyFirewall installs the ruleset for the current leases and
// assignments if it changed (or force, or it's been a while).
func (c *Controller) applyFirewall(ctx context.Context, force bool) error {
	phones, err := c.phones(ctx)
	if err != nil {
		// Keep whatever is installed: it was right for the last known state
		// and fails closed for anything new.
		c.Logger.Error("lan: phone routing not updated", "error", err)
		return err
	}
	// Everything the firewall installs follows from the phones list.
	rules := fmt.Sprint(phones)
	c.mu.Lock()
	same := rules == c.rules && c.fwErr == nil && time.Since(c.appliedAt) < reapplyEvery
	c.mu.Unlock()
	if same && !force {
		return nil
	}
	err = c.Firewall.Fence(ctx, phones)
	if err != nil && c.currentPort() != nil {
		// Fail closed: phones get nothing rather than a half-fenced router.
		if downErr := c.Firewall.Down(ctx); downErr != nil {
			c.Logger.Error("lan: couldn't take the phone network down after a firewall failure", "error", downErr)
		}
		err = fmt.Errorf("phone network firewall not installed (port taken down): %w", err)
	}
	c.mu.Lock()
	wasDown := c.fwErr != nil
	c.mu.Unlock()
	if err == nil && wasDown && c.currentPort() != nil {
		err = c.Firewall.Up(ctx) // fenced again: the port comes back
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil && (c.fwErr == nil || c.fwErr.Error() != err.Error()) {
		c.Logger.Error("lan: phones have no network", "error", err)
	} else if err == nil && c.fwErr != nil {
		c.Logger.Info("lan: phone network firewall installed")
	}
	c.rules, c.appliedAt, c.fwErr = rules, time.Now(), err
	return err
}

// phones lists every leased adapter with its phone's routing. Adapters with
// no identified phone, or a phone without a profile, get no route: nothing
// is forwarded for them.
func (c *Controller) phones(ctx context.Context) ([]Phone, error) {
	leases, err := c.Leases.List(ctx)
	if err != nil {
		return nil, err
	}
	assignments, err := c.Profiles.Assignments(ctx)
	if err != nil {
		return nil, err
	}
	phones := make([]Phone, 0, len(leases))
	for _, l := range leases {
		p := Phone{IP: l.IP, MAC: l.MAC}
		if a, ok := assignments[l.Serial]; ok && l.Serial != "" {
			p.Table, p.DNS = a.Table, a.DNS
		}
		phones = append(phones, p)
	}
	return phones, nil
}

func (c *Controller) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return DefaultInterval
}
