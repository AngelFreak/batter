package device

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEncodeTouchEvent(t *testing.T) {
	buf := EncodeTouchEvent(ActionDown, 42, 0.5, 0.3, 1080, 1920, 0xFFFF)

	assert.Equal(t, byte(ControlTypeTouch), buf[0])
	assert.Equal(t, byte(ActionDown), buf[1])
	assert.Equal(t, uint64(42), binary.BigEndian.Uint64(buf[2:10]))
	// x,y are stored as absolute pixel coords: uint32(0.5*1080)=540, uint32(0.3*1920)=576
	assert.Equal(t, uint32(540), binary.BigEndian.Uint32(buf[10:14]))
	assert.Equal(t, uint32(576), binary.BigEndian.Uint32(buf[14:18]))
	assert.Equal(t, uint16(1080), binary.BigEndian.Uint16(buf[18:20]))
	assert.Equal(t, uint16(1920), binary.BigEndian.Uint16(buf[20:22]))
	assert.Equal(t, uint16(0xFFFF), binary.BigEndian.Uint16(buf[22:24]))
	assert.Len(t, buf, 32)
}

func TestEncodeKeyEvent(t *testing.T) {
	buf := EncodeKeyEvent(ActionDown, 66, 0, 0) // KEYCODE_ENTER

	assert.Equal(t, byte(ControlTypeKeycode), buf[0])
	assert.Equal(t, byte(ActionDown), buf[1])
	assert.Equal(t, uint32(66), binary.BigEndian.Uint32(buf[2:6]))
	assert.Equal(t, uint32(0), binary.BigEndian.Uint32(buf[6:10]))
	assert.Equal(t, uint32(0), binary.BigEndian.Uint32(buf[10:14]))
	assert.Len(t, buf, 14)
}

func TestEncodeTextEvent(t *testing.T) {
	text := "hello"
	buf := EncodeTextEvent(text)

	assert.Equal(t, byte(ControlTypeText), buf[0])
	assert.Equal(t, uint32(5), binary.BigEndian.Uint32(buf[1:5]))
	assert.Equal(t, text, string(buf[5:]))
	assert.Len(t, buf, 10)
}

func TestEncodeTextEventUnicode(t *testing.T) {
	text := "hello world"
	buf := EncodeTextEvent(text)

	assert.Equal(t, byte(ControlTypeText), buf[0])
	textLen := binary.BigEndian.Uint32(buf[1:5])
	assert.Equal(t, text, string(buf[5:5+textLen]))
}

func TestEncodeScrollEvent(t *testing.T) {
	buf := EncodeScrollEvent(0.5, 0.5, 1080, 1920, 0, -1)

	assert.Equal(t, byte(ControlTypeScroll), buf[0])
	// x,y are stored as absolute pixel coords: uint32(0.5*1080)=540, uint32(0.5*1920)=960
	assert.Equal(t, uint32(540), binary.BigEndian.Uint32(buf[1:5]))
	assert.Equal(t, uint32(960), binary.BigEndian.Uint32(buf[5:9]))
	assert.Equal(t, uint16(1080), binary.BigEndian.Uint16(buf[9:11]))
	assert.Equal(t, uint16(1920), binary.BigEndian.Uint16(buf[11:13]))
	assert.Len(t, buf, 21)
}

func TestEncodeBackOrScreenOn(t *testing.T) {
	buf := EncodeBackOrScreenOn(ActionDown)

	assert.Equal(t, byte(ControlTypeBackOrScreenOn), buf[0])
	assert.Equal(t, byte(ActionDown), buf[1])
	assert.Len(t, buf, 2)
}

func TestEncodeSetClipboard(t *testing.T) {
	text := "clip"
	buf := EncodeSetClipboard(7, text, true)

	assert.Equal(t, byte(ControlTypeSetClipboard), buf[0])
	assert.Equal(t, uint64(7), binary.BigEndian.Uint64(buf[1:9]))
	assert.Equal(t, byte(1), buf[9]) // paste = true
	assert.Equal(t, uint32(len(text)), binary.BigEndian.Uint32(buf[10:14]))
	assert.Equal(t, text, string(buf[14:]))
	assert.Len(t, buf, 14+len(text))
}

