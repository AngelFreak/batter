package device

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// maxAudioPacket bounds an audio packet; Opus packets are a few hundred bytes.
const maxAudioPacket = 1 << 20

// audioReadLoop reads the audio socket: a 4-byte codec ID (or a disable
// code), then packets with the same 12-byte header as video. It only ever
// ends the audio stream, never the session.
func (s *Session) audioReadLoop() {
	defer close(s.audioDone)
	defer s.endAudio()

	var codec [4]byte
	if _, err := io.ReadFull(s.audioConn, codec[:]); err != nil {
		s.setAudioStatus(AudioStatus{Reason: "the device closed the audio stream"})
		return
	}
	switch id := binary.BigEndian.Uint32(codec[:]); id {
	case audioCodecOpus:
		s.setAudioStatus(AudioStatus{Available: true, Codec: "opus"})
	case audioCodecDisabled, audioCodecConfigError:
		reason := "the device cannot capture audio (it needs Android 11 or newer)"
		if id == audioCodecConfigError {
			reason = "audio configuration error on the device"
		}
		s.audioMu.Lock()
		s.audioDisabledAt = time.Now()
		s.audioMu.Unlock()
		s.logger.Warn("device disabled audio", "reason", reason)
		s.setAudioStatus(AudioStatus{Reason: reason})
		return
	default:
		s.setAudioStatus(AudioStatus{Reason: fmt.Sprintf("unsupported audio codec 0x%08x", id)})
		return
	}

	header := make([]byte, 12)
	for {
		if _, err := io.ReadFull(s.audioConn, header); err != nil {
			s.logger.Debug("audio stream ended", "error", err)
			return
		}
		if binary.BigEndian.Uint64(header[0:8])&PacketFlagSession != 0 {
			s.logger.Warn("session packet on the audio stream; protocol mismatch")
			return
		}
		size := binary.BigEndian.Uint32(header[8:12])
		if size == 0 || size > maxAudioPacket {
			s.logger.Warn("invalid audio packet size", "size", size)
			return
		}
		msg := make([]byte, 12+size)
		copy(msg, header)
		if _, err := io.ReadFull(s.audioConn, msg[12:]); err != nil {
			s.logger.Debug("audio stream ended", "error", err)
			return
		}

		s.broadcastAudio(msg)
	}
}

// audioSubBuffer is how many packets a listener may have queued (~200ms of
// 20ms Opus packets).
const audioSubBuffer = 10

// broadcastAudio delivers one packet to every listener without blocking.
// Opus packets decode independently, so a listener that falls behind drops
// its backlog and continues from the newest packet (latency-first, like
// video); the config packet is kept.
func (s *Session) broadcastAudio(msg []byte) {
	isConfig := binary.BigEndian.Uint64(msg[0:8])&PacketFlagConfig != 0
	s.audioMu.Lock()
	defer s.audioMu.Unlock()
	if isConfig {
		s.audioConfig = msg
	}
	for _, ch := range s.audioSubscribers {
		if !offer(ch, msg) {
			dropBacklog(ch)
			offer(ch, msg)
		}
	}
}

// AudioSubscribers returns how many listeners are subscribed to the audio.
func (s *Session) AudioSubscribers() int {
	s.audioMu.RLock()
	defer s.audioMu.RUnlock()
	return len(s.audioSubscribers)
}

// setAudioStatus records the audio status and releases WaitAudio callers.
func (s *Session) setAudioStatus(st AudioStatus) {
	s.audioMu.Lock()
	defer s.audioMu.Unlock()
	s.audioStatus = st
	select {
	case <-s.audioReady:
	default:
		close(s.audioReady)
	}
}

// endAudio marks the audio stream over and releases its subscribers.
func (s *Session) endAudio() {
	s.audioMu.Lock()
	if s.audioStatus.Available {
		s.audioStatus = AudioStatus{Reason: "the audio stream ended"}
	}
	s.audioEnded = true
	for id, ch := range s.audioSubscribers {
		close(ch)
		delete(s.audioSubscribers, id)
	}
	s.audioMu.Unlock()

	// A stream that ends before saying anything still has to answer WaitAudio.
	select {
	case <-s.audioReady:
	default:
		s.setAudioStatus(AudioStatus{Reason: "the audio stream ended"})
	}
}

// WaitAudio blocks until the device says whether it can stream audio (it
// does so once its encoder runs), or ctx ends.
func (s *Session) WaitAudio(ctx context.Context) AudioStatus {
	select {
	case <-s.audioReady:
	case <-ctx.Done():
		return AudioStatus{Reason: "the device has not started audio yet"}
	}
	s.audioMu.RLock()
	defer s.audioMu.RUnlock()
	return s.audioStatus
}

// SubscribeAudio returns a channel of audio packets (12-byte header + Opus
// data). It is closed when the audio stream or the session ends.
func (s *Session) SubscribeAudio(id string) chan []byte {
	ch := make(chan []byte, audioSubBuffer)
	s.audioMu.Lock()
	defer s.audioMu.Unlock()
	if s.audioEnded {
		close(ch)
		return ch
	}
	s.audioSubscribers[id] = ch
	return ch
}

// UnsubscribeAudio removes an audio subscription.
func (s *Session) UnsubscribeAudio(id string) {
	s.audioMu.Lock()
	defer s.audioMu.Unlock()
	if ch, ok := s.audioSubscribers[id]; ok {
		close(ch)
		delete(s.audioSubscribers, id)
	}
}

// GetAudioConfigPacket returns the Opus config packet (header + OpusHead),
// which a decoder needs before any audio, or nil if none arrived yet.
func (s *Session) GetAudioConfigPacket() []byte {
	s.audioMu.RLock()
	defer s.audioMu.RUnlock()
	if s.audioConfig == nil {
		return nil
	}
	return append([]byte(nil), s.audioConfig...)
}

// diedFromAudio reports whether the session's video ended right after the
// device disabled audio: scrcpy-server does that on a fatal audio error.
// Only meaningful once the session is dead.
func (s *Session) diedFromAudio() bool {
	s.audioMu.RLock()
	disabledAt := s.audioDisabledAt
	s.audioMu.RUnlock()
	return !disabledAt.IsZero() && s.videoEndedAt.Sub(disabledAt) < 3*time.Second
}
