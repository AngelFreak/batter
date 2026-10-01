package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestOriginCheckDefaultsToSameOrigin(t *testing.T) {
	cases := []struct {
		name, origin, host, forwardedHost string
		want                              bool
	}{
		{"direct, same host", "http://batter.lan:8080", "batter.lan:8080", "", true},
		{"via Next proxy (Host rewritten, X-Forwarded-Host kept)", "https://batter.example.com", "localhost:8080", "batter.example.com", true},
		{"LAN IP, no config needed", "http://192.168.1.50:3000", "localhost:8080", "192.168.1.50:3000", true},
		{"default port omitted by browser", "https://batter.example.com", "batter.example.com:443", "", true},
		{"cross-site page", "https://evil.example", "localhost:8080", "batter.example.com", false},
		{"same host, different port", "http://batter.lan:9999", "batter.lan:8080", "", false},
		{"localhost is not special anymore", "http://localhost:5173", "batter.lan:8080", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/ws/device/x/video", nil)
			r.Host = tc.host
			if tc.forwardedHost != "" {
				r.Header.Set("X-Forwarded-Host", tc.forwardedHost)
			}
			if got := IsOriginAllowed(tc.origin, r, nil); got != tc.want {
				t.Fatalf("IsOriginAllowed(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}

func TestOriginCheckExplicitListOverridesSameOrigin(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "batter.lan"
	allowed := []string{"https://ui.example.com"}
	if !IsOriginAllowed("https://ui.example.com", r, allowed) {
		t.Fatal("listed origin rejected")
	}
	if IsOriginAllowed("http://batter.lan", r, allowed) {
		t.Fatal("with an explicit list, unlisted same-origin should be rejected")
	}
}
