package vpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/XpertaDK/batter/db"
	"github.com/XpertaDK/batter/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeSystem stands in for the network and the relays, logging both in one
// sequence so tests can check ordering (kill switch before relay).
type fakeSystem struct {
	mu     sync.Mutex
	log    []string
	stdin  map[string]string
	fail   string // commands containing this fail
	dump   string // `wg show <iface> dump` output
	relays map[int]bool
}

func (f *fakeSystem) run(_ context.Context, stdin, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := name + " " + strings.Join(args, " ")
	f.log = append(f.log, cmd)
	if stdin != "" {
		if f.stdin == nil {
			f.stdin = map[string]string{}
		}
		f.stdin[cmd] = stdin
	}
	if f.fail != "" && strings.Contains(cmd, f.fail) {
		return []byte("RTNETLINK answers: Operation not permitted"), errors.New("exit status 2")
	}
	if strings.HasPrefix(cmd, "wg show") && strings.HasSuffix(cmd, " dump") {
		return []byte(f.dump), nil
	}
	if strings.HasPrefix(cmd, "ip -o link show") {
		return []byte("5: wg0: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1420 qdisc noqueue state UNKNOWN\n"), nil
	}
	return nil, nil
}

func (f *fakeSystem) startRelay(ctx context.Context, uid uint32, port int) {
	f.mu.Lock()
	f.log = append(f.log, fmt.Sprintf("relay start uid=%d port=%d", uid, port))
	if f.relays == nil {
		f.relays = map[int]bool{}
	}
	f.relays[port] = true
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		f.relays[port] = false
		f.mu.Unlock()
	}()
}

func (f *fakeSystem) relayRunning(port int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.relays[port]
}

// waitRelay waits for a relay's stop to land (it happens on a goroutine).
func (f *fakeSystem) waitRelay(t *testing.T, port int, running bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for f.relayRunning(port) != running {
		if time.Now().After(deadline) {
			t.Fatalf("relay on port %d running=%v, want %v", port, !running, running)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *fakeSystem) index(t *testing.T, prefix string) int {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.log, func(c string) bool { return strings.HasPrefix(c, prefix) })
	if i < 0 {
		t.Fatalf("no %q; ran:\n  %s", prefix, strings.Join(f.log, "\n  "))
	}
	return i
}

func (f *fakeSystem) has(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.log, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

func (f *fakeSystem) reset() {
	f.mu.Lock()
	f.log = nil
	f.mu.Unlock()
}

// testDB returns a fresh database with migrations up to and including
// upTo ("" = all). Needs BATTER_TEST_DATABASE_URL; skips otherwise.
func testDB(t *testing.T, upTo string) *pgxpool.Pool {
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
	name := fmt.Sprintf("batter_vpn_%d", time.Now().UnixNano())
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
	if err := migrateUpTo(ctx, pool, upTo); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func migrateUpTo(ctx context.Context, pool *pgxpool.Pool, upTo string) error {
	if upTo == "" {
		return migrate.Up(ctx, pool, quiet)
	}
	sub := fstest.MapFS{}
	entries, err := db.Migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() > upTo {
			continue
		}
		b, err := db.Migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		sub["migrations/"+e.Name()] = &fstest.MapFile{Data: b}
	}
	return migrate.UpFS(ctx, pool, sub, quiet)
}

func newService(t *testing.T, pool *pgxpool.Pool, sys *fakeSystem) *Service {
	t.Helper()
	s := &Service{DB: pool, Run: sys.run, StartRelay: sys.startRelay, Logger: quiet}
	t.Cleanup(s.Stop)
	return s
}

// profileConf is sample with its own DNS server, to tell profiles apart.
func profileConf(dns string) string {
	return strings.Replace(sample, "DNS = 10.64.0.1", "DNS = "+dns, 1)
}

func addDevice(t *testing.T, pool *pgxpool.Pool, serial string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), "INSERT INTO devices (serial) VALUES ($1)", serial); err != nil {
		t.Fatal(err)
	}
}

