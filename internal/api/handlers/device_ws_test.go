package handlers

import (
	"net/http/httptest"
	"testing"
)

// The upgrader is what actually guards the video/control sockets, so test its
// CheckOrigin rather than only the shared helper.
func TestWebSocketUpgraderAcceptsSameOriginWithoutConfig(t *testing.T) {
	h := NewDeviceWSHandler(nil, nil, nil)

	same := httptest.NewRequest("GET", "/ws/device/x/control", nil)
	same.Host = "localhost:8080"
	same.Header.Set("X-Forwarded-Host", "192.168.1.50:3000")
	same.Header.Set("Origin", "http://192.168.1.50:3000")
	if !h.upgrader.CheckOrigin(same) {
		t.Fatal("same-origin WebSocket rejected with no ALLOWED_ORIGINS set")
	}

	cross := httptest.NewRequest("GET", "/ws/device/x/control", nil)
	cross.Host = "localhost:8080"
	cross.Header.Set("X-Forwarded-Host", "192.168.1.50:3000")
	cross.Header.Set("Origin", "https://evil.example")
	if h.upgrader.CheckOrigin(cross) {
		t.Fatal("cross-site WebSocket accepted")
	}
}
