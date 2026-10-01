package lan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Host is the box side of the phone network's port (*HostNet).
type Host interface {
	Available() error
	List(ctx context.Context) ([]HostNIC, error)
	Find(ctx context.Context, mac string) (HostNIC, bool, error)
	Adopt(ctx context.Context, mac string) (HostNIC, error)
	Release(ctx context.Context, name string) error
	// Present reports whether the port (Iface) is in Batter's namespace.
	Present(ctx context.Context) bool
}

// Present reports whether Iface is in Batter's namespace.
func (h *HostNet) Present(ctx context.Context) bool {
	_, err := h.cmd(ctx, "", "ip", "link", "show", "dev", Iface)
	return err == nil
}

// Port is the chosen port as stored.
type Port struct {
	MAC  string `json:"mac"`
	Name string `json:"name"` // the box's name for it
}

// PortSetting stores the chosen port (phone_lan_port, migration 007).
type PortSetting struct{ DB *pgxpool.Pool }

// Get returns the chosen port, or nil when the phone network is off.
func (s *PortSetting) Get(ctx context.Context) (*Port, error) {
	var p Port
	err := s.DB.QueryRow(ctx, "SELECT mac, name FROM phone_lan_port").Scan(&p.MAC, &p.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Set chooses the port.
func (s *PortSetting) Set(ctx context.Context, p Port) error {
	_, err := s.DB.Exec(ctx,
		`INSERT INTO phone_lan_port (id, mac, name) VALUES (true, $1, $2)
		 ON CONFLICT (id) DO UPDATE SET mac = EXCLUDED.mac, name = EXCLUDED.name, updated_at = now()`,
		strings.ToLower(p.MAC), p.Name)
	return err
}

// Clear turns the phone network off.
func (s *PortSetting) Clear(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM phone_lan_port")
	return err
}

// adopted is the box NIC currently in Batter's namespace.
type adopted struct {
	mac, name string
}

// Port states.
const (
	StateOff         = "off"         // no port chosen
	StateActive      = "active"      // the port is in Batter's namespace, serving phones
	StateMissing     = "missing"     // the chosen port isn't on the box (unplugged?)
	StateError       = "error"       // see Status.Error
	StateUnavailable = "unavailable" // Batter can't see the box's NICs
)

// Status is the phone network's state for admins.
type Status struct {
	State       string `json:"state"`
	Port        *Port  `json:"port,omitempty"`
	Error       string `json:"error,omitempty"`
	Unavailable string `json:"unavailable,omitempty"`
	// Warning flags a port the box itself manages (brings up): while it's
	// up on the box, phones can reach the box.
	Warning string   `json:"warning,omitempty"`
	Address string   `json:"address"`
	Pool    string   `json:"pool"`
	Clients []Client `json:"clients"`
}

// Client is a device that got an address on the phone network: a phone
// (identified over adb) or anything else, such as the switch itself.
type Client struct {
	MAC      string    `json:"mac"`
	IP       string    `json:"ip"`
	Hostname string    `json:"hostname,omitempty"`
	Serial   string    `json:"serial,omitempty"`
	LastSeen time.Time `json:"last_seen"`
}

// PortError means a NIC can't be chosen as the port.
type PortError struct{ msg string }

func (e *PortError) Error() string { return e.msg }

func (c *Controller) currentPort() *adopted {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.port
}

func (c *Controller) setState(state, err string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state != c.state || err != c.portErr {
		switch state {
		case StateError, StateMissing:
			c.Logger.Warn("lan: phone network "+state, "error", err)
		default:
			c.Logger.Info("lan: phone network " + state)
		}
	}
	c.state, c.portErr = state, err
}

func (c *Controller) setWarning(w string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if w != "" && w != c.warning {
		c.Logger.Warn("lan: " + w)
	}
	c.warning = w
}

// syncPort brings the port in line with the setting and reports whether
// the phone network is serving. The caller holds c.pass.
func (c *Controller) syncPort(ctx context.Context) bool {
	want, err := c.Ports.Get(ctx)
	if err != nil {
		c.Logger.Error("lan: phone network setting unreadable", "error", err)
		return c.currentPort() != nil
	}
	if err := c.Host.Available(); err != nil {
		c.setState(StateUnavailable, err.Error())
		return false
	}
	cur := c.currentPort()
	if cur != nil && (want == nil || cur.mac != strings.ToLower(want.MAC)) {
		if !c.release(ctx) {
			return false
		}
		cur = nil
	}
	if want == nil {
		c.setState(StateOff, "")
		return false
	}
	if cur != nil && !c.Host.Present(ctx) {
		// Unplugged (a USB NIC): it reappears on the box when re-plugged
		// and is taken again then.
		c.stopDHCP()
		c.clearLANTransports()
		c.mu.Lock()
		c.port = nil
		c.mu.Unlock()
		cur = nil
	}
	if cur == nil && c.Host.Present(ctx) {
		// A port left in Batter's namespace by an earlier run (the
		// namespace outlived it): give it back and take it properly.
		c.mu.Lock()
		c.port = &adopted{mac: strings.ToLower(want.MAC), name: want.Name}
		c.mu.Unlock()
		if !c.release(ctx) {
			return false
		}
	}
	if cur == nil {
		return c.adopt(ctx, *want)
	}
	return true
}

// adopt takes the chosen NIC from the box: fence first, then move it in,
// then bring it up, then serve DHCP.
func (c *Controller) adopt(ctx context.Context, want Port) bool {
	phones, err := c.phones(ctx)
	if err == nil {
		err = c.Firewall.Fence(ctx, phones)
	}
	if err != nil {
		c.setState(StateError, "firewall not installed, so the port stays on the box: "+err.Error())
		return false
	}
	nic, ok, err := c.Host.Find(ctx, want.MAC)
	if err != nil {
		c.setState(StateError, err.Error())
		return false
	}
	if !ok {
		c.setState(StateMissing, fmt.Sprintf("network port %s (%s) isn't on the box; plug it in", want.Name, want.MAC))
		return false
	}
	if nic.Up {
		c.setWarning(fmt.Sprintf("%s was up on the box before Batter took it: something on the box manages it "+
			"(NetworkManager, netplan, ifupdown). While Batter is stopped the box can bring it up again, and phones "+
			"can then reach the box. Set it unmanaged (docs/DEPLOY.md).", nic.Name))
	}
	if _, err := c.Host.Adopt(ctx, want.MAC); err != nil {
		c.setState(StateError, err.Error())
		return false
	}
	c.mu.Lock()
	c.port = &adopted{mac: nic.MAC, name: nic.Name}
	c.rules = "" // the next pass re-applies
	c.mu.Unlock()
	if nic.Name != want.Name {
		if err := c.Ports.Set(ctx, Port{MAC: nic.MAC, Name: nic.Name}); err != nil {
			c.Logger.Warn("lan: couldn't record the port's name", "error", err)
		}
	}
	if err := c.Firewall.Up(ctx); err != nil {
		_ = c.Firewall.Down(ctx)
		c.setState(StateError, "port not brought up: "+err.Error())
		return false
	}
	// Phones that knew another port's MAC for Batter's address learn this
	// one now rather than when their ARP entry expires.
	_, _ = c.Firewall.cmd(ctx, "", "arping", "-U", "-c", "1", "-I", Iface, c.Net.Addr.String())
	c.startDHCP()
	c.setState(StateActive, "")
	c.Logger.Info("lan: phone network port taken from the box", "name", nic.Name, "mac", nic.MAC)
	return true
}

// release gives the port back to the box (down) and removes the firewall.
// If the NIC can't be given back it stays down, fenced. The caller holds
// c.pass.
func (c *Controller) release(ctx context.Context) bool {
	cur := c.currentPort()
	c.stopDHCP()
	c.clearLANTransports()
	if err := c.Host.Release(ctx, cur.name); err != nil {
		_ = c.Firewall.Down(ctx)
		c.setState(StateError, fmt.Sprintf("couldn't give %s back to the box (it stays down in Batter): %v", cur.name, err))
		return false
	}
	c.mu.Lock()
	c.port = nil
	c.rules = ""
	c.mu.Unlock()
	if err := c.Firewall.Remove(ctx); err != nil {
		c.Logger.Error("lan: phone network firewall not fully removed", "error", err)
	}
	c.setWarning("")
	c.Logger.Info("lan: phone network port given back to the box", "name", cur.name, "mac", cur.mac)
	// If something on the box manages the NIC it brings it up again; then
	// the box is on the phones' switch. Check after it has had a moment.
	go func() {
		time.Sleep(5 * time.Second)
		checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if nic, ok, err := c.Host.Find(checkCtx, cur.mac); err == nil && ok && nic.Up {
			c.setWarning(fmt.Sprintf("the box brought %s up after Batter gave it back: something on the box manages it, "+
				"and phones on the switch can now reach the box. Unplug the switch or set %s unmanaged (docs/DEPLOY.md).", nic.Name, nic.Name))
		}
	}()
	return true
}

func (c *Controller) startDHCP() {
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.dhcpStop = cancel
	c.mu.Unlock()
	go func() {
		for ctx.Err() == nil {
			if err := c.DHCPServer().Serve(ctx, Iface); err != nil {
				c.Logger.Warn("lan: DHCP server stopped; restarting", "error", err)
				select {
				case <-ctx.Done():
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()
}

func (c *Controller) stopDHCP() {
	c.mu.Lock()
	stop := c.dhcpStop
	c.dhcpStop = nil
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// clearLANTransports sends every phone's adb calls back to USB.
func (c *Controller) clearLANTransports() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for serial := range c.onLAN {
		c.ADB.ClearTransport(serial)
	}
	c.onLAN = nil
	c.connected = nil
	c.reprov = nil
}

// SetPort chooses the box NIC with mac as the phone network's port ("" =
// off) and applies it now.
func (c *Controller) SetPort(ctx context.Context, mac string) (Status, error) {
	mac = strings.ToLower(strings.TrimSpace(mac))
	if mac == "" {
		if err := c.Ports.Clear(ctx); err != nil {
			return Status{}, err
		}
		c.Pass(ctx)
		return c.Status(ctx)
	}
	if err := c.Host.Available(); err != nil {
		return Status{}, err
	}
	if cur := c.currentPort(); cur == nil || cur.mac != mac {
		nic, ok, err := c.Host.Find(ctx, mac)
		if err != nil {
			return Status{}, err
		}
		if !ok {
			return Status{}, &PortError{fmt.Sprintf("no usable network port %s on the box", mac)}
		}
		if !nic.Usable {
			return Status{}, &PortError{fmt.Sprintf("%s can't be the phone network's port: %s", nic.Name, nic.Reason)}
		}
		if err := c.Ports.Set(ctx, Port{MAC: nic.MAC, Name: nic.Name}); err != nil {
			return Status{}, err
		}
	}
	c.Pass(ctx)
	return c.Status(ctx)
}

// Interfaces lists the box's NICs for the picker, the port in use first.
func (c *Controller) Interfaces(ctx context.Context) ([]HostNIC, error) {
	if err := c.Host.Available(); err != nil {
		return nil, err
	}
	nics, err := c.Host.List(ctx)
	if err != nil {
		return nil, err
	}
	if cur := c.currentPort(); cur != nil {
		nics = append([]HostNIC{{Name: cur.name, MAC: cur.mac, Up: true, Usable: true, InUse: true}}, nics...)
	}
	return nics, nil
}

// Status reports the phone network's state and the devices on it.
func (c *Controller) Status(ctx context.Context) (Status, error) {
	st := Status{Address: c.Net.Addr.String() + "/" + fmt.Sprint(c.Net.Prefix.Bits()),
		Pool: c.Net.PoolStart.String() + "-" + c.Net.PoolEnd.String(), Clients: []Client{}}
	want, err := c.Ports.Get(ctx)
	if err != nil {
		return Status{}, err
	}
	st.Port = want
	c.mu.Lock()
	st.State, st.Error, st.Warning = c.state, c.portErr, c.warning
	c.mu.Unlock()
	if st.State == "" {
		st.State = StateOff
	}
	if st.State == StateUnavailable {
		st.Unavailable, st.Error = st.Error, ""
	}
	if st.State == StateOff && want != nil {
		st.State = StateMissing // chosen, not yet taken
	}
	leases, err := c.Leases.List(ctx)
	if err != nil {
		return Status{}, err
	}
	for _, l := range leases {
		st.Clients = append(st.Clients, Client{MAC: l.MAC, IP: l.IP.String(), Hostname: l.Hostname, Serial: l.Serial, LastSeen: l.LastSeen})
	}
	return st, nil
}