func assign(t *testing.T, pool *pgxpool.Pool, serial, profileID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), "UPDATE devices SET vpn_profile_id = $1 WHERE serial = $2", profileID, serial); err != nil {
		t.Fatal(err)
	}
}

func TestProfilesGetTheirOwnSlotAndKillSwitchBeforeRelay(t *testing.T) {
	sys := &fakeSystem{}
	s := newService(t, testDB(t, ""), sys)
	ctx := context.Background()

	a, err := s.Create(ctx, "Sweden", profileConf("10.64.0.1"), true)
	if err != nil {
		t.Fatal(err)
	}
	if a.ApplyError != "" {
		t.Fatalf("apply error: %s", a.ApplyError)
	}
	b, err := s.Create(ctx, "Denmark", profileConf("10.65.0.1"), true)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []struct {
		uid, table, iface string
		port              int
	}{{"31416", "51820", "wg0", 31416}, {"31417", "51821", "wg1", 31417}} {
		rule := sys.index(t, "ip rule add uidrange "+want.uid+"-"+want.uid+" lookup "+want.table)
		unreachable := sys.index(t, "ip route replace unreachable default table "+want.table)
		link := sys.index(t, "ip link add "+want.iface+" type wireguard")
		relay := sys.index(t, fmt.Sprintf("relay start uid=%s port=%d", want.uid, want.port))
		if rule > relay || unreachable > relay || rule > link {
			t.Fatalf("slot %s: relay or tunnel before kill switch:\n  %s", want.iface, strings.Join(sys.log, "\n  "))
		}
		sys.index(t, "ip route replace 0.0.0.0/0 dev "+want.iface+" table "+want.table)
	}
	if a.ID == b.ID || a.Name != "Sweden" || b.Name != "Denmark" {
		t.Fatalf("profiles: %+v %+v", a, b)
	}
}

