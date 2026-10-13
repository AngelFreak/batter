package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/device"
	"github.com/XpertaDK/batter/internal/device/scrcpytest"
	"github.com/gin-gonic/gin"
	ws "github.com/gorilla/websocket"
)

// startAudioTestServer runs a real device manager (with a fake adb on PATH)
// and the audio WebSocket route, with a session whose fake device has
// connected. It returns the device end and the WebSocket URL.
func startAudioTestServer(t *testing.T) (*scrcpytest.Device, string) {
	t.Helper()
	dev, _, url := startSessionTestServer(t, "audio", (*DeviceWSHandler).AudioStream)
	return dev, url
}

// startSessionTestServer runs a real device manager (with a fake adb on
// PATH) and one /ws/device/:serial/<kind> route, with a full-tier session
// whose fake device has connected. It returns the device end, the manager
// and the WebSocket URL.
func startSessionTestServer(t *testing.T, kind string, route func(*DeviceWSHandler, *gin.Context)) (*scrcpytest.Device, *device.Manager, string) {
	t.Helper()
	fake := scrcpytest.NewADB(t)
	t.Setenv("PATH", fake.Dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	server := filepath.Join(fake.Dir, "scrcpy-server")
	if err := os.WriteFile(server, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dm, err := device.NewManager(device.ManagerConfig{ScrcpyServerPath: server, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dm.Shutdown)

	started := make(chan error, 1)
	go func() {
		_, err := dm.StartSession(context.Background(), "FAKE", device.TierOptions(device.TierFull))
		started <- err
	}()
	args, port := fake.WaitLaunch(t)
	dev := scrcpytest.Connect(t, port, scrcpytest.HasArg(args, "audio=true"))
	if err := <-started; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewDeviceWSHandler(dm, logger, nil)
	r.GET("/ws/device/:serial/"+kind, func(c *gin.Context) { route(h, c) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return dev, dm, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/device/FAKE/" + kind
}

func dialAudio(t *testing.T, url string) *ws.Conn {
	t.Helper()
	conn, _, err := ws.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	return conn
}

func readStatus(t *testing.T, conn *ws.Conn) device.AudioStatus {
	t.Helper()
	kind, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	var st device.AudioStatus
	if kind != ws.TextMessage || json.Unmarshal(data, &st) != nil {
		t.Fatalf("first message = %d %q, want a JSON status", kind, data)
	}
	return st
}

func TestAudioStreamSendsStatusConfigThenPackets(t *testing.T) {
	dev, url := startAudioTestServer(t)
	dev.WriteAudioHeader(t, scrcpytest.CodecOpus)
	opusHead := []byte("OpusHead\x01\x02\x38\x01\x80\xbb\x00\x00\x00\x00\x00")
	scrcpytest.WritePacket(t, dev.Audio, scrcpytest.FlagConfig, opusHead)

	conn := dialAudio(t, url)
	if st := readStatus(t, conn); !st.Available || st.Codec != "opus" {
		t.Fatalf("status = %+v, want available opus", st)
	}
	// The config packet was sent before this listener joined; it must still
	// come first so the browser can configure its decoder.
	_, cfg, err := conn.ReadMessage()
	if err != nil || len(cfg) != 12+len(opusHead) || cfg[0] != 0x40 { // config flag, bit 62
		t.Fatalf("config message = %x, %v", cfg, err)
	}

	// The handler subscribes after sending the cached config; keep sending
	// until a media packet arrives rather than racing that.
	payload := []byte{0xfc, 0xff, 0xfe}
	got := make(chan []byte, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				close(got)
				return
			}
			if msg[0]&0x40 == 0 {
				got <- msg
				return
			}
		}
	}()
	for i := uint64(1); ; i++ {
		scrcpytest.WritePacket(t, dev.Audio, i*20000, payload)
		select {
		case msg, ok := <-got:
			if !ok || len(msg) != 12+len(payload) || string(msg[12:]) != string(payload) {
				t.Fatalf("audio message = %x", msg)
			}
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestAudioStreamReportsUnavailableAndCloses(t *testing.T) {
	dev, url := startAudioTestServer(t)
	dev.WriteAudioHeader(t, scrcpytest.AudioDisabled)

	conn := dialAudio(t, url)
	if st := readStatus(t, conn); st.Available || st.Reason == "" {
		t.Fatalf("status = %+v, want unavailable with a reason", st)
	}
	if _, _, err := conn.ReadMessage(); !ws.IsCloseError(err, ws.CloseNormalClosure) {
		t.Fatalf("after unavailable status: %v, want a normal close", err)
	}
}

func TestAudioStreamWithoutSessionIs404(t *testing.T) {
	_, url := startAudioTestServer(t)
	_, resp, err := ws.DefaultDialer.Dial(strings.Replace(url, "/FAKE/", "/OTHER/", 1), nil)
	if err == nil || resp == nil || resp.StatusCode != 404 {
		t.Fatalf("dial without session: %v %v, want 404", resp, err)
	}
}
