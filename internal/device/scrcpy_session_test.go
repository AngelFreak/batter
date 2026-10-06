package device

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/device/scrcpytest"
)

// A session packet mid-stream (the phone rotated) updates the size and is
// consumed: viewers get only media packets, and the stream stays in step.
func TestRotationUpdatesSizeWithoutReachingViewers(t *testing.T) {
	s, dev, _ := startFakeSession(t, scrcpytest.NewADB(t), fullOpts)
	ch := s.SubscribeVideo("viewer")
	defer s.UnsubscribeVideo("viewer")

	dev.WriteSession(t, scrcpytest.Height, scrcpytest.Width)
	scrcpytest.WritePacket(t, dev.Video, scrcpytest.FlagConfig, []byte{0, 0, 0, 1, 0x67})
	scrcpytest.WritePacket(t, dev.Video, 1000|scrcpytest.FlagKeyFrame, []byte{0, 0, 0, 1, 0x65})

	cfg := recv(t, ch, "config")
	if len(cfg) != 12+5 || binary.BigEndian.Uint64(cfg)&scrcpytest.FlagSession != 0 {
		t.Fatalf("first packet to viewer = %x, want the config packet", cfg)
	}
	if got := s.GetConfigPacket(); len(got) != len(cfg) {
		t.Fatalf("stored config = %x, want %x", got, cfg)
	}
	key := recv(t, ch, "keyframe")
	if len(key) != 12+5 || key[16] != 0x65 {
		t.Fatalf("second packet to viewer = %x, want the keyframe", key)
	}
	if w, h := s.Size(); w != scrcpytest.Height || h != scrcpytest.Width {
		t.Fatalf("size after rotation = %dx%d, want %dx%d", w, h, scrcpytest.Height, scrcpytest.Width)
	}
	if !s.IsAlive() {
		t.Fatal("session died on a session packet")
	}
}

// A server speaking the pre-4.0 framing (width and height after the codec,
// no session packet) must fail the session loudly, not be misparsed.
func TestHandshakeRejectsOldFraming(t *testing.T) {
	fake := scrcpytest.NewADB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adb := &ADB{adbPath: fake.Path, logger: logger}
	errc := make(chan error, 1)
	go func() {
		s, err := newSession(adb, "FAKE", filepath.Join(fake.Dir, "scrcpy-server"), ServerVersion, TierOptions(TierThumbnail), logger)
		if s != nil {
			s.Close()
		}
		errc <- err
	}()
	_, port := fake.WaitLaunch(t)
	video, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer video.Close()
	// scrcpy 3.x: name, codec, width, height, then the first media packet.
	meta := make([]byte, 64+12+12)
	copy(meta, "Old Phone")
	binary.BigEndian.PutUint32(meta[64:], scrcpytest.CodecH264)
	binary.BigEndian.PutUint32(meta[68:], 1080)
	binary.BigEndian.PutUint32(meta[72:], 2400)
	binary.BigEndian.PutUint64(meta[76:], 1<<63) // a 3.x config packet
	binary.BigEndian.PutUint32(meta[84:], 6)
	if _, err := video.Write(meta); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "unexpected first video packet") {
			t.Fatalf("newSession error = %v, want unexpected first video packet", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("newSession did not return")
	}
}

// The audio stream never carries session packets; one means the server and
// this client disagree on the protocol, so the audio stream ends.
func TestAudioEndsOnSessionPacket(t *testing.T) {
	s, dev, _ := startFakeSession(t, scrcpytest.NewADB(t), fullOpts)
	dev.WriteAudioHeader(t, scrcpytest.CodecOpus)
	waitAudio(t, s)
	ch := s.SubscribeAudio("listener")
	// Its last word reads as a plausible packet size, so only the session
	// flag check keeps it (and the next 3 bytes) from playing as audio.
	scrcpytest.WritePacket(t, dev.Audio, scrcpytest.FlagSession, []byte{0xfc, 0xff, 0xfe})
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("session packet was delivered as audio")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("audio stream did not end")
	}
	assertVideoFlows(t, s, dev)
}
