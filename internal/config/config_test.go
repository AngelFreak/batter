package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadLogLevel(t *testing.T) {
	cases := map[string]slog.Level{"": slog.LevelInfo, "debug": slog.LevelDebug, "WARN": slog.LevelWarn}
	for in, want := range cases {
		t.Run("LOG_LEVEL="+in, func(t *testing.T) {
			t.Setenv("JWT_SECRET", "s")
			t.Setenv("LOG_LEVEL", in)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.LogLevel != want {
				t.Fatalf("LogLevel = %v, want %v", cfg.LogLevel, want)
			}
		})
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid LOG_LEVEL")
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("TRUSTED_PROXIES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TrustedProxies != nil {
		t.Fatalf("default TrustedProxies = %v, want nil (trust none)", cfg.TrustedProxies)
	}

	t.Setenv("TRUSTED_PROXIES", "127.0.0.1, 10.0.0.0/8")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"127.0.0.1", "10.0.0.0/8"}; !reflect.DeepEqual(cfg.TrustedProxies, want) {
		t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
}

func TestLoadGeneratesAndPersistsJWTSecretWhenUnset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DATA_DIR", dir)

	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.JWTSecret) < 64 {
		t.Fatalf("generated secret %q is too short", first.JWTSecret)
	}
	info, err := os.Stat(filepath.Join(dir, "jwt-secret"))
	if err != nil {
		t.Fatalf("secret not persisted: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secret file mode %v, want 0600", perm)
	}

	// A restart must reuse it, or every user is logged out on each deploy.
	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if second.JWTSecret != first.JWTSecret {
		t.Fatal("secret changed across restarts")
	}
}

func TestLoadPrefersJWTSecretFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("JWT_SECRET", "from-env")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWTSecret != "from-env" {
		t.Fatalf("JWTSecret = %q, want from-env", cfg.JWTSecret)
	}
	if _, err := os.Stat(filepath.Join(dir, "jwt-secret")); !os.IsNotExist(err) {
		t.Fatal("secret file written even though JWT_SECRET was set")
	}
}

// Inside a container, "gateway" names the default gateway: connections to
// a port docker publishes (caddy on the host -> 127.0.0.1:8080) arrive from
// it.
func TestTrustedProxiesGatewayResolvesDefaultGateway(t *testing.T) {
	routes := filepath.Join(t.TempDir(), "route")
	// /proc/net/route: gateway 172.18.0.1, little-endian hex.
	table := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t010012AC\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
		"eth0\t000012AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n"
	if err := os.WriteFile(routes, []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}
	routeFile = routes
	t.Cleanup(func() { routeFile = "/proc/net/route" })
	t.Setenv("JWT_SECRET", "x")
	t.Setenv("TRUSTED_PROXIES", "127.0.0.1, gateway")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.TrustedProxies, ","); got != "127.0.0.1,172.18.0.1" {
		t.Fatalf("TrustedProxies = %s, want 127.0.0.1,172.18.0.1", got)
	}

	routeFile = filepath.Join(t.TempDir(), "missing")
	if _, err := Load(); err == nil {
		t.Fatal("gateway with no readable route table accepted")
	}
}
