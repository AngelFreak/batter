package device

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/XpertaDK/batter/internal/device/scrcpytest"
)

var fullOpts = TierOptions(TierFull)

// startFakeSession runs the real newSession against a fake adb and a fake
// device that connects back over loopback like scrcpy-server.
func startFakeSession(t *testing.T, fake *scrcpytest.ADB, opts SessionOptions) (*Session, *scrcpytest.Device, []string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adb := &ADB{adbPath: fake.Path, logger: logger}

	type result struct {
		s   *Session
		err error
	}
	res := make(chan result, 1)
	go func() {
		s, err := newSession(adb, "FAKE", filepath.Join(fake.Dir, "scrcpy-server"), "3.3.4", opts, logger)
		res <- result{s, err}
	}()
	args, port := fake.WaitLaunch(t)
	dev := scrcpytest.Connect(t, port, scrcpytest.HasArg(args, "audio=true"))
	select {
	case r := <-res:
		if r.err != nil {
			t.Fatalf("newSession: %v", r.err)
		}
		t.Cleanup(r.s.Close)
		return r.s, dev, args
	case <-time.After(10 * time.Second):
		t.Fatal("newSession did not return")
		return nil, nil, nil
	}
}

func recv(t *testing.T, ch <-chan []byte, what string) []byte {
	t.Helper()
	select {
	case msg, ok := <-ch:
		if !ok {
			t.Fatalf("%s: channel closed", what)
		}
		return msg
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: nothing received", what)
		return nil
	}
}

func waitAudio(t *testing.T, s *Session) AudioStatus {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.WaitAudio(ctx)
}

// assertVideoFlows proves the video path still works by pushing a frame
// through the device's video socket to a subscriber.
func assertVideoFlows(t *testing.T, s *Session, dev *scrcpytest.Device) {
	t.Helper()
	ch := s.SubscribeVideo("video-check")
	defer s.UnsubscribeVideo("video-check")
	scrcpytest.WritePacket(t, dev.Video, 1000|1<<62, []byte{0, 0, 0, 1, 0x65}) // an IDR keyframe
	if got := recv(t, ch, "video"); len(got) != 12+5 {
		t.Fatalf("video packet = %d bytes, want 17", len(got))
	}
	if !s.IsAlive() {
		t.Fatal("session reported dead")
	}
}

func TestServerArgsEnableOpusOutputAudioForFullSessionsOnly(t *testing.T) {
	full := buildServerArgs(1, "3.3.4", fullOpts)
	for _, want := range []string{"audio=true", "audio_codec=opus", "audio_source=output"} {
		if !scrcpytest.HasArg(full, want) {
			t.Errorf("full session args %v missing %s", full, want)
		}
	}
	// audio_dup would keep the phone playing; the phone must stay silent.
	if scrcpytest.HasArg(full, "audio_dup=true") {
		t.Errorf("full session args %v enable audio_dup", full)
	}
	// Capturing "output" silences the phone, so grid thumbnails (nobody
	// listening) must not capture.
	if thumb := buildServerArgs(1, "3.3.4", TierOptions(TierThumbnail)); !scrcpytest.HasArg(thumb, "audio=false") {
		t.Errorf("thumbnail args %v do not disable audio", thumb)
	}
}

func TestSessionStreamsOpusAudioAlongsideVideo(t *testing.T) {
	s, dev, _ := startFakeSession(t, scrcpytest.NewADB(t), fullOpts)
	if s.DeviceName != "Fake Phone" || s.Width != 1080 || s.Height != 2400 {
		t.Fatalf("video handshake = %q %dx%d", s.DeviceName, s.Width, s.Height)
	}

	dev.WriteAudioHeader(t, scrcpytest.CodecOpus)
	if st := waitAudio(t, s); !st.Available || st.Codec != "opus" {
		t.Fatalf("audio status = %+v, want available opus", st)
	}

	ch := s.SubscribeAudio("a")
	defer s.UnsubscribeAudio("a")
	opusHead := []byte("OpusHead\x01\x02\x38\x01\x80\xbb\x00\x00\x00\x00\x00")
	scrcpytest.WritePacket(t, dev.Audio, scrcpytest.FlagConfig, opusHead)
	if got := recv(t, ch, "audio config"); string(got[12:]) != string(opusHead) {
		t.Fatalf("config payload = %q", got[12:])
	}
	scrcpytest.WritePacket(t, dev.Audio, 20000, []byte{0xfc, 0xff, 0xfe})
	got := recv(t, ch, "audio packet")
	if len(got) != 15 || got[7] != 0x20 || got[11] != 3 {
		t.Fatalf("audio packet = %x, want 12-byte header (pts 20000, size 3) + payload", got)
	}

	// A listener joining later still gets the decoder config.
	if cfg := s.GetAudioConfigPacket(); cfg == nil || string(cfg[12:]) != string(opusHead) {
		t.Fatalf("cached audio config = %x", cfg)
	}
	assertVideoFlows(t, s, dev)
}

