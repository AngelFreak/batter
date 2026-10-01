package tether

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

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeRelay writes a script that records each start in a file, then runs body.
func fakeRelay(t *testing.T, body string) (path, starts string) {
	t.Helper()
	dir := t.TempDir()
	starts = filepath.Join(dir, "starts")
	path = filepath.Join(dir, "gnirehtet")
	script := "#!/bin/sh\necho \"$@\" >> " + starts + "\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, starts
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

func TestRelayRestartsWhenItExits(t *testing.T) {
	bin, starts := fakeRelay(t, "exit 1")
	r := &Relay{Path: bin, Logger: quiet, backoff: func(int) time.Duration { return 10 * time.Millisecond }}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for countLines(t, starts) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("relay started %d times, want it restarted repeatedly", countLines(t, starts))
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	b, _ := os.ReadFile(starts)
	if first := strings.SplitN(string(b), "\n", 2)[0]; first != "relay" {
		t.Fatalf("relay invoked with args %q, want \"relay\"", first)
	}
}

func TestRelayStopsOnCancel(t *testing.T) {
	bin, starts := fakeRelay(t, "exec sleep 30")
	r := &Relay{Path: bin, Logger: quiet}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	for countLines(t, starts) < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel (relay process not killed?)")
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	if relayBackoff(0) != time.Second || relayBackoff(1) != 2*time.Second {
		t.Fatalf("backoff(0..1) = %v, %v", relayBackoff(0), relayBackoff(1))
	}
	if relayBackoff(20) != 30*time.Second {
		t.Fatalf("backoff(20) = %v, want 30s cap", relayBackoff(20))
	}
}

func TestRelayListensOnItsPort(t *testing.T) {
	bin, starts := fakeRelay(t, "exit 0")
	r := &Relay{Path: bin, Port: 31417, Logger: quiet, backoff: func(int) time.Duration { return time.Hour }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for countLines(t, starts) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("relay not started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if b, _ := os.ReadFile(starts); strings.TrimSpace(string(b)) != "relay -p 31417" {
		t.Fatalf("started with %q, want relay -p 31417", b)
	}
}
