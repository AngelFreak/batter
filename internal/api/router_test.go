package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/api"
	"github.com/XpertaDK/batter/internal/api/handlers"
	"github.com/XpertaDK/batter/internal/auth"
	"github.com/XpertaDK/batter/internal/device"
	"github.com/XpertaDK/batter/internal/migrate"
	"github.com/XpertaDK/batter/internal/tether"
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
}

func newTestEnv(t *testing.T) *testEnv {
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
	}
	router, err := api.NewRouter(api.RouterConfig{
		DeviceManager: dm,
		DB:            db,
		JWTManager:    env.jwt,
		Logger:        logger,
		Tether:        &tether.Controller{ADB: dm.ADB(), APK: "/nonexistent/gnirehtet.apk", Logger: logger},
		// Never touch the test machine's network.
		VPN: &vpn.Manager{
			Path:   filepath.Join(t.TempDir(), "wireguard.json"),
			UID:    tether.RelayUID,
			Run:    func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil },
			Logger: logger,
		},
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
		{"PUT", base + "/tether", `{"enabled":false}`, "manage"},
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

func TestTetherToggleNeedsManageAndPersists(t *testing.T) {
	env := newTestEnv(t)
	path := "/api/v1/devices/" + testSerial + "/tether"

	for _, user := range []string{"viewer", "controller"} {
		if w := env.do(t, user, "PUT", path, `{"enabled":true}`); w.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d, want 403", user, w.Code)
		}
	}

	// The test device isn't attached, so applying fails, but the setting is
	// kept and applied when the device connects.
	w := env.do(t, "manager", "PUT", path, `{"enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("manager: status %d; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ReverseTether bool   `json:"reverse_tether"`
		ApplyError    string `json:"apply_error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || !resp.ReverseTether || resp.ApplyError == "" {
		t.Fatalf("want reverse_tether=true and the apply error reported; body: %s", w.Body.String())
	}
	var stored bool
	if err := env.db.QueryRow(context.Background(), "SELECT reverse_tether FROM devices WHERE serial = $1", testSerial).Scan(&stored); err != nil || !stored {
		t.Fatalf("reverse_tether not stored (got %v, err %v)", stored, err)
	}

	// The reconcile loop sees the stored setting (and applies it on connect).
	if serials, err := handlers.TetherEnabledSerials(context.Background(), env.db); err != nil || !slices.Equal(serials, []string{testSerial}) {
		t.Fatalf("TetherEnabledSerials = %v, %v; want [%s]", serials, err, testSerial)
	}

	w = env.do(t, "manager", "GET", "/api/v1/devices/"+testSerial, "")
	var dev struct {
		ReverseTether bool `json:"reverse_tether"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &dev); err != nil || !dev.ReverseTether {
		t.Fatalf("GET device doesn't report reverse_tether=true: %s", w.Body.String())
	}

	if w := env.do(t, "manager", "PUT", path, `{"enabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("disable: status %d", w.Code)
	}
	if err := env.db.QueryRow(context.Background(), "SELECT reverse_tether FROM devices WHERE serial = $1", testSerial).Scan(&stored); err != nil || stored {
		t.Fatalf("reverse_tether still set after disable")
	}

	if w := env.do(t, "admin", "PUT", "/api/v1/devices/NOSUCHDEVICE/tether", `{"enabled":true}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown device: status %d, want 404", w.Code)
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

func TestVPNRoutesAreAdminOnly(t *testing.T) {
	env := newTestEnv(t)
	body, _ := json.Marshal(map[string]any{"config": vpnTestConfig, "enabled": true})
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/vpn", ""},
		{"PUT", "/api/v1/vpn", string(body)},
		{"POST", "/api/v1/vpn/check", ""},
		{"DELETE", "/api/v1/vpn", ""},
	}
	for _, rt := range routes {
		for _, user := range []string{"viewer", "manager", "operator"} {
			if w := env.do(t, user, rt.method, rt.path, rt.body); w.Code != http.StatusForbidden {
				t.Errorf("%s %s as %s: status %d, want 403", rt.method, rt.path, user, w.Code)
			}
		}
		if w := env.do(t, "admin", rt.method, rt.path, rt.body); w.Code == http.StatusForbidden || w.Code == http.StatusNotFound {
			t.Errorf("%s %s as admin: status %d", rt.method, rt.path, w.Code)
		}
	}
}

func TestVPNConfigNeverReturnsPrivateKey(t *testing.T) {
	env := newTestEnv(t)
	body, _ := json.Marshal(map[string]any{"config": vpnTestConfig, "enabled": true})

	w := env.do(t, "admin", "PUT", "/api/v1/vpn", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: status %d; body: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), vpnTestPriv) {
		t.Fatalf("PUT response contains the private key: %s", w.Body.String())
	}

	w = env.do(t, "admin", "GET", "/api/v1/vpn", "")
	if strings.Contains(w.Body.String(), vpnTestPriv) {
		t.Fatalf("GET response contains the private key: %s", w.Body.String())
	}
	var info struct {
		Configured bool `json:"configured"`
		Enabled    bool `json:"enabled"`
		Config     struct {
			PublicKey string `json:"public_key"`
			Peers     []struct {
				Endpoint string `json:"endpoint"`
			} `json:"peers"`
		} `json:"config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.Configured || !info.Enabled || info.Config.PublicKey == "" ||
		len(info.Config.Peers) != 1 || info.Config.Peers[0].Endpoint != "vpn.example.net:51820" {
		t.Fatalf("GET doesn't describe the saved config: %s", w.Body.String())
	}

	// Toggling doesn't need the config (and its key) sent again.
	if w := env.do(t, "admin", "PUT", "/api/v1/vpn", `{"enabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("toggle: status %d; body: %s", w.Code, w.Body.String())
	}

	bad, _ := json.Marshal(map[string]any{"config": "[Interface]\nPostUp = echo hi\n", "enabled": true})
	if w := env.do(t, "admin", "PUT", "/api/v1/vpn", string(bad)); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid config: status %d, want 400", w.Code)
	}
}
