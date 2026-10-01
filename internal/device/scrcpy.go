package device

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

// Device message types (sent by scrcpy-server to client via control connection).
const (
	DeviceMsgTypeClipboard    = 0
	DeviceMsgTypeACKClipboard = 1
)

// SessionOptions configures a scrcpy session.
type SessionOptions struct {
	MaxSize int `json:"max_size"` // 0 = device default
	MaxFPS  int `json:"max_fps"`  // 0 = no limit
	// DisableAudio forces a video-only session. Audio is otherwise captured
	// for full-tier sessions only: capturing the "output" source silences
	// the phone, which nobody should pay for a grid thumbnail.
	DisableAudio bool `json:"-"`
}

func (o SessionOptions) wantsAudio() bool {
	return !o.DisableAudio && tierFor(o) == TierFull
}

// What scrcpy-server writes in place of the audio codec ID when it disables
// the stream (Streamer.writeDisableStream), and the codec ID for Opus.
const (
	audioCodecDisabled    = 0
	audioCodecConfigError = 1
	audioCodecOpus        = 0x6f707573 // "opus"
)

// AudioStatus says whether a session can stream audio, and if not, why.
type AudioStatus struct {
	Available bool   `json:"available"`
	Codec     string `json:"codec,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Session represents an active scrcpy connection to a device.
type Session struct {
	Serial     string
	SCID       uint32
	DeviceName string
	Width      int
	Height     int

	videoConn   net.Conn
	controlConn net.Conn
	controlMu   sync.Mutex // serializes writes to controlConn
	videoPort   int

	// Video subscribers: id -> channel
	videoSubscribers map[string]chan []byte
	subscribersMu    sync.RWMutex

	// Control ownership: only one client can send input at a time
	controlOwner   string
	controlOwnerMu sync.Mutex

	// Clipboard from device
	clipboardText string
	clipboardMu   sync.RWMutex
	clipboardCh   chan string

	// Stored SPS/PPS for new subscribers
	configPacket []byte
	configMu     sync.RWMutex

	// Audio is best effort: none of it may affect video, control or IsAlive.
	audioConn        net.Conn
	audioMu          sync.RWMutex // guards the audio fields below
	audioSubscribers map[string]chan []byte
	audioConfig      []byte        // last Opus config packet (header + OpusHead)
	audioStatus      AudioStatus   // valid once audioReady is closed
	audioReady       chan struct{} // closed once the device says whether audio works
	audioEnded       bool          // stream over; no more packets will come
	audioDisabledAt  time.Time     // when the device disabled audio, if it did
	audioDone        chan struct{} // closed when audioReadLoop returns
	videoEndedAt     time.Time     // written before done is closed

	cancel context.CancelFunc
	done   chan struct{}
	logger *slog.Logger
}

// newSession launches scrcpy-server on the device and establishes connections.
func newSession(adb *ADB, serial, scrcpyServerPath, scrcpyVersion string, opts SessionOptions, logger *slog.Logger) (*Session, error) {
	ctx, cancel := context.WithCancel(context.Background())

	scid := rand.Uint32() & 0x7FFFFFFF // Must fit in Java signed int
	logger = logger.With("serial", serial, "scid", fmt.Sprintf("%08x", scid))

	s := &Session{
		Serial:           serial,
		SCID:             scid,
		videoSubscribers: make(map[string]chan []byte),
		audioSubscribers: make(map[string]chan []byte),
		audioReady:       make(chan struct{}),
		audioDone:        make(chan struct{}),
		clipboardCh:      make(chan string, 4),
		cancel:           cancel,
		done:             make(chan struct{}),
		logger:           logger,
	}

	// Step 0: Wake screen before starting (scrcpy captures a black surface if screen is off)
	_, _ = adb.Shell(ctx, serial, "input", "keyevent", "KEYCODE_WAKEUP")

	// Step 1: Push scrcpy-server to device
	logger.Info("pushing scrcpy-server to device")
	if err := adb.Push(ctx, serial, scrcpyServerPath, "/data/local/tmp/scrcpy-server.jar"); err != nil {
		cancel()
		return nil, fmt.Errorf("push scrcpy-server: %w", err)
	}

	// Step 2: Listen on a free local port (reverse tunnel: server connects to us)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	s.videoPort = port

	abstractName := fmt.Sprintf("scrcpy_%08x", scid)

	cleanup := func() {
		listener.Close()
		_ = adb.RemoveReverse(context.Background(), serial, abstractName)
	}

	// Step 3: Set up ADB reverse tunnel (device connects to our listener)
	logger.Info("setting up reverse tunnel", "port", port, "abstract", abstractName)
	if err := adb.Reverse(ctx, serial, abstractName, port); err != nil {
		cancel()
		listener.Close()
		return nil, fmt.Errorf("adb reverse: %w", err)
	}

	// Step 4: Launch scrcpy-server on device (reverse tunnel mode, no tunnel_forward)
	serverArgs := buildServerArgs(scid, scrcpyVersion, opts)
	logger.Info("launching scrcpy-server", "args", serverArgs)
	go func() {
		out, err := adb.ServerShell(ctx, serial, serverArgs...)
		if err != nil && ctx.Err() == nil {
			logger.Error("scrcpy-server exited", "error", err, "output", string(out))
		}
	}()

	// Step 5: Accept video connection from server (with timeout)
	logger.Info("waiting for video connection")
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second))
	videoConn, err := listener.Accept()
	if err != nil {
		cancel()
		cleanup()
		return nil, fmt.Errorf("accept video: %w", err)
	}
	s.videoConn = videoConn

	// Step 6: Read video handshake (reverse tunnel: no dummy byte)
	// 64 bytes device name + 4 bytes codec + 4 bytes width + 4 bytes height = 76 bytes
	handshake := make([]byte, 76)
	if _, err := io.ReadFull(videoConn, handshake); err != nil {
		cancel()
		videoConn.Close()
		cleanup()
		return nil, fmt.Errorf("read handshake: %w", err)
	}

	s.DeviceName = strings.TrimRight(string(handshake[0:64]), "\x00")
	// handshake[64:68] = codec ID (H.264 = 0x68323634)
	s.Width = int(binary.BigEndian.Uint32(handshake[68:72]))
	s.Height = int(binary.BigEndian.Uint32(handshake[72:76]))

	logger.Info("video handshake complete",
		"device_name", s.DeviceName,
		"width", s.Width,
		"height", s.Height,
	)

	// Step 7: Accept the audio connection, which scrcpy-server opens between
	// video and control. Its header comes later, once the device's encoder
	// runs (or fails), so it is read by audioReadLoop, never waited on here.
	if opts.wantsAudio() {
		_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
		audioConn, err := listener.Accept()
		if err != nil {
			cancel()
			videoConn.Close()
			cleanup()
			return nil, fmt.Errorf("accept audio: %w", err)
		}
		s.audioConn = audioConn
	}

	// Step 8: Accept control connection from server
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	controlConn, err := listener.Accept()
	if err != nil {
		cancel()
		videoConn.Close()
		if s.audioConn != nil {
			s.audioConn.Close()
		}
		cleanup()
		return nil, fmt.Errorf("accept control: %w", err)
	}
	s.controlConn = controlConn
	listener.Close()

	// Step 9: Start control read loop (reads clipboard messages from device)
	go s.controlReadLoop(ctx)

	if s.audioConn != nil {
		go s.audioReadLoop()
	} else {
		s.setAudioStatus(AudioStatus{Reason: "audio is off for this session"})
		close(s.audioDone)
	}

	// Step 10: Start video read loop
	go s.videoReadLoop(ctx, adb, serial, abstractName)

	return s, nil
}

func buildServerArgs(scid uint32, version string, opts SessionOptions) []string {
	args := []string{
		"CLASSPATH=/data/local/tmp/scrcpy-server.jar",
		"app_process", "/", "com.genymobile.scrcpy.Server", version,
		"control=true",
		"video_codec=h264",
		"send_frame_meta=true",
		"stay_awake=true",
		"power_on=true",
		fmt.Sprintf("scid=%08x", scid),
	}
	if opts.wantsAudio() {
		// "output" captures what the phone plays and mutes its speaker;
		// audio_dup (keep playing on the phone) is deliberately not set.
		args = append(args, "audio=true", "audio_codec=opus", "audio_source=output")
	} else {
		args = append(args, "audio=false")
	}
	maxSize := opts.MaxSize
	if maxSize <= 0 {
		maxSize = 1024 // Default: scale down to reduce encoder load
	}
	args = append(args, fmt.Sprintf("max_size=%d", maxSize))

	maxFPS := opts.MaxFPS
	if maxFPS <= 0 {
		maxFPS = 30
	}
	args = append(args, fmt.Sprintf("max_fps=%d", maxFPS))
	return args
}

// videoReadLoop reads H.264 NALUs from the video socket and broadcasts to subscribers.
func (s *Session) videoReadLoop(ctx context.Context, adb *ADB, serial string, abstractName string) {
	defer func() {
		_ = adb.RemoveReverse(context.Background(), serial, abstractName)
		s.videoEndedAt = time.Now()
		close(s.done)
	}()

	headerBuf := make([]byte, 12)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read 12-byte frame header
		// Bytes 0-7: PTS (with flags in MSB bits)
		// Bytes 8-11: packet size
		if _, err := io.ReadFull(s.videoConn, headerBuf); err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Error("video read header error", "error", err)
			return
		}

		ptsAndFlags := binary.BigEndian.Uint64(headerBuf[0:8])
		size := binary.BigEndian.Uint32(headerBuf[8:12])

		if size == 0 || size > 10*1024*1024 { // sanity check: max 10MB per frame
			s.logger.Warn("invalid frame size", "size", size)
			return
		}

		// Read NALU data
		naluData := make([]byte, size)
		if _, err := io.ReadFull(s.videoConn, naluData); err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Error("video read NALU error", "error", err)
			return
		}

		// Check if this is a config packet (PTS flags)
		// In scrcpy, bit 63 of PTS indicates config packet, bit 62 indicates key frame
		isConfig := (ptsAndFlags >> 63) & 1
		if isConfig == 1 {
			s.configMu.Lock()
			// Store header + NALU as config packet
			s.configPacket = make([]byte, 12+len(naluData))
			copy(s.configPacket, headerBuf)
			copy(s.configPacket[12:], naluData)
			s.configMu.Unlock()
		}

		// Build message: header + NALU
		msg := make([]byte, 12+len(naluData))
		copy(msg, headerBuf)
		copy(msg[12:], naluData)

		// Broadcast to all subscribers
		s.subscribersMu.RLock()
		for _, ch := range s.videoSubscribers {
			select {
			case ch <- msg:
			default:
				// Drop frame if subscriber is slow
			}
		}
		s.subscribersMu.RUnlock()
	}
}

// SubscribeVideo creates a new video subscription. Returns a channel that receives
// raw video packets (12-byte header + H.264 NALU data).
func (s *Session) SubscribeVideo(id string) chan []byte {
	ch := make(chan []byte, 60) // buffer ~1 second at 60fps

	s.subscribersMu.Lock()
	s.videoSubscribers[id] = ch
	s.subscribersMu.Unlock()

	return ch
}

// GetConfigPacket returns the stored SPS/PPS config packet, if available.
func (s *Session) GetConfigPacket() []byte {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if s.configPacket == nil {
		return nil
	}
	cp := make([]byte, len(s.configPacket))
	copy(cp, s.configPacket)
	return cp
}

// UnsubscribeVideo removes a video subscription.
func (s *Session) UnsubscribeVideo(id string) {
	s.subscribersMu.Lock()
	if ch, ok := s.videoSubscribers[id]; ok {
		close(ch)
		delete(s.videoSubscribers, id)
	}
	s.subscribersMu.Unlock()
}

// ClaimControl claims control for a client (last-writer-wins).
func (s *Session) ClaimControl(id string) bool {
	s.controlOwnerMu.Lock()
	defer s.controlOwnerMu.Unlock()
	s.controlOwner = id
	return true
}

// ReleaseControl releases control ownership.
func (s *Session) ReleaseControl(id string) {
	s.controlOwnerMu.Lock()
	defer s.controlOwnerMu.Unlock()
	if s.controlOwner == id {
		s.controlOwner = ""
	}
}

// WriteControl sends a binary control message to the device. It is safe to
// call from multiple goroutines: writes are serialized so scrcpy's binary
// control protocol is never interleaved.
// RequestKeyframe makes the device send a new config packet and keyframe.
// scrcpy only sends frames when the screen changes, so a viewer joining an
// existing session would otherwise see nothing until something moves.
func (s *Session) RequestKeyframe() error {
	return s.WriteControl(EncodeResetVideo())
}

func (s *Session) WriteControl(data []byte) error {
	if s.controlConn == nil {
		return fmt.Errorf("control connection not established")
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	_, err := s.controlConn.Write(data)
	return err
}

// IsAlive returns true if the video read loop is still running.
func (s *Session) IsAlive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// Close shuts down the session and cleans up resources.
func (s *Session) Close() {
	s.cancel()

	if s.videoConn != nil {
		s.videoConn.Close()
	}
	if s.controlConn != nil {
		s.controlConn.Close()
	}
	if s.audioConn != nil {
		s.audioConn.Close()
	}
	s.endAudio()

	// Close all subscriber channels
	s.subscribersMu.Lock()
	for id, ch := range s.videoSubscribers {
		close(ch)
		delete(s.videoSubscribers, id)
	}
	s.subscribersMu.Unlock()

	// Wait for video loop to finish
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		s.logger.Warn("timeout waiting for video loop to finish")
	}
	select {
	case <-s.audioDone:
	case <-time.After(5 * time.Second):
		s.logger.Warn("timeout waiting for audio loop to finish")
	}
}

// controlReadLoop reads device messages from the control connection.
// Currently handles clipboard (type 0) and ACK (type 1) messages.
func (s *Session) controlReadLoop(ctx context.Context) {
	reader := bufio.NewReader(s.controlConn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read message type (1 byte)
		msgType, err := reader.ReadByte()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Debug("control read loop ended", "error", err)
			return
		}

		switch msgType {
		case DeviceMsgTypeClipboard:
			// Format: textLen(4) + text(N)
			lenBuf := make([]byte, 4)
			if _, err := io.ReadFull(reader, lenBuf); err != nil {
				s.logger.Debug("control read clipboard length error", "error", err)
				return
			}
			textLen := binary.BigEndian.Uint32(lenBuf)
			if textLen > 10*1024*1024 { // 10MB sanity limit
				s.logger.Warn("clipboard text too large", "size", textLen)
				return
			}
			textBuf := make([]byte, textLen)
			if _, err := io.ReadFull(reader, textBuf); err != nil {
				s.logger.Debug("control read clipboard text error", "error", err)
				return
			}
			text := string(textBuf)

			s.clipboardMu.Lock()
			s.clipboardText = text
			s.clipboardMu.Unlock()

			// Non-blocking send to clipboard channel
			select {
			case s.clipboardCh <- text:
			default:
				// Drop if no one is listening
			}

		case DeviceMsgTypeACKClipboard:
			// Format: sequence(8)
			ackBuf := make([]byte, 8)
			if _, err := io.ReadFull(reader, ackBuf); err != nil {
				s.logger.Debug("control read ACK error", "error", err)
				return
			}
			// ACK is ignored for now

		default:
			s.logger.Debug("unknown device message type", "type", msgType)
			return
		}
	}
}

// GetClipboardText returns the last received clipboard text from the device.
func (s *Session) GetClipboardText() string {
	s.clipboardMu.RLock()
	defer s.clipboardMu.RUnlock()
	return s.clipboardText
}

// ClipboardCh returns a channel that receives clipboard text updates from the device.
func (s *Session) ClipboardCh() <-chan string {
	return s.clipboardCh
}
