package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/api"
	"github.com/XpertaDK/batter/internal/api/handlers"
	"github.com/XpertaDK/batter/internal/auth"
	"github.com/XpertaDK/batter/internal/device"
	"github.com/XpertaDK/batter/internal/migrate"
	"github.com/XpertaDK/batter/internal/vpn"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests drive the real router (api.NewRouter) against a real Postgres so
// that the RBAC wiring — which middleware guards which route — is what's under
// test, not the middleware in isolation.
//
// They need a Postgres the test may create/drop databases on, e.g.:
//
//	BATTER_TEST_DATABASE_URL=postgres://batter:batter@localhost:5434/batter?sslmode=disable go test ./internal/api/
//
// and `adb` on PATH (the device manager requires it). Otherwise they skip.

const testSerial = "TESTSERIAL01"

type testEnv struct {
	router *gin.Engine
	db     *pgxpool.Pool
	jwt    *auth.JWTManager
	users  map[string]string // username -> id
	lan    *fakeLAN
	dm     *device.Manager
}

// fakeLAN stands in for the phone LAN's controller, recording reloads.
type fakeLAN struct {
	mu      sync.Mutex
	reloads int
	err     error
}

func (l *fakeLAN) Reload(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reloads++
	return l.err
}

func (l *fakeLAN) Provision(context.Context, string) error { return l.err }

func (l *fakeLAN) NeedsReprovision(string) (netip.Addr, bool) { return netip.Addr{}, false }

func (l *fakeLAN) reloaded() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reloads
}

// envSetup lets a test swap parts of the environment before the router is
// built (see withRealLAN).
type envSetup struct {
	db  *pgxpool.Pool
	dm  *device.Manager
	vpn *vpn.Service
	lan handlers.PhoneLAN
}

