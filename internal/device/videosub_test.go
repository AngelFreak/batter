package device

import (
	"encoding/binary"
	"testing"
)

// packet builds a frame with scrcpy flags and a sequence number payload.
func packet(config, key bool, seq uint32) []byte {
	msg := make([]byte, 16)
	var flags uint64
	if config {
		flags |= 1 << 62
	}
	if key {
		flags |= 1 << 61
	}
	binary.BigEndian.PutUint64(msg, flags)
	binary.BigEndian.PutUint32(msg[8:], 4)
	binary.BigEndian.PutUint32(msg[12:], seq)
	return msg
}

func drain(ch <-chan []byte) [][]byte {
	var out [][]byte
	for {
		select {
		case m := <-ch:
			out = append(out, m)
		default:
			return out
		}
	}
}

func TestSlowViewerSkipsToKeyframeWhileFastViewerGetsEverything(t *testing.T) {
	s := &Session{videoSubscribers: map[string]*videoSub{}}
	fast := s.SubscribeVideo("fast")
	slow := s.SubscribeVideo("slow")

	var fastGot [][]byte
	send := func(m []byte) {
		s.broadcast(m)
		fastGot = append(fastGot, drain(fast)...) // the fast viewer keeps up
	}
	send(packet(true, false, 0)) // config
	send(packet(false, true, 1)) // keyframe
	for seq := uint32(2); seq < 100; seq++ {
		send(packet(false, false, seq)) // the slow viewer never reads
		if n := len(slow); n > videoSubBuffer {
			t.Fatalf("slow viewer's backlog %d > %d", n, videoSubBuffer)
		}
	}
	send(packet(true, false, 100)) // config: never dropped
	send(packet(false, false, 101))
	send(packet(false, true, 102)) // keyframe: slow viewer resumes here
	send(packet(false, false, 103))

	if len(fastGot) != 104 {
		t.Fatalf("fast viewer got %d packets, want all 104", len(fastGot))
	}
	got := drain(slow)
	if len(got) == 0 {
		t.Fatal("slow viewer got nothing")
	}
	sawConfig100 := false
	for i, m := range got {
		config, key := packetFlags(m)
		seq := binary.BigEndian.Uint32(m[12:])
		if config && seq == 100 {
			sawConfig100 = true
		}
		// After a gap, the first frame decoded must be a keyframe.
		if i > 0 && !config && !key {
			prev := binary.BigEndian.Uint32(got[i-1][12:])
			if seq != prev+1 {
				t.Fatalf("slow viewer got delta %d right after %d (decoding across a gap)", seq, prev)
			}
		}
	}
	if !sawConfig100 {
		t.Fatalf("config packet dropped for the slow viewer: %v", seqs(got))
	}
	if last := binary.BigEndian.Uint32(got[len(got)-1][12:]); last != 103 {
		t.Fatalf("slow viewer ends at %d, want the latest frame 103: %v", last, seqs(got))
	}
}

func TestNewViewerStartsAtAKeyframe(t *testing.T) {
	s := &Session{videoSubscribers: map[string]*videoSub{}}
	ch := s.SubscribeVideo("v")
	s.broadcast(packet(false, false, 1))
	s.broadcast(packet(false, true, 2))
	s.broadcast(packet(false, false, 3))
	if got := seqs(drain(ch)); len(got) != 2 || got[0] != 2 {
		t.Fatalf("new viewer got %v, want [2 3] (nothing before the keyframe)", got)
	}
}

func seqs(msgs [][]byte) []uint32 {
	var out []uint32
	for _, m := range msgs {
		out = append(out, binary.BigEndian.Uint32(m[12:]))
	}
	return out
}

// Audio packets are independent, so a lagging listener just skips ahead:
// its queue stays short and it gets the newest audio, not a growing delay.
func TestSlowAudioListenerSkipsAhead(t *testing.T) {
	s := &Session{audioSubscribers: map[string]chan []byte{}}
	ch := s.SubscribeAudio("slow")
	s.broadcastAudio(packet(true, false, 0)) // config
	for seq := uint32(1); seq <= 200; seq++ {
		s.broadcastAudio(packet(false, false, seq))
		if n := len(ch); n > audioSubBuffer {
			t.Fatalf("audio backlog %d > %d", n, audioSubBuffer)
		}
	}
	got := seqs(drain(ch))
	if got[len(got)-1] != 200 {
		t.Fatalf("listener ends at %d, want the newest packet 200: %v", got[len(got)-1], got)
	}
}
