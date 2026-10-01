// Command fakeadb stands in for adb in tests: one phone, FAKE01, whose
// scrcpy-server is simulated well enough for a real Batter session. When
// Batter launches the server it connects back through the recorded reverse
// tunnel, sends the video handshake, and answers each RESET_VIDEO on the
// control socket with a config packet and a keyframe (logging it to
// $FAKEADB_DIR/resets). Everything else succeeds quietly.
//
// Full-tier sessions (audio=true) also get an Opus audio stream.
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
	"strings"
	"time"
)

const serial = "FAKE01"

var dir = cmp.Or(os.Getenv("FAKEADB_DIR"), "/tmp/fakeadb")

func main() {
	_ = os.MkdirAll(dir, 0o755)
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "-s" {
		if args[1] != serial {
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
		fmt.Printf("List of devices attached\n%s device product:fake model:Fake_Phone device:fake transport_id:1\n", serial)
	case "get-state":
		fmt.Println("device")
	case "reverse":
		reverse(args[1:])
	case "shell":
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
			if err := writePacket(video, 1<<63, []byte{0, 0, 0, 1, 0x67, 0x42}); err != nil { // config
				return err
			}
			if err := writePacket(video, 1<<62, []byte{0, 0, 0, 1, 0x65, 0x88}); err != nil { // keyframe
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