func newTestEnv(t *testing.T, opts ...func(*envSetup)) *testEnv {
	t.Helper()

	adminURL := os.Getenv("BATTER_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("BATTER_TEST_DATABASE_URL not set")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dm := newDeviceManager(t, logger)

	ctx := context.Background()
	db := createTestDatabase(t, ctx, adminURL)
	if err := migrate.Up(ctx, db, logger); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	env := &testEnv{
		db:    db,
		jwt:   auth.NewJWTManager("test-secret", 3600),
		users: map[string]string{},
		lan:   &fakeLAN{},
		dm:    dm,
	}
	vpnSvc := &vpn.Service{
		DB: db,
		// Never touch the test machine's network.
		Run:    func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil },
		Logger: logger,
	}
	setup := &envSetup{db: db, dm: dm, vpn: vpnSvc, lan: env.lan}
	for _, o := range opts {
		o(setup)
	}
	router, err := api.NewRouter(api.RouterConfig{
		DeviceManager: dm,
		DB:            db,
		JWTManager:    env.jwt,
		Logger:        logger,
		LAN:           setup.lan,
		VPN:           vpnSvc,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.router = router

	for _, u := range []struct{ name, role string }{
		{"admin", "admin"},
		{"operator", "operator"},
		{"viewer", "viewer"},
		{"controller", "viewer"},
		{"manager", "viewer"},
		{"stranger", "viewer"},
	} {
		var id string
		err := db.QueryRow(ctx,
			"INSERT INTO users (username, password_hash, role) VALUES ($1, 'x', $2) RETURNING id",
			u.name, u.role,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert user %s: %v", u.name, err)
		}
		env.users[u.name] = id
	}
	env.seedDevice(t)
	return env
}

// newDeviceManager returns a real device manager (NewRouter needs one). It
// requires adb on PATH; the scrcpy server is a stub since no device is used.
func newDeviceManager(t *testing.T, logger *slog.Logger) *device.Manager {
	t.Helper()
	if _, err := exec.LookPath("adb"); err != nil {
		t.Skip("adb not on PATH")
	}
	scrcpy := filepath.Join(t.TempDir(), "scrcpy-server")
	if err := os.WriteFile(scrcpy, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	dm, err := device.NewManager(device.ManagerConfig{ScrcpyServerPath: scrcpy, Logger: logger})
	if err != nil {
		t.Fatalf("device manager: %v", err)
	}
	t.Cleanup(dm.Shutdown)
	return dm
}

func createTestDatabase(t *testing.T, ctx context.Context, adminURL string) *pgxpool.Pool {
	t.Helper()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	name := fmt.Sprintf("batter_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatalf("create test database: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	return db
}

// seedDevice (re)creates the test device with per-user grants. Called again
// after a test deletes the device, since grants cascade with it.
func (e *testEnv) seedDevice(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.db.Exec(ctx, "DELETE FROM devices WHERE serial = $1", testSerial); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "INSERT INTO devices (serial) VALUES ($1)", testSerial); err != nil {
		t.Fatal(err)
	}
	for user, perm := range map[string]string{"viewer": "view", "controller": "control", "manager": "manage"} {
		_, err := e.db.Exec(ctx,
			"INSERT INTO user_device_access (user_id, device_serial, permission) VALUES ($1, $2, $3)",
			e.users[user], testSerial, perm,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func (e *testEnv) do(t *testing.T, user, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, err := e.jwt.GenerateToken(e.users[user], user, e.role(t, user))
	if err != nil {
		t.Fatal(err)
	}
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *testEnv) role(t *testing.T, user string) string {
	t.Helper()
	var role string
	if err := e.db.QueryRow(context.Background(), "SELECT role FROM users WHERE username = $1", user).Scan(&role); err != nil {
		t.Fatal(err)
	}
	return role
}

func TestDeviceRoutesEnforcePermissionLevels(t *testing.T) {
	env := newTestEnv(t)
	base := "/api/v1/devices/" + testSerial

	routes := []struct {
		method, path, body, required string
	}{
		// Viewing a device needs a running session, so these stay at "view".
		{"GET", base, "", "view"},
		{"POST", base + "/session/start", "", "view"},
		{"POST", base + "/session/upgrade", "", "view"},
		{"POST", base + "/session/downgrade", "", "view"},
		// Acting on the device.
		{"POST", base + "/session/stop", "", "control"},
		{"POST", base + "/wake", "", "control"},
		{"POST", base + "/push", "", "control"},
		{"POST", base + "/install", "", "control"},
		// Changing Batter's record of the device.
		{"PUT", base, `{"nickname":"n"}`, "manage"},
		{"PUT", base + "/tether", `{"profile_id":null}`, "manage"},
		{"DELETE", base, "", "manage"},
	}
	users := map[string]int{"stranger": -1, "viewer": 0, "controller": 1, "manager": 2, "admin": 99}
	levels := map[string]int{"view": 0, "control": 1, "manage": 2}

	for _, rt := range routes {
		for user, level := range users {
			wantForbidden := level < levels[rt.required]
			t.Run(fmt.Sprintf("%s %s as %s", rt.method, strings.TrimPrefix(rt.path, base), user), func(t *testing.T) {
				env.seedDevice(t)
				w := env.do(t, user, rt.method, rt.path, rt.body)
				if gotForbidden := w.Code == http.StatusForbidden; gotForbidden != wantForbidden {
					t.Fatalf("status %d (forbidden=%v), want forbidden=%v; body: %s",
						w.Code, gotForbidden, wantForbidden, w.Body.String())
				}
			})
		}
	}
}

func TestDeviceAudioSocketNeedsViewPermission(t *testing.T) {
	env := newTestEnv(t)
	for user, wantAllowed := range map[string]bool{"stranger": false, "viewer": true, "admin": true} {
		t.Run(user, func(t *testing.T) {
			token, err := env.jwt.GenerateToken(env.users[user], user, env.role(t, user))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/ws/device/"+testSerial+"/audio?token="+token, nil)
			w := httptest.NewRecorder()
			env.router.ServeHTTP(w, req)
			// Allowed callers reach the handler, which 404s without a session
			// (unlike gin's bare 404 for an unregistered route).
			allowed := w.Code == http.StatusNotFound && strings.Contains(w.Body.String(), "no active session")
			if allowed != wantAllowed || (!allowed && w.Code != http.StatusForbidden) {
				t.Fatalf("status %d body %q, want allowed=%v", w.Code, w.Body.String(), wantAllowed)
			}
		})
	}
}

func TestGroupMutationsRequireAdmin(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	var groupID, teamID string
	if err := env.db.QueryRow(ctx, "INSERT INTO device_groups (name) VALUES ('g') RETURNING id").Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if err := env.db.QueryRow(ctx, "INSERT INTO user_groups (name) VALUES ('team') RETURNING id").Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	g := "/api/v1/groups/" + groupID

	adminOnly := []struct{ method, path, body string }{
		{"POST", "/api/v1/groups", `{"name":"new"}`},
		{"PUT", g, `{"name":"renamed"}`},
		{"POST", g + "/devices", `{"serials":["` + testSerial + `"]}`},
		{"DELETE", g + "/devices/" + testSerial, ""},
		{"GET", g + "/access", ""},
		{"GET", g + "/team-access", ""},
		{"POST", g + "/team-access", `{"user_group_id":"` + teamID + `","permission":"control"}`},
		{"DELETE", g, ""}, // last: removes the group
	}
	for _, rt := range adminOnly {
		for _, user := range []string{"viewer", "operator"} {
			t.Run(rt.method+" "+rt.path+" as "+user, func(t *testing.T) {
				if w := env.do(t, user, rt.method, rt.path, rt.body); w.Code != http.StatusForbidden {
					t.Fatalf("status %d, want 403; body: %s", w.Code, w.Body.String())
				}
			})
		}
		t.Run(rt.method+" "+rt.path+" as admin", func(t *testing.T) {
			if w := env.do(t, "admin", rt.method, rt.path, rt.body); w.Code == http.StatusForbidden {
				t.Fatalf("admin got 403; body: %s", w.Body.String())
			}
		})
	}

	t.Run("listing groups stays open to any user", func(t *testing.T) {
		if w := env.do(t, "viewer", "GET", "/api/v1/groups", ""); w.Code != http.StatusOK {
			t.Fatalf("status %d, want 200; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestBatchOpsSkipDevicesCallerCannotAct(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	var groupID string
	if err := env.db.QueryRow(ctx, "INSERT INTO device_groups (name) VALUES ('batch') RETURNING id").Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(ctx, "INSERT INTO device_group_members (device_serial, group_id) VALUES ($1, $2)", testSerial, groupID); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		user, op    string
		wantSkipped int
	}{
		{"stranger", "start", 1}, // no access at all
		{"viewer", "start", 0},   // view is enough to start a session
		{"viewer", "stop", 1},    // but not to stop one
		{"controller", "stop", 0},
		{"admin", "stop", 0},
	}
	for _, tc := range cases {
		t.Run(tc.op+" as "+tc.user, func(t *testing.T) {
			w := env.do(t, tc.user, "POST", "/api/v1/groups/"+groupID+"/batch/"+tc.op, "")
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, want 200; body: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Skipped *int `json:"skipped"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Skipped == nil {
				t.Fatalf("response missing skipped count: %s", w.Body.String())
			}
			if *resp.Skipped != tc.wantSkipped {
				t.Fatalf("skipped = %d, want %d; body: %s", *resp.Skipped, tc.wantSkipped, w.Body.String())
			}
		})
	}
}

func TestRequestLogsNeverContainWSToken(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router, err := api.NewRouter(api.RouterConfig{
		DeviceManager: newDeviceManager(t, logger),
		JWTManager:    auth.NewJWTManager("test-secret", 3600),
		Logger:        logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/ws/device/X/video?token=super-secret-jwt&other=1", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	logs := buf.String()
	if strings.Contains(logs, "super-secret-jwt") {
		t.Fatalf("token leaked into logs: %s", logs)
	}
	if !strings.Contains(logs, "other=1") {
		t.Fatalf("non-secret query params should still be logged: %s", logs)
	}
}

func TestLoginRateLimitKeysOnTrustedClientIP(t *testing.T) {
	env := newTestEnv(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dm := newDeviceManager(t, logger)

	// httptest requests come from 192.0.2.1. Each attempt claims a different
	// X-Forwarded-For; returns how many of 11 attempts were rate limited.
	limited := func(trusted []string) int {
		router, err := api.NewRouter(api.RouterConfig{
			DeviceManager: dm, DB: env.db, JWTManager: env.jwt, Logger: logger, TrustedProxies: trusted,
		})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for i := 0; i < 11; i++ {
			req := httptest.NewRequest("POST", "/api/v1/auth/login",
				strings.NewReader(`{"username":"nobody","password":"wrong"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code == http.StatusTooManyRequests {
				n++
			}
		}
		return n
	}

	t.Run("untrusted peer cannot dodge the limit by spoofing X-Forwarded-For", func(t *testing.T) {
		if got := limited(nil); got != 1 {
			t.Fatalf("%d of 11 attempts limited, want 1", got)
		}
	})
	t.Run("trusted proxy's X-Forwarded-For identifies distinct clients", func(t *testing.T) {
		if got := limited([]string{"192.0.2.1"}); got != 0 {
			t.Fatalf("%d of 11 attempts limited, want 0", got)
		}
	})
}

func TestScreenLockEndpointsNeedOperator(t *testing.T) {
	env := newTestEnv(t)
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/devices/lock/" + testSerial, ""},
		{"POST", "/api/v1/devices/lock/" + testSerial + "/remove", `{"credential":"1234"}`},
	}
	for _, rt := range routes {
		// A per-device "manage" grant isn't enough: preparing phones is an
		// operator task, like registering them.
		for _, user := range []string{"viewer", "manager"} {
			if w := env.do(t, user, rt.method, rt.path, rt.body); w.Code != http.StatusForbidden {
				t.Fatalf("%s %s as %s: status %d, want 403", rt.method, rt.path, user, w.Code)
			}
		}
		// The test device isn't attached, so the operator gets an error, but
		// not a permission error.
		if w := env.do(t, "operator", rt.method, rt.path, rt.body); w.Code == http.StatusForbidden || w.Code == http.StatusNotFound {
			t.Fatalf("%s %s as operator: status %d", rt.method, rt.path, w.Code)
		}
	}
}

// Throwaway WireGuard keys for the VPN tests.
const (
	vpnTestPriv = "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	vpnTestPeer = "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
)

var vpnTestConfig = `[Interface]
PrivateKey = ` + vpnTestPriv + `
Address = 10.64.0.2/32
DNS = 10.64.0.1

[Peer]
PublicKey = ` + vpnTestPeer + `
Endpoint = vpn.example.net:51820
AllowedIPs = 0.0.0.0/0
`

// createProfile creates a VPN profile as admin and returns its id.
func (e *testEnv) createProfile(t *testing.T, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "config": vpnTestConfig, "enabled": true})
	w := e.do(t, "admin", "POST", "/api/v1/vpn/profiles", string(body))
	var p struct {
		ID string `json:"id"`
	}
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.ID == "" {
		t.Fatalf("create profile: %d %s", w.Code, w.Body.String())
	}
	return p.ID
}

func (e *testEnv) deviceProfile(t *testing.T) *string {
	t.Helper()
	var id *string
	if err := e.db.QueryRow(context.Background(), "SELECT vpn_profile_id::text FROM devices WHERE serial = $1", testSerial).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTetherAssignmentNeedsManageAndPersists(t *testing.T) {
	env := newTestEnv(t)
	path := "/api/v1/devices/" + testSerial + "/tether"
	profile := env.createProfile(t, "Sweden")
	assign := `{"profile_id":"` + profile + `"}`

	for _, user := range []string{"viewer", "controller"} {
		if w := env.do(t, user, "PUT", path, assign); w.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d, want 403", user, w.Code)
		}
	}

	// The phone LAN's routing follows the assignment straight away.
	reloads := env.lan.reloaded()
	w := env.do(t, "manager", "PUT", path, assign)
	if w.Code != http.StatusOK {
		t.Fatalf("manager: status %d; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ProfileID  *string `json:"vpn_profile_id"`
		ApplyError string  `json:"apply_error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.ProfileID == nil || *resp.ProfileID != profile || resp.ApplyError != "" {
		t.Fatalf("want the profile stored and applied; body: %s", w.Body.String())
	}
	if got := env.deviceProfile(t); got == nil || *got != profile {
		t.Fatalf("profile not stored: %v", got)
	}
	if env.lan.reloaded() <= reloads {
		t.Fatal("assignment stored but the LAN's routing not reloaded")
	}

	// When applying fails the setting is still kept; the error is reported.
	env.lan.err = errors.New("nft: Operation not permitted")
	w = env.do(t, "manager", "PUT", path, assign)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"apply_error":"nft: Operation not permitted"`) {
		t.Fatalf("failed apply: %d %s", w.Code, w.Body.String())
	}
	env.lan.err = nil

	w = env.do(t, "manager", "GET", "/api/v1/devices/"+testSerial, "")
	var dev struct {
		ProfileID   string `json:"vpn_profile_id"`
		ProfileName string `json:"vpn_profile_name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &dev); err != nil || dev.ProfileID != profile || dev.ProfileName != "Sweden" {
		t.Fatalf("GET device doesn't report its profile: %s", w.Body.String())
	}

	for name, body := range map[string]string{
		"unknown profile": `{"profile_id":"00000000-0000-0000-0000-000000000000"}`,
		"not a uuid":      `{"profile_id":"sweden"}`,
		"no profile_id":   `{}`,
	} {
		if w := env.do(t, "manager", "PUT", path, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, w.Code)
		}
	}

	if w := env.do(t, "manager", "PUT", path, `{"profile_id":null}`); w.Code != http.StatusOK {
		t.Fatalf("turn off: status %d", w.Code)
	}
	if got := env.deviceProfile(t); got != nil {
		t.Fatalf("profile still set after turning tethering off: %s", *got)
	}

	if w := env.do(t, "admin", "PUT", "/api/v1/devices/NOSUCHDEVICE/tether", assign); w.Code != http.StatusNotFound {
		t.Fatalf("unknown device: status %d, want 404", w.Code)
	}
}

func TestDeletingAProfileTurnsItsPhonesOff(t *testing.T) {
	env := newTestEnv(t)
	profile := env.createProfile(t, "Sweden")
	if w := env.do(t, "manager", "PUT", "/api/v1/devices/"+testSerial+"/tether", `{"profile_id":"`+profile+`"}`); w.Code != http.StatusOK {
		t.Fatalf("assign: %d %s", w.Code, w.Body.String())
	}
	reloads := env.lan.reloaded()

	w := env.do(t, "admin", "DELETE", "/api/v1/vpn/profiles/"+profile, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), testSerial) {
		t.Fatalf("delete: %d %s (want the affected phone listed)", w.Code, w.Body.String())
	}
	if got := env.deviceProfile(t); got != nil {
		t.Fatalf("phone still on the deleted profile")
	}
	if env.lan.reloaded() <= reloads {
		t.Fatal("deleting the profile didn't reload the LAN's routing")
	}
}

func TestVPNProfileRoutesAreAdminOnly(t *testing.T) {
	env := newTestEnv(t)
	id := env.createProfile(t, "Sweden")
	body, _ := json.Marshal(map[string]any{"name": "Norway", "config": vpnTestConfig})
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/vpn/profiles", ""},
		{"POST", "/api/v1/vpn/profiles", string(body)},
		{"GET", "/api/v1/vpn/profiles/" + id, ""},
		{"PUT", "/api/v1/vpn/profiles/" + id, `{"enabled":false}`},
		{"POST", "/api/v1/vpn/profiles/" + id + "/check", ""},
		{"DELETE", "/api/v1/vpn/profiles/" + id, ""}, // last: removes it
	}
	for _, rt := range routes {
		for _, user := range []string{"viewer", "manager", "operator"} {
			if w := env.do(t, user, rt.method, rt.path, rt.body); w.Code != http.StatusForbidden {
				t.Errorf("%s %s as %s: status %d, want 403", rt.method, rt.path, user, w.Code)
			}
		}
		if w := env.do(t, "admin", rt.method, rt.path, rt.body); w.Code >= 400 {
			t.Errorf("%s %s as admin: status %d; body: %s", rt.method, rt.path, w.Code, w.Body.String())
		}
	}
}

func TestProfileNamesAreListedForEveryone(t *testing.T) {
	env := newTestEnv(t)
	id := env.createProfile(t, "Sweden")
	w := env.do(t, "viewer", "GET", "/api/v1/vpn/profile-names", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	want := `{"profiles":[{"id":"` + id + `","name":"Sweden"}]}`
	if w.Body.String() != want {
		t.Fatalf("body %s, want ids and names only: %s", w.Body.String(), want)
	}
}

func TestVPNProfilesNeverReturnPrivateKey(t *testing.T) {
	env := newTestEnv(t)
	body, _ := json.Marshal(map[string]any{"name": "Sweden", "config": vpnTestConfig, "enabled": true})
	w := env.do(t, "admin", "POST", "/api/v1/vpn/profiles", string(body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var p struct {
		ID     string `json:"id"`
		Config struct {
			PublicKey string `json:"public_key"`
			Peers     []struct {
				Endpoint string `json:"endpoint"`
			} `json:"peers"`
		} `json:"config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.Config.PublicKey == "" ||
		len(p.Config.Peers) != 1 || p.Config.Peers[0].Endpoint != "vpn.example.net:51820" {
		t.Fatalf("create response doesn't describe the profile: %s", w.Body.String())
	}
	responses := []*httptest.ResponseRecorder{
		w,
		env.do(t, "admin", "GET", "/api/v1/vpn/profiles", ""),
		env.do(t, "admin", "GET", "/api/v1/vpn/profiles/"+p.ID, ""),
		env.do(t, "admin", "PUT", "/api/v1/vpn/profiles/"+p.ID, `{"name":"Sverige"}`),
	}
	for i, r := range responses {
		if strings.Contains(r.Body.String(), vpnTestPriv) {
			t.Fatalf("response %d contains the private key: %s", i, r.Body.String())
		}
	}

	bad, _ := json.Marshal(map[string]any{"name": "Bad", "config": "[Interface]\nPostUp = echo hi\n"})
	if w := env.do(t, "admin", "POST", "/api/v1/vpn/profiles", string(bad)); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid config: status %d, want 400", w.Code)
	}
	if w := env.do(t, "admin", "GET", "/api/v1/vpn/profiles/00000000-0000-0000-0000-000000000000", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown profile: status %d, want 404", w.Code)
	}
}

// Every open dashboard polls the device list; through the real router, a
// burst of polls must share one `adb devices` run.
func TestDeviceListPollingSharesOneADBRun(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	fake := "#!/bin/sh\necho \"$@\" >> " + calls + "\n" +
		"case \"$*\" in devices*) sleep 0.2; printf 'List of devices attached\\n" + testSerial + " device\\n' ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "adb"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	env := newTestEnv(t)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := env.do(t, "admin", "GET", "/api/v1/devices", ""); w.Code != http.StatusOK {
				t.Errorf("status %d", w.Code)
			}
		}()
	}
	wg.Wait()
	b, _ := os.ReadFile(calls)
	if n := strings.Count(string(b), "devices -l"); n != 1 {
		t.Fatalf("%d `adb devices` runs for 20 concurrent polls, want 1", n)
	}
}
