package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/device"
	"github.com/gin-gonic/gin"
	ws "github.com/gorilla/websocket"
)

// fakePhoneSession runs a real device manager against test/fakeadb (a
// simulated phone), starts a session on it, and serves h's WebSocket
// handlers from a real HTTP server.
func fakePhoneSession(t *testing.T, tune func(h *DeviceWSHandler)) (*device.Session, string) {
	t.Helper()
	bin := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, "adb"), "../../../test/fakeadb").CombinedOutput(); err != nil {
		t.Fatalf("build fakeadb: %v\n%s", err, out)
	}
	t.Setenv("FAKEADB_DIR", t.TempDir())
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	scrcpy := filepath.Join(t.TempDir(), "scrcpy-server")
	if err := os.WriteFile(scrcpy, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	dm, err := device.NewManager(device.ManagerConfig{ScrcpyServerPath: scrcpy, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dm.Shutdown)
	session, err := dm.StartSession(context.Background(), "FAKE01", device.SessionOptions{})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	h := NewDeviceWSHandler(dm, logger, nil)
	if tune != nil {
		tune(h)
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/ws/device/:serial/video", h.VideoStream)
	r.GET("/ws/device/:serial/control", h.ControlStream)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return session, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/device/FAKE01/"
}

func dial(t *testing.T, url string) *ws.Conn {
	t.Helper()
	conn, _, err := ws.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func waitSubscribers(t *testing.T, s *device.Session, want int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for s.VideoSubscribers() != want {
		if time.Now().After(deadline) {
			t.Fatalf("%d video subscribers after %v, want %d", s.VideoSubscribers(), within, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A viewer closing its tab on a still screen (no frames coming) must not
// keep the handler, its subscription and frame buffer around until the
// next frame.
func TestVideoHandlerExitsWhenClientLeavesStillScreen(t *testing.T) {
	session, base := fakePhoneSession(t, nil)
	conn := dial(t, base+"video")
	waitSubscribers(t, session, 1, 2*time.Second)
	_, _, _ = conn.ReadMessage() // the join keyframe; then the screen is still
	conn.Close()
	waitSubscribers(t, session, 0, time.Second)
}

// A client that vanishes without closing (laptop leaves Wi-Fi) stops
// answering pings; the handler must notice and unsubscribe.
func TestVideoHandlerDropsClientThatStopsAnsweringPings(t *testing.T) {
	session, base := fakePhoneSession(t, func(h *DeviceWSHandler) {
		h.pingPeriod, h.pongWait = 50*time.Millisecond, 300*time.Millisecond
	})
	conn := dial(t, base+"video") // never reads, so never answers a ping
	defer conn.Close()
	waitSubscribers(t, session, 1, 2*time.Second)
	waitSubscribers(t, session, 0, 2*time.Second)
}

// A live client answers pings and keeps its stream past the pong wait.
func TestVideoHandlerKeepsClientThatAnswersPings(t *testing.T) {
	session, base := fakePhoneSession(t, func(h *DeviceWSHandler) {
		h.pingPeriod, h.pongWait = 50*time.Millisecond, 300*time.Millisecond
	})
	conn := dial(t, base+"video")
	defer conn.Close()
	go func() { // reading answers pings
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	time.Sleep(800 * time.Millisecond)
	if n := session.VideoSubscribers(); n != 1 {
		t.Fatalf("live client dropped: %d subscribers", n)
	}
}

func TestControlHandlerDropsClientThatStopsAnsweringPings(t *testing.T) {
	session, base := fakePhoneSession(t, func(h *DeviceWSHandler) {
		h.pingPeriod, h.pongWait = 50*time.Millisecond, 300*time.Millisecond
	})
	conn := dial(t, base+"control")
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for !session.ClaimControl("probe") {
		if time.Now().After(deadline) {
			t.Fatal("silent control client still holds control")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOversizedControlMessageClosesConnection(t *testing.T) {
	_, base := fakePhoneSession(t, nil)
	conn := dial(t, base+"control")
	defer conn.Close()
	huge := `{"type":"text","text":"` + strings.Repeat("a", maxControlMessage+1) + `"}`
	if err := conn.WriteMessage(ws.TextMessage, []byte(huge)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	if !ws.IsCloseError(err, ws.CloseMessageTooBig) {
		t.Fatalf("read after oversized message: %v, want close 1009", err)
	}
}

// Pasting a large clipboard is the biggest legitimate control message; it
// must still get through.
func TestLargeClipboardStillAccepted(t *testing.T) {
	_, base := fakePhoneSession(t, nil)
	conn := dial(t, base+"control")
	defer conn.Close()
	paste := `{"type":"set_clipboard","paste":true,"text":"` + strings.Repeat("é", 120_000) + `"}`
	if err := conn.WriteMessage(ws.TextMessage, []byte(paste)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, _, err := conn.ReadMessage()
	if ne, ok := err.(interface{ Timeout() bool }); !ok || !ne.Timeout() {
		t.Fatalf("connection not kept open after a large paste: %v", err)
	}
}
