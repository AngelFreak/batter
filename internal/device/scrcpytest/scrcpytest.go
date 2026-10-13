// Package scrcpytest fakes the device side of a scrcpy session for tests: an
// adb that accepts every command and records the scrcpy-server launch, and a
// device that connects back to the session's listener over loopback in the
// same socket order, with the same handshake, as scrcpy-server 5.0.
package scrcpytest

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Codec IDs scrcpy-server writes as the first 4 bytes of a stream.
const (
	CodecH264 uint32 = 0x68323634 // "h264"
	CodecOpus uint32 = 0x6f707573 // "opus"

	// Written instead of a codec ID when the device disables the audio
	// stream: 0 = audio unavailable (keep mirroring video), 1 = a
	// configuration error (the real client stops).
	AudioDisabled    uint32 = 0
	AudioConfigError uint32 = 1
)

// Flags in the 12-byte packet header (scrcpy 4.0+). Deliberately not taken
// from package device, so a wrong constant there fails the tests.
const (
	FlagSession  uint64 = 1 << 63 // video session packet: size, no payload
	FlagConfig   uint64 = 1 << 62 // codec config packet
	FlagKeyFrame uint64 = 1 << 61 // keyframe
)

// Size of the first session packet Connect sends.
const (
	Width  = 1080
	Height = 2400
)

// ADB is a fake adb executable. reverse records the listener port, a shell
// running app_process records the server arguments and then blocks like the
// real server would; every other command succeeds silently.
type ADB struct {
	Dir  string // contains the "adb" script, for prepending to PATH
	Path string
}

// NewADB writes the fake adb script into a temporary directory.
func NewADB(t testing.TB) *ADB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "adb")
	script := `#!/bin/sh
d='` + dir + `'
if [ "$1" = -s ]; then shift 2; fi
case "$1" in
reverse)
  if [ "$2" != --remove ] && [ "$2" != --list ]; then
    echo "${3#tcp:}" > "$d/port.tmp" && mv "$d/port.tmp" "$d/port"
  fi ;;
shell)
  case "$*" in
  *app_process*)
    shift
    echo "$*" > "$d/args.tmp" && mv "$d/args.tmp" "$d/args"
    exec sleep 60 ;;
  esac ;;
esac
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ADB{Dir: dir, Path: path}
}

// WaitLaunch waits for a session to launch scrcpy-server and returns the
// server arguments and the local port it must connect to. Each launch is
// consumed, so a later call waits for the next session.
func (a *ADB) WaitLaunch(t testing.TB) (args []string, port int) {
	t.Helper()
	argsPath := filepath.Join(a.Dir, "args")
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(argsPath)
		if err == nil {
			_ = os.Remove(argsPath)
			p, err := os.ReadFile(filepath.Join(a.Dir, "port"))
			if err != nil {
				t.Fatalf("server launched before adb reverse: %v", err)
			}
			port, err = strconv.Atoi(strings.TrimSpace(string(p)))
			if err != nil {
				t.Fatalf("bad port %q: %v", p, err)
			}
			return strings.Fields(string(raw)), port
		}
		if time.Now().After(deadline) {
			t.Fatal("scrcpy-server was never launched")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// HasArg reports whether the server was launched with arg.
func HasArg(args []string, arg string) bool {
	for _, a := range args {
		if a == arg {
			return true
		}
	}
	return false
}

// Device is the device end of a scrcpy session's sockets.
type Device struct {
	Video, Audio, Control net.Conn
}

// Connect dials the session like scrcpy-server does in reverse-tunnel mode:
// video, then audio (if enabled), then control, then the 64-byte device name
// on the first socket followed by the codec ID and a session packet
// (Width x Height).
// The audio header is left to the caller, since the real server only writes
// it once its encoder is running (or the disable code if it can't capture).
func Connect(t testing.TB, port int, withAudio bool) *Device {
	t.Helper()
	dial := func() net.Conn {
		c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			t.Fatalf("dial session: %v", err)
		}
		return c
	}
	d := &Device{Video: dial()}
	if withAudio {
		d.Audio = dial()
	}
	d.Control = dial()
	t.Cleanup(d.Close)

	meta := make([]byte, 64+4)
	copy(meta, "Fake Phone")
	binary.BigEndian.PutUint32(meta[64:], CodecH264)
	write(t, d.Video, meta)
	d.WriteSession(t, Width, Height)
	return d
}

// WriteSession writes a video session packet, as the server does before the
// first frame and whenever the capture size changes (rotation).
func (d *Device) WriteSession(t testing.TB, width, height uint32) {
	t.Helper()
	buf := binary.BigEndian.AppendUint32(nil, uint32(FlagSession>>32))
	buf = binary.BigEndian.AppendUint32(buf, width)
	buf = binary.BigEndian.AppendUint32(buf, height)
	write(t, d.Video, buf)
}

// WriteAudioHeader writes the audio stream's codec ID (or disable code).
func (d *Device) WriteAudioHeader(t testing.TB, codec uint32) {
	t.Helper()
	write(t, d.Audio, binary.BigEndian.AppendUint32(nil, codec))
}

// WritePacket writes one packet with scrcpy's 12-byte header: PTS and flags
// (big-endian uint64), then the payload size (big-endian uint32).
func WritePacket(t testing.TB, conn net.Conn, ptsAndFlags uint64, payload []byte) {
	t.Helper()
	buf := binary.BigEndian.AppendUint64(nil, ptsAndFlags)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(payload)))
	write(t, conn, append(buf, payload...))
}

// Close closes every socket, as when scrcpy-server exits.
func (d *Device) Close() {
	for _, c := range []net.Conn{d.Video, d.Audio, d.Control} {
		if c != nil {
			_ = c.Close()
		}
	}
}

func write(t testing.TB, c net.Conn, b []byte) {
	t.Helper()
	if _, err := c.Write(b); err != nil {
		t.Fatalf("write to session: %v", err)
	}
}
