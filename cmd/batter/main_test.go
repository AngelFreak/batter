package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPServerTimeouts(t *testing.T) {
	srv := newHTTPServer(http.NotFoundHandler())

	if srv.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout unset: slow-header clients can hold connections forever")
	}
	if srv.IdleTimeout <= 0 {
		t.Error("IdleTimeout unset: idle keep-alive connections are never reaped")
	}
	// Whole-request timeouts would cut off large APK uploads and long-lived
	// WebSocket streams, so they must stay off.
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Errorf("ReadTimeout=%v WriteTimeout=%v, want 0", srv.ReadTimeout, srv.WriteTimeout)
	}
}

func TestServeDrainsInFlightRequestOnShutdown(t *testing.T) {
	entered := make(chan struct{})
	srv := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		time.Sleep(200 * time.Millisecond)
		io.WriteString(w, "done")
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, srv, ln, 5*time.Second) }()

	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			got <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		got <- result{string(b), err}
	}()

	<-entered
	cancel() // what SIGTERM does in main

	if r := <-got; r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request: body=%q err=%v, want it to complete", r.body, r.err)
	}
	if err := <-served; err != nil {
		t.Fatalf("serve returned %v, want nil after clean shutdown", err)
	}
	if _, err := http.Get("http://" + ln.Addr().String()); err == nil {
		t.Fatal("server still accepting connections after shutdown")
	}
}
