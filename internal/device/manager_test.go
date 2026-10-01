package device

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newFakeADBManager returns a Manager whose adb is a script that simulates one
// hung device: any command naming serial "SLOW" appends a line to the returned
// marker file and then blocks for 3s; every other command fails immediately,
// like adb does for an absent device.
func newFakeADBManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "slow-calls")
	script := filepath.Join(dir, "adb")
	err := os.WriteFile(script, []byte(`#!/bin/sh
for a in "$@"; do
  if [ "$a" = SLOW ]; then echo call >> "`+marker+`"; exec sleep 3; fi
done
echo "error: device not found" >&2
exit 1
`), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := &Manager{
		adb:              &ADB{adbPath: script, logger: logger},
		sessions:         make(map[string]*Session),
		sessionTiers:     make(map[string]SessionTier),
		fullViewers:      make(map[string]int),
		scrcpyServerPath: filepath.Join(dir, "scrcpy-server"),
		scrcpyVersion:    "test",
		logger:           logger,
	}
	return m, marker
}

func slowCalls(t *testing.T, marker string) int {
	t.Helper()
	b, err := os.ReadFile(marker)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

func waitForSlowCalls(t *testing.T, marker string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for slowCalls(t, marker) < n {
		if time.Now().After(deadline) {
			t.Fatalf("fake adb never saw %d call(s) for SLOW", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// within fails the test if fn doesn't return within d.
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s blocked for over %v behind the hung device", what, d)
	}
}

func TestHungDeviceDoesNotBlockOtherDevices(t *testing.T) {
	m, marker := newFakeADBManager(t)
	go m.StartSession(context.Background(), "SLOW", SessionOptions{}) //nolint:errcheck
	waitForSlowCalls(t, marker, 1)

	within(t, 200*time.Millisecond, "GetSession", func() { m.GetSession("OTHER") })
	within(t, 200*time.Millisecond, "ActiveSessionCount", func() { m.ActiveSessionCount() })
	within(t, 200*time.Millisecond, "GetSessionTier", func() { m.GetSessionTier("OTHER") })
	within(t, 200*time.Millisecond, "health sweep", m.cleanupDeadSessions)
	within(t, time.Second, "StartSession on another device", func() {
		if _, err := m.StartSession(context.Background(), "FAST", SessionOptions{}); err == nil {
			t.Error("expected FAST to fail (fake adb has no such device)")
		}
	})
	within(t, time.Second, "StopSession on another device", func() { _ = m.StopSession("FAST") })
}

func TestSessionOpsOnSameDeviceAreSerialized(t *testing.T) {
	m, marker := newFakeADBManager(t)
	go m.StartSession(context.Background(), "SLOW", SessionOptions{}) //nolint:errcheck
	waitForSlowCalls(t, marker, 1)

	// A second start for the same device must wait for the first rather than
	// race it on the device (two scrcpy servers, clobbered reverse tunnels).
	go m.StartSession(context.Background(), "SLOW", SessionOptions{}) //nolint:errcheck
	time.Sleep(300 * time.Millisecond)
	if n := slowCalls(t, marker); n != 1 {
		t.Fatalf("%d concurrent adb calls for SLOW, want 1 (second start should wait)", n)
	}
}