func TestEncodeSetClipboardNoPaste(t *testing.T) {
	buf := EncodeSetClipboard(0, "", false)

	assert.Equal(t, byte(ControlTypeSetClipboard), buf[0])
	assert.Equal(t, byte(0), buf[9]) // paste = false
	assert.Equal(t, uint32(0), binary.BigEndian.Uint32(buf[10:14]))
	assert.Len(t, buf, 14)
}

func TestEncodeGetClipboard(t *testing.T) {
	buf := EncodeGetClipboard(CopyKeyCopy)

	assert.Equal(t, byte(ControlTypeGetClipboard), buf[0])
	assert.Equal(t, byte(CopyKeyCopy), buf[1])
	assert.Len(t, buf, 2)
}

func TestEncodeResetVideo(t *testing.T) {
	// scrcpy 3.x TYPE_RESET_VIDEO = 17, no payload: restarts the encoder,
	// which emits a fresh config packet and keyframe.
	if got := EncodeResetVideo(); len(got) != 1 || got[0] != 17 {
		t.Fatalf("EncodeResetVideo() = %v, want [17]", got)
	}
}

func TestRequestKeyframeSendsResetVideoOnControlSocket(t *testing.T) {
	device, server := net.Pipe()
	defer device.Close()
	s := &Session{controlConn: server}

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 8)
		n, _ := device.Read(buf)
		got <- buf[:n]
	}()
	if err := s.RequestKeyframe(); err != nil {
		t.Fatal(err)
	}
	if b := <-got; len(b) != 1 || b[0] != 17 {
		t.Fatalf("control socket received %v, want [17]", b)
	}
}

// resetCounter reads the device side of a control pipe and timestamps every
// RESET_VIDEO it receives.
func resetCounter(t *testing.T, device net.Conn) func() []time.Time {
	t.Helper()
	var mu sync.Mutex
	var resets []time.Time
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := device.Read(buf)
			if err != nil {
				return
			}
			mu.Lock()
			for _, b := range buf[:n] {
				if b == 17 {
					resets = append(resets, time.Now())
				}
			}
			mu.Unlock()
		}
	}()
	return func() []time.Time {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(resets)
	}
}

func keyframeSession(t *testing.T, every time.Duration) (*Session, func() []time.Time) {
	t.Helper()
	device, server := net.Pipe()
	t.Cleanup(func() { device.Close(); server.Close() })
	s := &Session{controlConn: server, keyframeEvery: every, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return s, resetCounter(t, device)
}

func waitResets(t *testing.T, resets func() []time.Time, n int) []time.Time {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(resets()) < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return resets()
}

// Several viewers or thumbnails joining together cause one encoder reset
// now and at most one more when the window ends, not one each.
func TestKeyframeRequestsAreCoalesced(t *testing.T) {
	s, resets := keyframeSession(t, 200*time.Millisecond)
	for range 10 {
		if err := s.RequestKeyframe(); err != nil {
			t.Fatal(err)
		}
	}
	got := waitResets(t, resets, 2)
	time.Sleep(300 * time.Millisecond)
	if got = resets(); len(got) != 2 {
		t.Fatalf("%d resets for 10 joins, want 2 (leading + trailing)", len(got))
	}
	if gap := got[1].Sub(got[0]); gap < 190*time.Millisecond {
		t.Fatalf("resets %v apart, want at least the 200ms window", gap)
	}
}

// A viewer joining just after a reset may have missed that reset's
// keyframe, so it must get another one sent after it joined.
func TestLateJoinerStillGetsAKeyframe(t *testing.T) {
	s, resets := keyframeSession(t, 200*time.Millisecond)
	_ = s.RequestKeyframe()
	waitResets(t, resets, 1)
	time.Sleep(50 * time.Millisecond)
	joined := time.Now()
	_ = s.RequestKeyframe()
	got := waitResets(t, resets, 2)
	if len(got) != 2 || got[1].Before(joined) {
		t.Fatalf("no reset after the late joiner's request: %v (joined %v)", got, joined)
	}
}

func TestKeyframeAfterQuietPeriodIsImmediate(t *testing.T) {
	s, resets := keyframeSession(t, 100*time.Millisecond)
	_ = s.RequestKeyframe()
	waitResets(t, resets, 1)
	time.Sleep(150 * time.Millisecond)
	asked := time.Now()
	_ = s.RequestKeyframe()
	got := waitResets(t, resets, 2)
	if len(got) != 2 || got[1].Sub(asked) > 50*time.Millisecond {
		t.Fatalf("reset after a quiet period was delayed: %v", got)
	}
}
