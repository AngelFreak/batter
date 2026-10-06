package handlers

import (
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/device"
	"github.com/XpertaDK/batter/internal/device/scrcpytest"
	ws "github.com/gorilla/websocket"
)

// After the phone rotates, a touch must carry the rotated size: scrcpy-server
// ignores touches whose size differs from the current video size, so a stale
// one makes input silently dead.
func TestTouchAfterRotationCarriesNewSize(t *testing.T) {
	dev, dm, url := startSessionTestServer(t, "control", (*DeviceWSHandler).ControlStream)
	conn, _, err := ws.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	touch := func() (w, h uint16) {
		t.Helper()
		if err := conn.WriteJSON(ControlMessage{Type: "touch", Action: device.ActionDown, X: 0.5, Y: 0.5}); err != nil {
			t.Fatal(err)
		}
		msg := make([]byte, 32)
		_ = dev.Control.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.ReadFull(dev.Control, msg); err != nil {
			t.Fatalf("device got no touch: %v", err)
		}
		if msg[0] != device.ControlTypeTouch {
			t.Fatalf("device got control type %d, want touch", msg[0])
		}
		return binary.BigEndian.Uint16(msg[18:20]), binary.BigEndian.Uint16(msg[20:22])
	}

	if w, h := touch(); w != scrcpytest.Width || h != scrcpytest.Height {
		t.Fatalf("touch before rotation sized %dx%d, want %dx%d", w, h, scrcpytest.Width, scrcpytest.Height)
	}

	dev.WriteSession(t, scrcpytest.Height, scrcpytest.Width)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if w, _ := dm.GetSession("FAKE").Size(); w == scrcpytest.Height {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session never saw the rotation")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if w, h := touch(); w != scrcpytest.Height || h != scrcpytest.Width {
		t.Fatalf("touch after rotation sized %dx%d, want %dx%d", w, h, scrcpytest.Height, scrcpytest.Width)
	}
}
