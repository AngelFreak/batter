package device

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const lanAddr = "10.77.0.100:5555"

func calledWith(t *testing.T, calls string) []string {
	t.Helper()
	b, _ := os.ReadFile(calls)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// Every per-device adb call goes to the phone's current transport, so a
// phone on the LAN is driven over adb-over-TCP with no caller knowing.
func TestDeviceCommandsUseThePhonesTransport(t *testing.T) {
	adb, calls := fakeADBBinary(t, "")
	adb.SetTransport("SER1", lanAddr)
	ctx := context.Background()
	apk := filepath.Join(t.TempDir(), "a.apk")

	_, _ = adb.Shell(ctx, "SER1", "true")
	_ = adb.Push(ctx, "SER1", apk, "/sdcard/a")
	_ = adb.Reverse(ctx, "SER1", "scrcpy_1", 1234)
	_ = adb.RemoveReverse(ctx, "SER1", "scrcpy_1")
	_, _ = adb.ListReverse(ctx, "SER1")
	_ = adb.Forward(ctx, "SER1", 1234, "x")
	_, _ = adb.Screenshot(ctx, "SER1")
	_, _ = adb.Install(ctx, "SER1", apk)
	_, _ = adb.ServerShell(ctx, "SER1", "app_process")
	_, _ = adb.ShellSecret(ctx, "SER1", "locksettings")
	_, _ = adb.GetState(ctx, "SER1")

	lines := calledWith(t, calls)
	if len(lines) != 11 {
		t.Fatalf("want 11 adb calls, got %d: %q", len(lines), lines)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "-s "+lanAddr+" ") {
			t.Errorf("call %q didn't go to the LAN transport", l)
		}
	}

	// Back on USB (transport gone): the serial itself is the transport.
	adb.ClearTransport("SER1")
	_, _ = adb.Shell(ctx, "SER1", "true")
	lines = calledWith(t, calls)
	if last := lines[len(lines)-1]; last != "-s SER1 shell true" {
		t.Fatalf("after clearing the LAN transport: %q", last)
	}
}

func TestTransportsStillRejectOptionLikeSerials(t *testing.T) {
	adb, calls := fakeADBBinary(t, "")
	adb.SetTransport("-bad", lanAddr)
	if _, err := adb.Shell(context.Background(), "-bad", "true"); err == nil {
		t.Fatal("option-like serial accepted")
	}
	if _, err := os.Stat(calls); err == nil {
		t.Fatal("adb ran for an invalid serial")
	}
}

// Listings name LAN phones by their real serial, never by ip:port, and
// TCP transports Batter hasn't identified yet are left out (they'd be
// registered under the wrong key).
func TestListDevicesNamesLANPhonesBySerial(t *testing.T) {
	adb, _ := fakeADBBinary(t, `printf 'List of devices attached\n`+
		`USB1 device product:a model:A\n`+
		`10.77.0.100:5555 device product:b model:B\n`+
		`10.77.0.101:5555 device product:c model:C\n'`)
	adb.SetTransport("SER2", lanAddr)
	devs, err := adb.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("devices: %+v", devs)
	}
	if devs[0].Serial != "USB1" || devs[0].Transport != "USB1" {
		t.Errorf("USB phone: %+v", devs[0])
	}
	if devs[1].Serial != "SER2" || devs[1].Transport != lanAddr || devs[1].Model != "B" || devs[1].State != "device" {
		t.Errorf("LAN phone: %+v", devs[1])
	}
}

// A phone seen on USB and on the LAN at once is listed once, as the
// transport its commands go to (the LAN one).
func TestListDevicesListsAPhoneOnBothTransportsOnce(t *testing.T) {
	adb, _ := fakeADBBinary(t, `printf 'List of devices attached\n`+
		`SER2 device product:b model:B\n`+
		`10.77.0.100:5555 device product:b model:B\n'`)
	adb.SetTransport("SER2", lanAddr)
	devs, err := adb.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 || devs[0].Serial != "SER2" || devs[0].Transport != lanAddr {
		t.Fatalf("devices: %+v", devs)
	}
}

func TestConnectReportsFailureEvenOnExitZero(t *testing.T) {
	for _, tc := range []struct {
		out string
		ok  bool
	}{
		{"connected to " + lanAddr, true},
		{"already connected to " + lanAddr, true},
		{"failed to connect to '" + lanAddr + "': Connection refused", false},
		{"cannot connect to " + lanAddr + ": No route to host (113)", false},
	} {
		adb, calls := fakeADBBinary(t, "echo \""+tc.out+"\"")
		err := adb.Connect(context.Background(), lanAddr)
		if (err == nil) != tc.ok {
			t.Errorf("output %q: err = %v, want ok=%v", tc.out, err, tc.ok)
		}
		if got := calledWith(t, calls); got[0] != "connect "+lanAddr {
			t.Errorf("ran %q", got)
		}
	}
}

func TestConnectRejectsNonAddresses(t *testing.T) {
	adb, calls := fakeADBBinary(t, "")
	for _, addr := range []string{"-x", "10.77.0.100", "host:5555", "10.77.0.100:0"} {
		if err := adb.Connect(context.Background(), addr); err == nil {
			t.Errorf("Connect(%q) accepted", addr)
		}
	}
	if _, err := os.Stat(calls); err == nil {
		t.Fatal("adb ran for an invalid address")
	}
}

// The serial comes from the phone itself (ro.serialno): `adb get-serialno`
// on a TCP transport answers with ip:port.
func TestSerialNoAsksThePhone(t *testing.T) {
	adb, calls := fakeADBBinary(t, `echo "R58M12345  "`)
	adb.SetTransport("OTHER", lanAddr) // a stale mapping mustn't redirect it
	serial, err := adb.SerialNo(context.Background(), lanAddr)
	if err != nil || serial != "R58M12345" {
		t.Fatalf("SerialNo = %q, %v", serial, err)
	}
	if got := calledWith(t, calls); got[0] != "-s "+lanAddr+" shell getprop ro.serialno" {
		t.Fatalf("ran %q", got)
	}
}

func TestSerialNoRejectsEmptyAndInvalidAnswers(t *testing.T) {
	for _, out := range []string{"", "-rf", "a b"} {
		adb, _ := fakeADBBinary(t, `printf '`+out+`'`)
		if s, err := adb.SerialNo(context.Background(), lanAddr); err == nil {
			t.Errorf("answer %q accepted as serial %q", out, s)
		}
	}
}

func TestTCPIPSwitchesTheUSBPhonesADBToTCP(t *testing.T) {
	adb, calls := fakeADBBinary(t, `echo "restarting in TCP mode port: 5555"`)
	if err := adb.TCPIP(context.Background(), "SER1", 5555); err != nil {
		t.Fatal(err)
	}
	if got := calledWith(t, calls); got[0] != "-s SER1 tcpip 5555" {
		t.Fatalf("ran %q", got)
	}
}
