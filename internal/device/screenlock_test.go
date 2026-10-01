package device

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Trimmed from a real Samsung (Android 16) `dumpsys lock_settings`.
const lockSettingsDump = `Current lock settings service state:

DO Enabled: false

User State:
  User 0
    Quality: 0
    CredentialType: PIN
    SeparateChallenge: true

Keys in namespace:
  User 0 [/data/system_de/0/spblob]:
    CredentialType: NONE
`

func TestParseCredentialTypeReadsUserZero(t *testing.T) {
	if got := parseCredentialType(lockSettingsDump); got != "PIN" {
		t.Fatalf("parseCredentialType = %q, want PIN", got)
	}
	if got := parseCredentialType("garbage"); got != "" {
		t.Fatalf("parseCredentialType(garbage) = %q, want empty", got)
	}
}

// fakePhone returns a Manager whose adb emulates a device: `shell` args are
// joined and run by sh, like adbd does, against a fake locksettings/dumpsys
// backed by files. The phone's credential starts as pin ("" = none).
func fakePhone(t *testing.T, pin string) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pin", pin)
	write("disabled", "false")
	write("device.sh", `
pin=$(cat "$D/pin"); disabled=$(cat "$D/disabled")
dumpsys() { printf 'User State:\n  User 0\n    CredentialType: %s\n' "$( [ -n "$pin" ] && echo PIN || echo NONE)"; }
locksettings() {
  case "$1" in
    get-disabled) echo "$disabled" ;;
    set-disabled) echo "$2" > "$D/disabled"; echo "Lock screen disabled set to $2" ;;
    clear)
      if [ "$2" = "--old" ] && [ "$3" = "$pin" ]; then : > "$D/pin"; echo "Lock credential cleared"
      else echo "Old password '$3' didn't match"; exit 255; fi ;;
  esac
}
`)
	adb := filepath.Join(dir, "adb")
	write("adb", `#!/bin/sh
# adb -s SERIAL shell ARGS... -> the device's shell runs the joined ARGS
shift 2; [ "$1" = shell ] || exit 1; shift
D="`+dir+`" exec sh -c ". '`+dir+`/device.sh'; $*"
`)
	if err := os.Chmod(adb, 0o755); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Manager{adb: &ADB{adbPath: adb, logger: logger}, logger: logger}, dir
}

func TestRemoveScreenLockWithCorrectPIN(t *testing.T) {
	m, _ := fakePhone(t, "1234")
	ctx := context.Background()

	before, err := m.ScreenLock(ctx, "S1")
	if err != nil || before.Credential != "PIN" || before.Disabled {
		t.Fatalf("before = %+v, %v; want PIN, not disabled", before, err)
	}
	after, err := m.RemoveScreenLock(ctx, "S1", "1234")
	if err != nil {
		t.Fatal(err)
	}
	if after.Credential != "NONE" || !after.Disabled {
		t.Fatalf("after = %+v, want NONE and disabled", after)
	}
}

func TestRemoveScreenLockWrongPINIsReportedWithoutLeakingIt(t *testing.T) {
	m, dir := fakePhone(t, "1234")
	_, err := m.RemoveScreenLock(context.Background(), "S1", "9999")
	if !errors.Is(err, ErrWrongCredential) {
		t.Fatalf("err = %v, want ErrWrongCredential", err)
	}
	if strings.Contains(err.Error(), "9999") {
		t.Fatalf("error message leaks the attempted PIN: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "disabled")); strings.TrimSpace(string(b)) != "false" {
		t.Fatal("lock screen disabled despite the wrong PIN")
	}
}

func TestRemoveScreenLockNeedsCredentialWhenLocked(t *testing.T) {
	m, _ := fakePhone(t, "1234")
	if _, err := m.RemoveScreenLock(context.Background(), "S1", ""); !errors.Is(err, ErrCredentialRequired) {
		t.Fatalf("err = %v, want ErrCredentialRequired", err)
	}
}

func TestRemoveScreenLockPassesPasswordLiterallyToDeviceShell(t *testing.T) {
	// Spaces, quotes and shell metacharacters must reach locksettings as one
	// literal argument, not be interpreted by the device's shell.
	const password = `pa ss'w"o;rd$(id)`
	m, _ := fakePhone(t, password)
	after, err := m.RemoveScreenLock(context.Background(), "S1", password)
	if err != nil {
		t.Fatal(err)
	}
	if after.Credential != "NONE" {
		t.Fatalf("after = %+v", after)
	}
}

func TestRemoveScreenLockWithoutCredentialJustSkipsSwipe(t *testing.T) {
	m, _ := fakePhone(t, "")
	after, err := m.RemoveScreenLock(context.Background(), "S1", "")
	if err != nil {
		t.Fatal(err)
	}
	if after.Credential != "NONE" || !after.Disabled {
		t.Fatalf("after = %+v, want NONE and disabled", after)
	}
}
