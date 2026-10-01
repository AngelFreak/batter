package lan

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/migrate"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/jackc/pgx/v5/pgxpool"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// testDB returns a fresh, migrated database. Needs BATTER_TEST_DATABASE_URL;
// skips otherwise.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("BATTER_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("BATTER_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("batter_lan_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	if err := migrate.Up(ctx, pool, quiet); err != nil {
		t.Fatal(err)
	}
	return pool
}

func testNet(t *testing.T, pool string) Network {
	t.Helper()
	n, err := ParseNetwork("10.77.0.1/24", pool)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// sentConn records what the server sends.
type sentConn struct {
	net.PacketConn
	mu   sync.Mutex
	sent []sent
}

type sent struct {
	to  net.Addr
	msg *dhcpv4.DHCPv4
}

func (c *sentConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	m, err := dhcpv4.FromBytes(b)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, sent{addr, m})
	return len(b), nil
}

func (c *sentConn) last(t *testing.T) sent {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		t.Fatal("server sent nothing")
	}
	return c.sent[len(c.sent)-1]
}

func (c *sentConn) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

var bcast = &net.UDPAddr{IP: net.IPv4bcast, Port: 68}

func mac(t *testing.T, s string) net.HardwareAddr {
	t.Helper()
	m, err := net.ParseMAC(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func discover(t *testing.T, hw net.HardwareAddr) *dhcpv4.DHCPv4 {
	t.Helper()
	m, err := dhcpv4.NewDiscovery(hw, dhcpv4.WithOption(dhcpv4.OptHostName("android-1")))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func request(t *testing.T, hw net.HardwareAddr, ip net.IP, server net.IP) *dhcpv4.DHCPv4 {
	t.Helper()
	mods := []dhcpv4.Modifier{
		dhcpv4.WithMessageType(dhcpv4.MessageTypeRequest),
		dhcpv4.WithHwAddr(hw),
		dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(ip)),
	}
	if server != nil {
		mods = append(mods, dhcpv4.WithOption(dhcpv4.OptServerIdentifier(server)))
	}
	m, err := dhcpv4.New(mods...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func newServer(t *testing.T, db *pgxpool.Pool, n Network) (*DHCP, *sentConn) {
	t.Helper()
	return &DHCP{Net: n, Leases: &Leases{DB: db, Net: n}, Logger: quiet}, &sentConn{}
}

func TestDiscoverOffersAPoolAddressWithBatterAsGatewayAndDNS(t *testing.T) {
	db := testDB(t)
	srv, conn := newServer(t, db, testNet(t, "10.77.0.100-10.77.0.200"))
	srv.Handle(conn, bcast, discover(t, mac(t, "02:00:00:00:00:01")))

	got := conn.last(t)
	m := got.msg
	if m.MessageType() != dhcpv4.MessageTypeOffer {
		t.Fatalf("reply %s, want OFFER", m.MessageType())
	}
	if !m.YourIPAddr.Equal(net.ParseIP("10.77.0.100")) {
		t.Errorf("offered %s, want the pool's first address", m.YourIPAddr)
	}
	lan := net.ParseIP("10.77.0.1")
	if r := m.Router(); len(r) != 1 || !r[0].Equal(lan) {
		t.Errorf("router %v, want Batter's LAN address", r)
	}
	if d := m.DNS(); len(d) != 1 || !d[0].Equal(lan) {
		t.Errorf("DNS %v, want Batter's LAN address", d)
	}
	if !m.ServerIdentifier().Equal(lan) {
		t.Errorf("server id %v", m.ServerIdentifier())
	}
	if mask := m.SubnetMask(); mask.String() != "ffffff00" {
		t.Errorf("mask %v", mask)
	}
	if m.IPAddressLeaseTime(0) == 0 {
		t.Error("no lease time")
	}
	if got.to.String() != bcast.String() {
		t.Errorf("sent to %v, want broadcast (the client has no address yet)", got.to)
	}
}

// An adapter's address is a reservation: it keeps it for good, across
// Batter restarts, so its phone's routing and adb address stay put.
func TestAdaptersKeepTheirAddressesAcrossRestarts(t *testing.T) {
	db := testDB(t)
	n := testNet(t, "10.77.0.100-10.77.0.200")
	a, b := mac(t, "02:00:00:00:00:0a"), mac(t, "02:00:00:00:00:0b")

	srv, conn := newServer(t, db, n)
	srv.Handle(conn, bcast, discover(t, a))
	ipA := conn.last(t).msg.YourIPAddr
	srv.Handle(conn, bcast, discover(t, b))
	ipB := conn.last(t).msg.YourIPAddr
	if ipA.Equal(ipB) {
		t.Fatalf("two adapters offered the same address %s", ipA)
	}

	restarted, conn2 := newServer(t, db, n)
	restarted.Handle(conn2, bcast, discover(t, b))
	if got := conn2.last(t).msg.YourIPAddr; !got.Equal(ipB) {
		t.Fatalf("after a restart B was offered %s, want its %s", got, ipB)
	}
}

func TestRequestIsAckedForItsOwnAddressOnly(t *testing.T) {
	db := testDB(t)
	srv, conn := newServer(t, db, testNet(t, "10.77.0.100-10.77.0.200"))
	var acked []Lease
	var mu sync.Mutex
	srv.OnLease = func(l Lease) {
		mu.Lock()
		acked = append(acked, l)
		mu.Unlock()
	}
	hw := mac(t, "02:00:00:00:00:01")
	lan := net.ParseIP("10.77.0.1")
	srv.Handle(conn, bcast, discover(t, hw))
	offered := conn.last(t).msg.YourIPAddr

	srv.Handle(conn, bcast, request(t, hw, offered, lan))
	if m := conn.last(t).msg; m.MessageType() != dhcpv4.MessageTypeAck || !m.YourIPAddr.Equal(offered) {
		t.Fatalf("request for its address: %s %s", m.MessageType(), m.YourIPAddr)
	}
	if len(acked) != 1 || acked[0].IP != netip.MustParseAddr(offered.String()) || acked[0].MAC != hw.String() {
		t.Fatalf("OnLease got %+v", acked)
	}

	// Someone else's address (or one from a previous network) is refused,
	// so the client starts over and gets its own.
	srv.Handle(conn, bcast, request(t, hw, net.ParseIP("10.77.0.150"), nil))
	got := conn.last(t)
	if got.msg.MessageType() != dhcpv4.MessageTypeNak {
		t.Fatalf("request for another address: %s", got.msg.MessageType())
	}
	if got.to.String() != bcast.String() {
		t.Errorf("NAK sent to %v, want broadcast", got.to)
	}
	if len(acked) != 1 {
		t.Fatal("OnLease called for a NAK")
	}
}

func TestRequestForAnotherServerIsIgnored(t *testing.T) {
	db := testDB(t)
	srv, conn := newServer(t, db, testNet(t, "10.77.0.100-10.77.0.200"))
	hw := mac(t, "02:00:00:00:00:01")
	srv.Handle(conn, bcast, discover(t, hw))
	n := conn.count()
	srv.Handle(conn, bcast, request(t, hw, conn.last(t).msg.YourIPAddr, net.ParseIP("10.77.0.9")))
	if conn.count() != n {
		t.Fatalf("answered a request meant for another server: %s", conn.last(t).msg.MessageType())
	}
}

// A renewing client already has its address; the reply goes straight to it.
func TestRenewalIsAnsweredToTheClient(t *testing.T) {
	db := testDB(t)
	srv, conn := newServer(t, db, testNet(t, "10.77.0.100-10.77.0.200"))
	hw := mac(t, "02:00:00:00:00:01")
	srv.Handle(conn, bcast, discover(t, hw))
	ip := conn.last(t).msg.YourIPAddr
	renew, err := dhcpv4.New(dhcpv4.WithMessageType(dhcpv4.MessageTypeRequest), dhcpv4.WithHwAddr(hw), dhcpv4.WithClientIP(ip))
	if err != nil {
		t.Fatal(err)
	}
	peer := &net.UDPAddr{IP: ip, Port: 68}
	srv.Handle(conn, peer, renew)
	got := conn.last(t)
	if got.msg.MessageType() != dhcpv4.MessageTypeAck || got.to.String() != peer.String() {
		t.Fatalf("renewal: %s to %v", got.msg.MessageType(), got.to)
	}
}

func TestFullPoolOffersNothing(t *testing.T) {
	db := testDB(t)
	srv, conn := newServer(t, db, testNet(t, "10.77.0.100-10.77.0.101"))
	srv.Handle(conn, bcast, discover(t, mac(t, "02:00:00:00:00:01")))
	srv.Handle(conn, bcast, discover(t, mac(t, "02:00:00:00:00:02")))
	srv.Handle(conn, bcast, discover(t, mac(t, "02:00:00:00:00:03")))
	if n := conn.count(); n != 2 {
		t.Fatalf("%d offers from a 2-address pool", n)
	}
}

// After the LAN is renumbered, an adapter's old reservation is replaced by
// one in the new pool.
func TestReservationOutsideThePoolIsReplaced(t *testing.T) {
	db := testDB(t)
	hw := mac(t, "02:00:00:00:00:01")
	old, conn := newServer(t, db, testNet(t, "10.77.0.100-10.77.0.200"))
	old.Handle(conn, bcast, discover(t, hw))

	srv, conn2 := newServer(t, db, testNet(t, "10.77.0.210-10.77.0.220"))
	srv.Handle(conn2, bcast, discover(t, hw))
	if got := conn2.last(t).msg.YourIPAddr; !got.Equal(net.ParseIP("10.77.0.210")) {
		t.Fatalf("offered %s, want 10.77.0.210", got)
	}
}

func TestBindingASerialMovesItToTheNewAdapter(t *testing.T) {
	db := testDB(t)
	n := testNet(t, "10.77.0.100-10.77.0.200")
	leases := &Leases{DB: db, Net: n}
	ctx := context.Background()
	a, err := leases.Allocate(ctx, "02:00:00:00:00:0a", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := leases.Allocate(ctx, "02:00:00:00:00:0b", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.Bind(ctx, a.MAC, "SER1"); err != nil {
		t.Fatal(err)
	}
	// The phone moved to another adapter.
	if err := leases.Bind(ctx, b.MAC, "SER1"); err != nil {
		t.Fatal(err)
	}
	all, err := leases.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range all {
		want := ""
		if l.MAC == b.MAC {
			want = "SER1"
		}
		if l.Serial != want {
			t.Errorf("lease %s bound to %q, want %q", l.MAC, l.Serial, want)
		}
	}
}

func TestParseNetwork(t *testing.T) {
	n, err := ParseNetwork("10.77.0.1/24", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.Addr.String() != "10.77.0.1" || n.Prefix.String() != "10.77.0.0/24" {
		t.Errorf("parsed %+v", n)
	}
	// Default pool: the upper half of the subnet, minus its last two
	// addresses (Docker's reserved gateway goes there).
	if n.PoolStart.String() != "10.77.0.128" || n.PoolEnd.String() != "10.77.0.253" {
		t.Errorf("default pool %s-%s", n.PoolStart, n.PoolEnd)
	}
	for _, bad := range [][2]string{
		{"10.77.0.1", ""},                           // no prefix
		{"10.77.0.0/24", ""},                        // network address
		{"fd00::1/64", ""},                          // IPv6
		{"10.77.0.1/24", "10.77.0.200-10.77.0.100"}, // reversed
		{"10.77.0.1/24", "10.77.1.1-10.77.1.9"},     // outside
		{"10.77.0.1/24", "10.77.0.1-10.77.0.9"},     // includes Batter
		{"10.77.0.1/24", "10.77.0.100"},             // not a range
	} {
		if _, err := ParseNetwork(bad[0], bad[1]); err == nil {
			t.Errorf("ParseNetwork(%q, %q) accepted", bad[0], bad[1])
		}
	}
}
