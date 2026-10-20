package lan

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrPoolFull means every pool address is reserved.
var ErrPoolFull = errors.New("phone LAN address pool is full")

// Lease is an adapter's reserved address and, once identified, the phone
// behind it.
type Lease struct {
	MAC      string // lower-case, colon-separated
	IP       netip.Addr
	Serial   string // "" until identified over adb
	Hostname string
	LastSeen time.Time
}

// Leases stores reservations in lan_leases. An adapter keeps its address
// for good; it is never handed to another one.
type Leases struct {
	DB  *pgxpool.Pool
	Net Network

	mu sync.Mutex // serializes allocation
}

// Allocate returns mac's reservation, creating one from the lowest free
// pool address if it has none (or one outside the current pool), and
// records that the adapter was seen.
func (l *Leases) Allocate(ctx context.Context, mac, hostname string) (Lease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ip netip.Prefix
	err := l.DB.QueryRow(ctx,
		`UPDATE lan_leases SET last_seen_at = now(), hostname = CASE WHEN $2 = '' THEN hostname ELSE $2 END
		 WHERE mac = $1 RETURNING ip`, mac, hostname).Scan(&ip)
	switch {
	case err == nil && l.Net.InPool(ip.Addr()):
		return l.get(ctx, mac)
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return Lease{}, err
	}

	free, err := l.free(ctx)
	if err != nil {
		return Lease{}, err
	}
	_, err = l.DB.Exec(ctx,
		`INSERT INTO lan_leases (mac, ip, hostname) VALUES ($1, $2, $3)
		 ON CONFLICT (mac) DO UPDATE SET ip = EXCLUDED.ip, last_seen_at = now()`,
		mac, free.String(), hostname)
	if err != nil {
		return Lease{}, err
	}
	return l.get(ctx, mac)
}

// free returns the lowest pool address no adapter has reserved.
func (l *Leases) free(ctx context.Context) (netip.Addr, error) {
	rows, err := l.DB.Query(ctx, "SELECT ip FROM lan_leases")
	if err != nil {
		return netip.Addr{}, err
	}
	used, err := pgx.CollectRows(rows, pgx.RowTo[netip.Prefix])
	if err != nil {
		return netip.Addr{}, err
	}
	taken := map[netip.Addr]bool{}
	for _, u := range used {
		taken[u.Addr()] = true
	}
	for ip := l.Net.PoolStart; l.Net.InPool(ip); ip = ip.Next() {
		if !taken[ip] {
			return ip, nil
		}
	}
	return netip.Addr{}, ErrPoolFull
}

// Get returns mac's reservation; ok is false if it has none.
func (l *Leases) Get(ctx context.Context, mac string) (lease Lease, ok bool, err error) {
	lease, err = l.get(ctx, mac)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, false, nil
	}
	return lease, err == nil, err
}

func (l *Leases) get(ctx context.Context, mac string) (Lease, error) {
	rows, err := l.DB.Query(ctx, selectLeases+" WHERE mac = $1", mac)
	if err != nil {
		return Lease{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanLease)
}

// List returns every reservation.
func (l *Leases) List(ctx context.Context) ([]Lease, error) {
	rows, err := l.DB.Query(ctx, selectLeases+" ORDER BY ip")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanLease)
}

// Bind records serial as the phone behind mac's adapter. A serial is behind
// one adapter at a time, so any earlier binding of it is cleared.
func (l *Leases) Bind(ctx context.Context, mac, serial string) error {
	tx, err := l.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, "UPDATE lan_leases SET serial = NULL WHERE serial = $1 AND mac <> $2", serial, mac); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "UPDATE lan_leases SET serial = $2 WHERE mac = $1", mac, serial)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no lease for %s", mac)
	}
	return tx.Commit(ctx)
}

const selectLeases = "SELECT mac, ip, COALESCE(serial, ''), hostname, last_seen_at FROM lan_leases"

func scanLease(row pgx.CollectableRow) (Lease, error) {
	var l Lease
	var ip netip.Prefix
	err := row.Scan(&l.MAC, &ip, &l.Serial, &l.Hostname, &l.LastSeen)
	l.IP = ip.Addr()
	return l, err
}
