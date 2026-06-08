package device

import "testing"

func TestCheckSerial(t *testing.T) {
	valid := []string{
		"4B191JEBF18123",           // USB serial
		"34011JEHN18959",           // USB serial
		"192.168.1.50:5555",        // network serial (host:port)
		"emulator-5554",            // emulator name
		"R3CN30XXXX",               // alphanumeric
		"some_device.with-chars:1", // full charset
	}
	for _, s := range valid {
		if err := checkSerial(s); err != nil {
			t.Errorf("checkSerial(%q) = %v, want nil", s, err)
		}
	}

	invalid := []string{
		"",             // empty
		"-e",           // adb option injection
		"--help",       // adb option injection
		"-s",           // adb option injection
		"dev ice",      // space
		"dev;rm -rf /", // shell metacharacters
		"dev$(whoami)", // command substitution
		"dev/../etc",   // path traversal chars
		"dev\nname",    // newline
	}
	for _, s := range invalid {
		if err := checkSerial(s); err == nil {
			t.Errorf("checkSerial(%q) = nil, want error", s)
		}
	}
}
