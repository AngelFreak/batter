// Command fakeadb stands in for adb in tests: one phone, FAKE01, whose
// scrcpy-server is simulated well enough for a real Batter session. When
// Batter launches the server it connects back through the recorded reverse
// tunnel, sends the video handshake, and answers each RESET_VIDEO on the
// control socket with a config packet and a keyframe (logging it to
// $FAKEADB_DIR/resets). Everything else succeeds quietly.
//
// Full-tier sessions (audio=true) also get an Opus audio stream.
//
// With $FAKEADB_STREAM_FPS set it also streams frames at that rate (a
// keyframe every second, $FAKEADB_FRAME_BYTES each). Each frame's payload
// is a start code, then its sequence number (uint32) and send time (unix
// nanoseconds, int64), so tests can check order and latency.
//
// The phone can also be on the LAN: with $FAKEADB_DIR/lan holding an
// address (ip:port) its adbd listens there, `adb connect <addr>` succeeds
// and the phone is then also reachable as -s <addr>. $FAKEADB_DIR/unplugged
// takes it off USB. `shell getprop ro.serialno` answers FAKE01 on either.
// Every call is logged to $FAKEADB_DIR/calls, so tests can check which
// transport a command went to.
//
// State lives in $FAKEADB_DIR (default /tmp/fakeadb).
package main

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const serial = "FAKE01"

var dir = cmp.Or(os.Getenv("FAKEADB_DIR"), "/tmp/fakeadb")

func main() {
	_ = os.MkdirAll(dir, 0o755)
	args := os.Args[1:]
	logCall(args)
	if len(args) >= 2 && args[0] == "-s" {
		if !slices.Contains(transports(), args[1]) {
			fmt.Fprintf(os.Stderr, "adb: device '%s' not found\n", args[1])
			os.Exit(1)
		}
		args = args[2:]
	}
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "version":
		fmt.Println("Android Debug Bridge version 1.0.41 (fake)")
	case "devices":
		fmt.Println("List of devices attached")
		for _, t := range transports() {
			fmt.Printf("%s device product:fake model:Fake_Phone device:fake transport_id:1\n", t)
		}
	case "connect":
		if len(args) == 2 && args[1] == state("lan") {
			_ = os.WriteFile(filepath.Join(dir, "connected"), []byte(args[1]), 0o644)
			fmt.Println("connected to " + args[1])
		} else {
			fmt.Printf("failed to connect to '%s': Connection refused\n", strings.Join(args[1:], " "))
			os.Exit(1)
		}
	case "disconnect":
		_ = os.Remove(filepath.Join(dir, "connected"))
	case "tcpip":
		_ = os.WriteFile(filepath.Join(dir, "tcpip"), []byte(args[len(args)-1]), 0o644)
		fmt.Println("restarting in TCP mode port: " + args[len(args)-1])
	case "get-state":
		fmt.Println("device")
	case "reverse":
		reverse(args[1:])
	case "shell":
		if len(args) == 3 && args[1] == "getprop" && args[2] == "ro.serialno" {
			fmt.Println(serial)
		}
		if len(args) > 2 && strings.HasPrefix(args[1], "CLASSPATH=") {
			if err := scrcpyServer(args); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	case "exec-out":
		os.Exit(1)
	}
}

// transports lists the names adb knows the phone by: FAKE01 while on USB,
// and its LAN address once connected there.
func transports() []string {
	var ts []string
	if _, err := os.Stat(filepath.Join(dir, "unplugged")); err != nil {
		ts = append(ts, serial)
	}
	if c := state("connected"); c != "" && c == state("lan") {
		ts = append(ts, c)
	}
	return ts
}

func state(name string) string {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	return strings.TrimSpace(string(b))
}

