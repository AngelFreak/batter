package device

import "encoding/binary"

// Video fan-out is latency-first: a viewer that can't keep up skips ahead
// to the next keyframe instead of queueing frames (which made remote
// viewers on slow links run seconds behind). Each subscriber has a small
// buffer; when it is full, the subscriber's queued deltas are dropped, it
// ignores deltas until the next keyframe, and a keyframe is requested
// (rate-limited, see RequestKeyframe) so it resyncs quickly. Config packets
// are never dropped, and the session's read loop never blocks on a viewer.

// videoSubBuffer is how many packets a viewer may have queued (~130ms at
// 30fps).
const videoSubBuffer = 4

type videoSub struct {
	ch chan []byte
	// resync: waiting for a keyframe, so deltas are useless and dropped. A
	// new viewer starts here: it can't decode until a keyframe anyway. Only
	// the read loop (broadcast) touches it.
	resync bool
}

func newVideoSub() *videoSub {
	return &videoSub{ch: make(chan []byte, videoSubBuffer), resync: true}
}

// packetFlags reads scrcpy's frame-meta flags (see PacketFlagConfig).
func packetFlags(msg []byte) (config, key bool) {
	if len(msg) < 12 {
		return false, false
	}
	flags := binary.BigEndian.Uint64(msg[0:8])
	return flags&PacketFlagConfig != 0, flags&PacketFlagKeyFrame != 0
}

// broadcast delivers one packet to every subscriber without blocking.
func (s *Session) broadcast(msg []byte) {
	config, key := packetFlags(msg)
	needKeyframe := false
	s.subscribersMu.RLock()
	for _, sub := range s.videoSubscribers {
		switch {
		case config:
			if !sub.offer(msg) {
				sub.dropBacklog()
				sub.offer(msg)
				sub.resync = true
			}
		case key:
			if !sub.offer(msg) {
				sub.dropBacklog()
				sub.offer(msg)
			}
			sub.resync = false
		case sub.resync:
			// A delta can't be decoded without the keyframe it follows.
		default:
			if !sub.offer(msg) {
				sub.dropBacklog()
				sub.resync = true
				needKeyframe = true
			}
		}
	}
	s.subscribersMu.RUnlock()
	if needKeyframe {
		go func() {
			if err := s.RequestKeyframe(); err != nil && s.logger != nil {
				s.logger.Debug("keyframe request for a lagging viewer failed", "error", err)
			}
		}()
	}
}

func (v *videoSub) offer(msg []byte) bool { return offer(v.ch, msg) }

func (v *videoSub) dropBacklog() { dropBacklog(v.ch) }

// offer queues msg unless ch is full.
func offer(ch chan []byte, msg []byte) bool {
	select {
	case ch <- msg:
		return true
	default:
		return false
	}
}

// dropBacklog empties ch, keeping the latest config packet queued (the
// decoder needs it; it is never dropped).
func dropBacklog(ch chan []byte) {
	var config []byte
	for {
		select {
		case msg := <-ch:
			if c, _ := packetFlags(msg); c {
				config = msg
			}
		default:
			if config != nil {
				offer(ch, config)
			}
			return
		}
	}
}
