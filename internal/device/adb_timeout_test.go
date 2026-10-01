package device

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeADBBinary writes an adb script that logs each invocation to a file
// and then runs body.
func fakeADBBinary(t *testing.T, body string) (adb *ADB, calls string) {
	t.Helper()
	dir := t.TempDir()
	calls = filepath.Join(dir, "calls")
	path := filepath.Join(dir, "adb")
	script := "#!/bin/sh\necho \"$@\" >> " + calls + "\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ADB{adbPath: path, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, calls
}

func invocations(t *testing.T, calls, prefix string) int {
	t.Helper()
	b, _ := os.ReadFile(calls)
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

func TestListDevicesSharedAcrossConcurrentCallers(t *testing.T) {
	adb, calls := fakeADBBinary(t, `sleep 0.2; printf 'List of devices attached\nS1 device product:p model:m\n'`)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			devs, err := adb.ListDevices(context.Background())
			if err != nil || len(devs) != 1 || devs[0].Serial != "S1" {
				t.Errorf("ListDevices = %v, %v", devs, err)
			}
		}()
	}
	wg.Wait()
	if n := invocations(t, calls, "devices"); n != 1 {
		t.Fatalf("%d `adb devices` runs for 20 concurrent callers, want 1", n)
	}
	// Still fresh: served from cache.
	if _, err := adb.ListDevices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := invocations(t, calls, "devices"); n != 1 {
		t.Fatalf("%d runs, want the cached listing reused", n)
	}
}

func TestListDevicesRefreshesAfterTTL(t *testing.T) {
	adb, calls := fakeADBBinary(t, `printf 'List of devices attached\nS1 device\n'`)
	adb.listTTL = 50 * time.Millisecond
	if _, err := adb.ListDevices(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := adb.ListDevices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := invocations(t, calls, "devices"); n != 2 {
		t.Fatalf("%d runs, want a fresh listing after the TTL", n)
	}
}

func TestCachedListingCantBeMutatedByCallers(t *testing.T) {
	adb, _ := fakeADBBinary(t, `printf 'List of devices attached\nS1 device\n'`)
	devs, _ := adb.ListDevices(context.Background())
	devs[0].Serial = "MUTATED"
	again, _ := adb.ListDevices(context.Background())
	if again[0].Serial != "S1" {
		t.Fatal("a caller's change leaked into the cache")
	}
}

func TestHungADBTimesOut(t *testing.T) {
	adb, _ := fakeADBBinary(t, `exec sleep 600`)
	adb.timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := adb.Shell(context.Background(), "S1", "getprop")
	if err == nil {
		t.Fatal("hung adb returned no error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("hung adb held the caller for %v", d)
	}
	start = time.Now()
	if _, err := adb.ShellSecret(context.Background(), "S1", "locksettings", "clear"); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("ShellSecret on hung adb: %v after %v", err, time.Since(start))
	}
}

func TestCallerDeadlineWins(t *testing.T) {
	adb, _ := fakeADBBinary(t, `sleep 0.5; echo ok`)
	adb.timeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := adb.Shell(ctx, "S1", "slow"); err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("caller's longer deadline not honoured: %q %v", out, err)
	}
}

func TestTransfersGetLongerTimeouts(t *testing.T) {
	adb := &ADB{}
	short := adb.timeoutFor([]string{"-s", "S1", "shell", "getprop"})
	for _, args := range [][]string{
		{"-s", "S1", "install", "-r", "x.apk"},
		{"-s", "S1", "push", "a", "b"},
	} {
		if got := adb.timeoutFor(args); got < 5*short {
			t.Errorf("%v timeout %v, want much longer than %v", args, got, short)
		}
	}
}

func TestServerShellHasNoDefaultTimeout(t *testing.T) {
	adb, _ := fakeADBBinary(t, `sleep 0.6; echo done`)
	adb.timeout = 100 * time.Millisecond
	out, err := adb.ServerShell(context.Background(), "S1", "app_process")
	if err != nil || strings.TrimSpace(string(out)) != "done" {
		t.Fatalf("long-running server cut off: %q %v", out, err)
	}
}