func logCall(args []string) {
	if f, err := os.OpenFile(filepath.Join(dir, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
	}
}

func reverse(args []string) {
	switch {
	case len(args) == 2 && !strings.HasPrefix(args[0], "-"):
		_ = os.WriteFile(filepath.Join(dir, "reverse-"+strings.TrimPrefix(args[0], "localabstract:")), []byte(args[1]), 0o644)
	case len(args) == 1 && args[0] == "--list":
		files, _ := filepath.Glob(filepath.Join(dir, "reverse-*"))
		for _, f := range files {
			host, _ := os.ReadFile(f)
			fmt.Printf("UsbFfs localabstract:%s %s\n", strings.TrimPrefix(filepath.Base(f), "reverse-"), host)
		}
	case len(args) == 2 && args[0] == "--remove":
		_ = os.Remove(filepath.Join(dir, "reverse-"+strings.TrimPrefix(args[1], "localabstract:")))
	}
}

func scrcpyServer(args []string) error {
	var scid string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "scid="); ok {
			scid = v
		}
	}
	host, err := os.ReadFile(filepath.Join(dir, "reverse-scrcpy_"+scid))
	if err != nil {
		return fmt.Errorf("no reverse tunnel for scid %s", scid)
	}
	addr := "127.0.0.1:" + strings.TrimPrefix(string(host), "tcp:")
	video, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	// scrcpy connects video, then audio (if enabled), then control.
	var audio net.Conn
	for _, a := range args {
		if a == "audio=true" {
			if audio, err = net.Dial("tcp", addr); err != nil {
				return err
			}
		}
	}
	handshake := make([]byte, 76)
	copy(handshake, "Fake Phone")
	copy(handshake[64:], "h264")
	binary.BigEndian.PutUint32(handshake[68:], 1080)
	binary.BigEndian.PutUint32(handshake[72:], 1920)
	if _, err := video.Write(handshake); err != nil {
		return err
	}
	control, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	if audio != nil {
		go streamAudio(audio)
	}
	var videoMu sync.Mutex
	if fps, _ := strconv.Atoi(os.Getenv("FAKEADB_STREAM_FPS")); fps > 0 {
		size, _ := strconv.Atoi(os.Getenv("FAKEADB_FRAME_BYTES"))
		go stream(video, &videoMu, fps, max(size, 16))
	}
	buf := make([]byte, 256)
	for {
		n, err := control.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		for _, b := range buf[:n] {
			if b != 17 { // RESET_VIDEO
				continue
			}
			if f, err := os.OpenFile(filepath.Join(dir, "resets"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				fmt.Fprintln(f, "reset")
				f.Close()
			}
			videoMu.Lock()
			err := writePacket(video, 1<<63, []byte{0, 0, 0, 1, 0x67, 0x42}) // config
			if err == nil {
				err = writePacket(video, 1<<62, frame(0, 16)) // keyframe
			}
			videoMu.Unlock()
			if err != nil {
				return err
			}
		}
	}
}

func writePacket(w io.Writer, flags uint64, payload []byte) error {
	header := make([]byte, 12)
	binary.BigEndian.PutUint64(header, flags)
	binary.BigEndian.PutUint32(header[8:], uint32(len(payload)))
	_, err := w.Write(append(header, payload...))
	return err
}

func frame(seq uint32, size int) []byte {
	p := make([]byte, size)
	copy(p, []byte{0, 0, 0, 1})
	binary.BigEndian.PutUint32(p[4:], seq)
	binary.BigEndian.PutUint64(p[8:], uint64(time.Now().UnixNano()))
	return p
}

func stream(video net.Conn, mu *sync.Mutex, fps, size int) {
	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()
	for seq := uint32(1); ; seq++ {
		<-ticker.C
		var flags uint64
		if seq%uint32(fps) == 0 {
			flags = 1 << 62
		}
		mu.Lock()
		err := writePacket(video, flags, frame(seq, size))
		mu.Unlock()
		if err != nil {
			return
		}
	}
}

// streamAudio sends the Opus codec ID, a config packet, then a 20ms packet
// stream, like scrcpy's audio socket.
func streamAudio(audio net.Conn) {
	if _, err := audio.Write(binary.BigEndian.AppendUint32(nil, 0x6f707573)); err != nil { // "opus"
		return
	}
	if err := writePacket(audio, 1<<63, []byte("OpusHead")); err != nil {
		return
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for pts := uint64(0); ; pts += 20000 {
		<-ticker.C
		if err := writePacket(audio, pts, []byte{0xfc, 0xff, 0xfe}); err != nil {
			return
		}
	}
}