func TestDisablingAProfileKeepsItsKillSwitch(t *testing.T) {
	sys := &fakeSystem{}
	s := newService(t, testDB(t, ""), sys)
	ctx := context.Background()
	p, err := s.Create(ctx, "Sweden", sample, true)
	if err != nil {
		t.Fatal(err)
	}
	sys.reset()
	off := false
	if _, err := s.Update(ctx, p.ID, ProfileUpdate{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	sys.waitRelay(t, 31416, false)
	sys.index(t, "ip link del wg0")
	if sys.has("ip rule del") || sys.has("ip route flush") {
		t.Fatalf("disabling removed the kill switch (its phones would go direct):\n  %s", strings.Join(sys.log, "\n  "))
	}

	on := true
	if _, err := s.Update(ctx, p.ID, ProfileUpdate{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	sys.waitRelay(t, 31416, true)
}

func TestReapplyingAConfigKeepsKillSwitchInPlace(t *testing.T) {
	sys := &fakeSystem{}
	s := newService(t, testDB(t, ""), sys)
	ctx := context.Background()
	p, err := s.Create(ctx, "Sweden", sample, true)
	if err != nil {
		t.Fatal(err)
	}
	sys.reset()
	conf := profileConf("10.64.0.9")
	if _, err := s.Update(ctx, p.ID, ProfileUpdate{Config: &conf}); err != nil {
		t.Fatal(err)
	}
	if sys.has("ip rule del") || sys.has("ip route flush") {
		t.Fatalf("re-applying removed the kill switch (leak window):\n  %s", strings.Join(sys.log, "\n  "))
	}
	if got := sys.stdin["wg setconf wg0 /dev/stdin"]; !strings.Contains(got, "PrivateKey = "+testPriv) {
		t.Fatalf("wg setconf not fed the new config")
	}
}

func TestKillSwitchFailureKeepsRelayOff(t *testing.T) {
	sys := &fakeSystem{fail: "ip rule add"}
	s := newService(t, testDB(t, ""), sys)
	p, err := s.Create(context.Background(), "Sweden", sample, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.ApplyError, ErrKillSwitch.Error()) {
		t.Fatalf("apply error = %q", p.ApplyError)
	}
	if sys.has("relay start") {
		t.Fatal("relay started without a kill switch")
	}
}

func TestTunnelFailureIsReportedButRelayRunsBehindKillSwitch(t *testing.T) {
	sys := &fakeSystem{fail: "wg setconf"}
	s := newService(t, testDB(t, ""), sys)
	p, err := s.Create(context.Background(), "Sweden", sample, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.ApplyError, "wg setconf") {
		t.Fatalf("apply error = %q", p.ApplyError)
	}
	if !sys.relayRunning(31416) {
		t.Fatal("relay not running (its phones would wait for nothing; the kill switch already blocks it)")
	}
}

func TestIPv6FailuresAreTolerated(t *testing.T) {
	sys := &fakeSystem{fail: "-6"}
	s := newService(t, testDB(t, ""), sys)
	p, err := s.Create(context.Background(), "Sweden", sample, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.ApplyError != "" {
		t.Fatalf("apply error: %s", p.ApplyError)
	}
}

func TestDeleteTearsDownFreesSlotAndUnassignsPhones(t *testing.T) {
	pool := testDB(t, "")
	sys := &fakeSystem{}
	s := newService(t, pool, sys)
	ctx := context.Background()
	p, err := s.Create(ctx, "Sweden", sample, true)
	if err != nil {
		t.Fatal(err)
	}
	addDevice(t, pool, "PHONE1")
	addDevice(t, pool, "PHONE2")
	assign(t, pool, "PHONE1", p.ID)

	serials, err := s.Delete(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(serials, []string{"PHONE1"}) {
		t.Fatalf("Delete returned %v, want the profile's phones [PHONE1]", serials)
	}
	var assigned *string
	if err := pool.QueryRow(ctx, "SELECT vpn_profile_id::text FROM devices WHERE serial = 'PHONE1'").Scan(&assigned); err != nil || assigned != nil {
		t.Fatalf("PHONE1 still assigned: %v %v", assigned, err)
	}
	sys.waitRelay(t, 31416, false)
	sys.index(t, "ip rule del uidrange 31416-31416 lookup 51820")
	sys.index(t, "ip link del wg0")

	q, err := s.Create(ctx, "Denmark", sample, true)
	if err != nil {
		t.Fatal(err)
	}
	if target, err := s.Target(ctx, q.ID); err != nil || target.Port != 31416 {
		t.Fatalf("slot 0 not reused: %+v %v", target, err)
	}
	if _, err := s.Delete(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting again: %v, want ErrNotFound", err)
	}
}

func TestProfileListAndNamesNeverIncludeSecrets(t *testing.T) {
	sys := &fakeSystem{dump: testPriv + "\t" + testPub + "\t51820\toff\n" +
		testPub + "\t" + testPSK + "\t203.0.113.7:51820\t0.0.0.0/0\t1759312800\t1234\t5678\t25\n"}
	s := newService(t, testDB(t, ""), sys)
	ctx := context.Background()
	if _, err := s.Create(ctx, "Sweden", sample, true); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names, err := s.Names(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{list, names} {
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), testPriv) || strings.Contains(string(b), testPSK) {
			t.Fatalf("leaks a secret: %s", b)
		}
	}
	if len(list) != 1 || list[0].Status == nil || !list[0].Status.Up || len(list[0].Status.Peers) != 1 ||
		list[0].Status.Peers[0].RxBytes != 1234 || list[0].Config.PublicKey == "" {
		t.Fatalf("list: %+v", list)
	}
	b, _ := json.Marshal(names)
	if string(b) != fmt.Sprintf(`[{"id":%q,"name":"Sweden"}]`, list[0].ID) {
		t.Fatalf("names = %s, want ids and names only", b)
	}
}

func TestInvalidProfilesAreRejected(t *testing.T) {
	s := newService(t, testDB(t, ""), &fakeSystem{})
	ctx := context.Background()
	var cfgErr *ConfigError
	if _, err := s.Create(ctx, "Bad", "[Interface]\nPostUp = rm -rf /\n", true); !errors.As(err, &cfgErr) {
		t.Fatalf("bad config: %v, want ConfigError", err)
	}
	if _, err := s.Create(ctx, "  ", sample, true); !errors.As(err, &cfgErr) {
		t.Fatalf("blank name: %v, want ConfigError", err)
	}
	if _, err := s.Create(ctx, "Sweden", sample, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "Sweden", sample, true); !errors.As(err, &cfgErr) {
		t.Fatalf("duplicate name: %v, want ConfigError", err)
	}
}

func TestSlotsRunOut(t *testing.T) {
	s := newService(t, testDB(t, ""), &fakeSystem{})
	ctx := context.Background()
	for i := range MaxProfiles {
		if _, err := s.Create(ctx, fmt.Sprintf("p%d", i), sample, false); err != nil {
			t.Fatalf("profile %d: %v", i, err)
		}
	}
	var cfgErr *ConfigError
	if _, err := s.Create(ctx, "one too many", sample, false); !errors.As(err, &cfgErr) {
		t.Fatalf("profile %d: %v, want ConfigError", MaxProfiles+1, err)
	}
}

func TestTetherTargetsFollowAssignments(t *testing.T) {
	pool := testDB(t, "")
	s := newService(t, pool, &fakeSystem{})
	ctx := context.Background()
	a, _ := s.Create(ctx, "Sweden", profileConf("10.64.0.1"), true)
	b, _ := s.Create(ctx, "Denmark", profileConf("10.65.0.1"), false)
	addDevice(t, pool, "PHONE1")
	addDevice(t, pool, "PHONE2")
	addDevice(t, pool, "PHONE3")
	assign(t, pool, "PHONE1", a.ID)
	assign(t, pool, "PHONE2", b.ID)

	got, err := s.TetherTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["PHONE1"].Port != 31416 || got["PHONE2"].Port != 31417 ||
		!slices.Equal(got["PHONE1"].DNS, []string{"10.64.0.1"}) || !slices.Equal(got["PHONE2"].DNS, []string{"10.65.0.1"}) {
		t.Fatalf("targets = %+v", got)
	}
}

func TestSyncAppliesStoredProfilesOnStart(t *testing.T) {
	pool := testDB(t, "")
	first := newService(t, pool, &fakeSystem{})
	ctx := context.Background()
	if _, err := first.Create(ctx, "Sweden", sample, true); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Create(ctx, "Off", sample, false); err != nil {
		t.Fatal(err)
	}
	first.Stop()

	sys := &fakeSystem{}
	restarted := newService(t, pool, sys)
	if err := restarted.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if sys.index(t, "ip rule add uidrange 31416-31416") > sys.index(t, "relay start uid=31416") {
		t.Fatal("relay started before its kill switch")
	}
	// A disabled profile is fenced off too, but has no relay or tunnel.
	sys.index(t, "ip rule add uidrange 31417-31417")
	if sys.has("relay start uid=31417") || sys.has("ip link add wg1") {
		t.Fatalf("disabled profile brought up:\n  %s", strings.Join(sys.log, "\n  "))
	}
}

// Upgrading from the single-tunnel version: phones tethered then (migration
// 005 lists them) move onto the imported "Default" profile.
func TestLegacyConfigImportedAsDefaultProfile(t *testing.T) {
	pool := testDB(t, "004_reverse_tether.sql")
	ctx := context.Background()
	for _, q := range []string{
		"INSERT INTO devices (serial, reverse_tether) VALUES ('TETHERED', true), ('PLAIN', false)",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateUpTo(ctx, pool, ""); err != nil {
		t.Fatalf("migrate to latest: %v", err)
	}

	path := filepath.Join(t.TempDir(), "wireguard.json")
	legacy, _ := json.Marshal(map[string]any{"enabled": true, "config": sample})
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	sys := &fakeSystem{}
	s := newService(t, pool, sys)
	if err := s.ImportLegacy(ctx, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("legacy file not removed")
	}
	names, _ := s.Names(ctx)
	if len(names) != 1 || names[0].Name != "Default" {
		t.Fatalf("profiles after import: %+v", names)
	}
	targets, err := s.TetherTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets["TETHERED"].Port != 31416 {
		t.Fatalf("targets after import = %+v, want TETHERED on the Default profile", targets)
	}
	// Importing again is a no-op (the file is gone).
	if err := s.ImportLegacy(ctx, path); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyTetheringWithoutConfigIsTurnedOff(t *testing.T) {
	pool := testDB(t, "004_reverse_tether.sql")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO devices (serial, reverse_tether) VALUES ('TETHERED', true)"); err != nil {
		t.Fatal(err)
	}
	if err := migrateUpTo(ctx, pool, ""); err != nil {
		t.Fatal(err)
	}
	s := newService(t, pool, &fakeSystem{})
	if err := s.ImportLegacy(ctx, filepath.Join(t.TempDir(), "wireguard.json")); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM legacy_tethered_devices").Scan(&n); err != nil || n != 0 {
		t.Fatalf("legacy list not cleared: %d %v", n, err)
	}
	if targets, _ := s.TetherTargets(ctx); len(targets) != 0 {
		t.Fatalf("targets = %v, want none", targets)
	}
}

func TestCheckExitRunsAsTheProfilesUID(t *testing.T) {
	sys := &fakeSystem{}
	s := newService(t, testDB(t, ""), sys)
	var asUID uint32
	s.CheckExitAs = func(_ context.Context, uid uint32) (string, error) {
		asUID = uid
		return "198.51.100.4", nil
	}
	ctx := context.Background()
	_, _ = s.Create(ctx, "Sweden", sample, true)
	b, _ := s.Create(ctx, "Denmark", sample, true)
	ip, err := s.CheckExit(ctx, b.ID)
	if err != nil || ip != "198.51.100.4" || asUID != 31417 {
		t.Fatalf("CheckExit = %q, %v as uid %d; want the Denmark relay's uid 31417", ip, err, asUID)
	}
}

// Routing alone lets a relay uid reach the container's own addresses (the
// local table wins over the uid rule), so a firewall also confines every
// relay uid to wg* interfaces, except replies on connections made to it.
func TestRelayFirewallRules(t *testing.T) {
	rules := relayFirewall()
	for _, want := range []string{
		"table inet batter_relay",
		"type filter hook output priority 0; policy accept;",
		"meta skuid 31416-31447 ct state established,related accept",
		`meta skuid 31416-31447 oifname "wg*" accept`,
		"meta skuid 31416-31447 reject",
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules lack %q:\n%s", want, rules)
		}
	}
	// Replaced atomically in one nft transaction: no window while re-applying.
	if !strings.HasPrefix(rules, "table inet batter_relay {}\ndelete table inet batter_relay\n") {
		t.Errorf("rules don't replace the table atomically:\n%s", rules)
	}
}

func TestFirewallGoesInBeforeRelay(t *testing.T) {
	sys := &fakeSystem{}
	s := newService(t, testDB(t, ""), sys)
	if _, err := s.Create(context.Background(), "Sweden", sample, true); err != nil {
		t.Fatal(err)
	}
	if sys.index(t, "nft -f /dev/stdin") > sys.index(t, "relay start") {
		t.Fatal("relay started before the firewall")
	}
	if got := sys.stdin["nft -f /dev/stdin"]; got != relayFirewall() {
		t.Fatalf("nft fed %q", got)
	}
}

func TestFirewallFailureKeepsRelayOff(t *testing.T) {
	sys := &fakeSystem{fail: "nft"}
	s := newService(t, testDB(t, ""), sys)
	p, err := s.Create(context.Background(), "Sweden", sample, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.ApplyError, ErrKillSwitch.Error()) || sys.has("relay start") {
		t.Fatalf("relay ran without the firewall (apply error %q)", p.ApplyError)
	}
}
