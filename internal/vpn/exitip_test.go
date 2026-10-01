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
	ip, err := ExitIPChecker(exe, "https://localhost:8443/ip", 0)(context.Background())
	if err != nil || ip != "198.51.100.4" {
		t.Fatalf("got %q, %v", ip, err)
	}
	if b, _ := os.ReadFile(args); strings.TrimSpace(string(b)) != "vpn-exit-ip https://localhost:8443/ip --connect 127.0.0.1" {
		t.Fatalf("ran with %q", b)
	}
}

func TestExitIPCheckerReportsFailure(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "batter")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho 'connect: network is unreachable' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ExitIPChecker(exe, "https://198.51.100.1/", 0)(context.Background())
	if err == nil || !strings.Contains(err.Error(), "network is unreachable") {
		t.Fatalf("err = %v, want the subcommand's message", err)
	}
}

// The checker uid may not reach local addresses (Docker's DNS resolver among
// them), so the check's hostname is resolved beforehand and the child
// connects to that address, keeping the hostname for HTTP and TLS.
func TestExitIPCommandConnectsToPreResolvedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "ip.invalid:"+strings.Split(r.Host, ":")[1] {
			http.Error(w, "wrong host "+r.Host, http.StatusBadRequest)
			return
		}
		w.Write([]byte("203.0.113.9\n"))
	}))
	defer srv.Close()
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	var out, errOut bytes.Buffer
	code := ExitIPCommand([]string{"http://ip.invalid:" + port + "/", "--connect", "127.0.0.1"}, &out, &errOut)
	if code != 0 || out.String() != "203.0.113.9\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out.String(), errOut.String())
	}
}
