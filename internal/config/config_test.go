package config

import (
	"log/slog"
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
