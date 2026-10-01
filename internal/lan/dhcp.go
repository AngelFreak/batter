package lan

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
)

// DefaultLeaseTime is how long a lease lasts before the phone renews it
// (renewal at half of it). Addresses are reservations, so this only bounds
// how long a phone keeps an address after Batter forgets it.
const DefaultLeaseTime = time.Hour

// DHCP is the phone LAN's DHCP server. Each adapter gets its reserved
// address, with Batter as gateway and DNS server.
type DHCP struct {
	Net       Network
	Leases    *Leases
	LeaseTime time.Duration // 0 = DefaultLeaseTime
	// OnLease is called after an adapter is given (ACKed) its address.
	OnLease func(Lease)
	Logger  *slog.Logger
}

// Serve answers DHCP on iface until ctx is done.
func (d *DHCP) Serve(ctx context.Context, iface string) error {
	srv, err := server4.NewServer(iface, &net.UDPAddr{IP: net.IPv4zero, Port: dhcpv4.ServerPort}, d.Handle)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	err = srv.Serve()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// Handle answers one request. peer is where the server4 package says to
// reply: the client's address when it has one, else broadcast.
func (d *DHCP) Handle(conn net.PacketConn, peer net.Addr, req *dhcpv4.DHCPv4) {
	if req.OpCode != dhcpv4.OpcodeBootRequest || len(req.ClientHWAddr) != 6 {
		return
	}
	mac := req.ClientHWAddr.String()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lanIP := net.IP(d.Net.Addr.AsSlice())

	var reply dhcpv4.MessageType
	var lease Lease
	switch req.MessageType() {
	case dhcpv4.MessageTypeDiscover:
		var err error
		if lease, err = d.Leases.Allocate(ctx, mac, req.HostName()); err != nil {
			d.logError(err, "no address for adapter", mac)
			return
		}
		reply = dhcpv4.MessageTypeOffer
	case dhcpv4.MessageTypeRequest:
		if id := req.ServerIdentifier(); id != nil && !id.Equal(lanIP) {
			return // the client chose another server's offer
		}
		want := req.RequestedIPAddress()
		if want == nil || want.IsUnspecified() {
			want = req.ClientIPAddr
		}
		var err error
		if lease, err = d.Leases.Allocate(ctx, mac, req.HostName()); err != nil {
			d.logError(err, "no address for adapter", mac)
			return
		}
		reply = dhcpv4.MessageTypeAck
		if !want.Equal(net.IP(lease.IP.AsSlice())) {
			reply = dhcpv4.MessageTypeNak
		}
	default:
		return // RELEASE, DECLINE, INFORM: reservations don't change
	}

	mods := []dhcpv4.Modifier{
		dhcpv4.WithMessageType(reply),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(lanIP)),
	}
	if reply != dhcpv4.MessageTypeNak {
		mods = append(mods,
			dhcpv4.WithYourIP(net.IP(lease.IP.AsSlice())),
			dhcpv4.WithNetmask(d.Net.Mask()),
			dhcpv4.WithRouter(lanIP),
			dhcpv4.WithDNS(lanIP),
			dhcpv4.WithLeaseTime(uint32(d.leaseTime().Seconds())),
		)
	} else {
		// A NAKed client may think it has an address it can't use.
		peer = &net.UDPAddr{IP: net.IPv4bcast, Port: dhcpv4.ClientPort}
	}
	resp, err := dhcpv4.NewReplyFromRequest(req, mods...)
	if err != nil {
		d.Logger.Error("lan: build DHCP reply", "mac", mac, "error", err)
		return
	}
	if _, err := conn.WriteTo(resp.ToBytes(), peer); err != nil {
		d.Logger.Warn("lan: send DHCP reply", "mac", mac, "error", err)
		return
	}
	if reply == dhcpv4.MessageTypeAck {
		d.Logger.Debug("lan: address leased", "mac", mac, "ip", lease.IP, "serial", lease.Serial)
		if d.OnLease != nil {
			d.OnLease(lease)
		}
	} else if reply == dhcpv4.MessageTypeNak {
		d.Logger.Info("lan: refused an address the adapter doesn't own", "mac", mac, "requested", requestedAddr(req))
	}
}

func (d *DHCP) leaseTime() time.Duration {
	if d.LeaseTime > 0 {
		return d.LeaseTime
	}
	return DefaultLeaseTime
}

func (d *DHCP) logError(err error, msg, mac string) {
	if errors.Is(err, ErrPoolFull) {
		d.Logger.Warn("lan: "+msg, "mac", mac, "error", err)
		return
	}
	d.Logger.Error("lan: "+msg, "mac", mac, "error", err)
}

func requestedAddr(req *dhcpv4.DHCPv4) netip.Addr {
	ip := req.RequestedIPAddress()
	if ip == nil {
		ip = req.ClientIPAddr
	}
	a, _ := netip.AddrFromSlice(ip.To4())
	return a
}