func TestSessionKeepsVideoWhenDeviceCannotCaptureAudio(t *testing.T) {
	for name, code := range map[string]uint32{
		"disabled (Android < 11, capture refused)": scrcpytest.AudioDisabled,
		"configuration error":                      scrcpytest.AudioConfigError,
	} {
		t.Run(name, func(t *testing.T) {
			s, dev, _ := startFakeSession(t, scrcpytest.NewADB(t), fullOpts)
			dev.WriteAudioHeader(t, code)
			if st := waitAudio(t, s); st.Available || st.Reason == "" {
				t.Fatalf("audio status = %+v, want unavailable with a reason", st)
			}
			assertVideoFlows(t, s, dev)
		})
	}
}

func TestSessionDoesNotWaitForTheAudioHeader(t *testing.T) {
	// The device writes the audio header only once its encoder runs; video
	// must be up before that, and audio is "not ready" until then.
	s, dev, _ := startFakeSession(t, scrcpytest.NewADB(t), fullOpts)
	assertVideoFlows(t, s, dev)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if st := s.WaitAudio(ctx); st.Available {
		t.Fatalf("audio available before its header: %+v", st)
	}
}

func TestSessionKeepsVideoWhenAudioStreamEnds(t *testing.T) {
	s, dev, _ := startFakeSession(t, scrcpytest.NewADB(t), fullOpts)
	dev.WriteAudioHeader(t, scrcpytest.CodecOpus)
	waitAudio(t, s)
	ch := s.SubscribeAudio("a")

	_ = dev.Audio.Close()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("got audio after the stream closed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("audio subscriber not released when the stream ended")
	}
	if st := waitAudio(t, s); st.Available {
		t.Fatalf("audio status after stream end = %+v", st)
	}
	s.UnsubscribeAudio("a") // must not double-close
	assertVideoFlows(t, s, dev)
}

func TestThumbnailSessionHasNoAudioSocket(t *testing.T) {
	s, dev, args := startFakeSession(t, scrcpytest.NewADB(t), TierOptions(TierThumbnail))
	if dev.Audio != nil {
		t.Fatalf("audio socket dialed with args %v", args)
	}
	if st := waitAudio(t, s); st.Available {
		t.Fatalf("thumbnail audio status = %+v", st)
	}
	assertVideoFlows(t, s, dev)
}

func TestManagerDropsAudioAfterItTookDownTheServer(t *testing.T) {
	// scrcpy-server exits on a fatal audio error after telling us audio is
	// disabled. Retrying with audio would fail the same way forever, so the
	// next session for that device must be video-only.
	fake := scrcpytest.NewADB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := &Manager{
		adb:              &ADB{adbPath: fake.Path, logger: logger},
		sessions:         make(map[string]*Session),
		sessionTiers:     make(map[string]SessionTier),
		fullViewers:      make(map[string]int),
		deviceLocks:      make(map[string]*sync.Mutex),
		scrcpyServerPath: filepath.Join(fake.Dir, "scrcpy-server"),
		scrcpyVersion:    "3.3.4",
		logger:           logger,
	}
	t.Cleanup(m.Shutdown)

	start := func() (*scrcpytest.Device, []string) {
		res := make(chan error, 1)
		go func() {
			_, err := m.StartSession(context.Background(), "FAKE", fullOpts)
			res <- err
		}()
		args, port := fake.WaitLaunch(t)
		dev := scrcpytest.Connect(t, port, scrcpytest.HasArg(args, "audio=true"))
		if err := <-res; err != nil {
			t.Fatal(err)
		}
		return dev, args
	}

	dev, args := start()
	if !scrcpytest.HasArg(args, "audio=true") {
		t.Fatalf("first session args %v lack audio", args)
	}
	dev.WriteAudioHeader(t, scrcpytest.AudioDisabled)
	waitAudio(t, m.GetSession("FAKE"))
	dev.Close() // the server exits
	deadline := time.Now().Add(5 * time.Second)
	for m.GetSession("FAKE").IsAlive() {
		if time.Now().After(deadline) {
			t.Fatal("session still alive after the server exited")
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.cleanupDeadSessions()

	_, args = start()
	if !scrcpytest.HasArg(args, "audio=false") {
		t.Fatalf("session after fatal audio error has args %v, want audio=false", args)
	}
}
