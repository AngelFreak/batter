package vpn

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExitIPCommandPrintsTheReportedIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("203.0.113.9\n"))
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	if code := ExitIPCommand([]string{srv.URL}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.String() != "203.0.113.9\n" {
		t.Fatalf("printed %q", out.String())
	}
}

func TestExitIPCommandRejectsNonIPAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>captive portal</html>"))
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	if code := ExitIPCommand([]string{srv.URL}, &out, &errOut); code == 0 {
		t.Fatalf("accepted %q", out.String())
	}
}

func TestExitIPCheckerRunsTheSubcommand(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "batter")
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" > " + args + "\necho 198.51.100.4\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ip, err := ExitIPChecker(exe, "https://ip.example", 0)(context.Background())
	if err != nil || ip != "198.51.100.4" {
		t.Fatalf("got %q, %v", ip, err)
	}
	if b, _ := os.ReadFile(args); strings.TrimSpace(string(b)) != "vpn-exit-ip https://ip.example" {
		t.Fatalf("ran with %q", b)
	}
}

func TestExitIPCheckerReportsFailure(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "batter")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho 'connect: network is unreachable' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ExitIPChecker(exe, "https://ip.example", 0)(context.Background())
	if err == nil || !strings.Contains(err.Error(), "network is unreachable") {
		t.Fatalf("err = %v, want the subcommand's message", err)
	}
}
