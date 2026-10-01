package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all application configuration.
type Config struct {
	// Server
	Port int
	Host string
	// TrustedProxies lists proxy IPs/CIDRs whose X-Forwarded-For is believed
	// when determining the client IP (rate limiting, audit log). nil trusts
	// none: the TCP peer address is the client.
	TrustedProxies []string
	LogLevel       slog.Level

	// Database
	DatabaseURL string

	// Device
	ScrcpyServerPath string
	ScrcpyVersion    string

	// VPNExitIPURL answers with the caller's public IP (plain text); used to
	// check where a VPN profile's phones exit.
	VPNExitIPURL string

	// Auth
	JWTSecret     string
	JWTExpirySecs int
	// AllowedOrigins lists browser origins allowed to call the API and open
	// WebSockets. Empty means same-origin only, which is right whenever the
	// UI and API are served from one host (the default deployment).
	AllowedOrigins []string

	// Data
	DataDir string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		Port:             getEnvInt("PORT", 8080),
		Host:             getEnv("HOST", "0.0.0.0"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://batter:batter@localhost:5432/batter?sslmode=disable"),
		ScrcpyServerPath: getEnv("SCRCPY_SERVER_PATH", "/usr/local/share/scrcpy/scrcpy-server"),
		ScrcpyVersion:    getEnv("SCRCPY_VERSION", "3.3.4"),
		VPNExitIPURL:     getEnv("VPN_EXIT_IP_URL", "https://api.ipify.org"),
		JWTSecret:        getEnv("JWT_SECRET", ""),
		DataDir:          getEnv("DATA_DIR", "./data"),
		JWTExpirySecs:    getEnvInt("JWT_EXPIRY_SECS", 3600),
	}

	if cfg.JWTSecret == "" {
		secret, err := loadOrCreateSecret(filepath.Join(cfg.DataDir, "jwt-secret"))
		if err != nil {
			return nil, fmt.Errorf("JWT_SECRET not set and no usable generated secret: %w", err)
		}
		cfg.JWTSecret = secret
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(getEnv("LOG_LEVEL", "info"))); err != nil {
		return nil, fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}

	for _, p := range splitAndTrim(getEnv("TRUSTED_PROXIES", "")) {
		if p == "gateway" {
			gw, err := defaultGateway()
			if err != nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES gateway: %w", err)
			}
			p = gw
		}
		if net.ParseIP(p) == nil {
			if _, _, err := net.ParseCIDR(p); err != nil {
				return nil, fmt.Errorf("invalid TRUSTED_PROXIES entry %q: want an IP or CIDR", p)
			}
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, p)
	}

	// Parse allowed origins
	if origins := getEnv("ALLOWED_ORIGINS", ""); origins != "" {
		for _, o := range splitAndTrim(origins) {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
		}
	}

	return cfg, nil
}

// loadOrCreateSecret returns the secret stored at path, generating one (32
// random bytes, hex) on first use. It lives in the data volume so tokens stay
// valid across restarts and redeploys without anyone managing a secret.
func loadOrCreateSecret(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return "", err
	}
	return secret, nil
}

// routeFile is the kernel's IPv4 routing table (overridable in tests).
var routeFile = "/proc/net/route"

// defaultGateway returns the default route's gateway. In a container that is
// the docker bridge, which connections to the container's published ports
// come from (via docker-proxy or NAT), so caddy on the host appears as it.
func defaultGateway() (string, error) {
	b, err := os.ReadFile(routeFile)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		gw, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil || gw == 0 {
			continue
		}
		// The table stores addresses in host (little-endian) byte order.
		return net.IPv4(byte(gw), byte(gw>>8), byte(gw>>16), byte(gw>>24)).String(), nil
	}
	return "", fmt.Errorf("no default route in %s", routeFile)
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}

func splitAndTrim(s string) []string {
	var result []string
	for _, part := range split(s, ",") {
		trimmed := trim(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func split(s, sep string) []string {
	var parts []string
	for {
		i := indexOf(s, sep)
		if i < 0 {
			parts = append(parts, s)
			break
		}
		parts = append(parts, s[:i])
		s = s[i+len(sep):]
	}
	return parts
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
