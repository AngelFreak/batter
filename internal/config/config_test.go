package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
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
