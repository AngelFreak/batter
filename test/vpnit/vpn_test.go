// Package vpnit is a Docker-based integration test of the tethering VPN: the
// real Batter image, configured through its API, against a throwaway
// WireGuard server. It proves the routing claims unit tests can't:
//
//   - the relay uid's traffic exits through the tunnel (exit IP is the VPN
//     server's), while other traffic (root, other uids) stays direct;
//   - the kill switch: with the tunnel down or gone, the relay uid has no
//     route out at all rather than leaking directly;
//   - the config survives a restart and is re-applied on startup;
//   - without a VPN (or with it disabled) the relay uid goes direct.
//
// It needs Docker with kernel WireGuard and runs only when asked:
//
//	BATTER_DOCKER_IT=1 go test ./test/vpnit/ -v -timeout 30m
//
// BATTER_IMAGE=<tag> skips building the image from this checkout.
package vpnit

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
)

const (
	project  = "batter-vpn-it"
	relayUID = "31416" // tether.RelayUID
	echoURL  = "http://echo/cgi-bin/ip"
)

func TestTetheringVPN(t *testing.T) {
	if os.Getenv("BATTER_DOCKER_IT") != "1" {
		t.Skip("set BATTER_DOCKER_IT=1 to run the Docker integration test")
	}
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	repo := filepath.Join(dir, "..", "..")

	image := os.Getenv("BATTER_IMAGE")
	if image == "" {
		image = "batter-vpn-it:latest"
		t.Log("building the Batter image (slow the first time)")
		run(t, 60*time.Minute, "docker", "build", "-q", "-t", image, repo)
	}

	serverKey, serverPub := keypair(t)
	clientKey, clientPub := keypair(t)
	env := append(os.Environ(), "SERVER_KEY="+serverKey, "CLIENT_PUB="+clientPub, "BATTER_IMAGE="+image)
	compose := func(timeout time.Duration, args ...string) (string, error) {
		args = append([]string{"compose", "-p", project, "-f", filepath.Join(dir, "compose.yml")}, args...)
		return runEnv(timeout, env, "docker", args...)
	}
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := compose(time.Minute, "logs", "--no-color", "--tail", "80", "batter", "wgserver")
			t.Logf("container logs:\n%s", logs)
		}
		if out, err := compose(2*time.Minute, "down", "-v", "--remove-orphans"); err != nil {
			t.Logf("compose down: %v\n%s", err, out)
		}
	})
	if out, err := compose(10*time.Minute, "up", "-d", "--wait"); err != nil {
		t.Fatalf("compose up: %v\n%s", err, out)
	}

	inBatter := func(user string, args ...string) (string, error) {
		a := []string{"exec", "-T"}
		if user != "" {
			a = append(a, "-u", user)
		}
		return compose(time.Minute, append(append(a, "batter"), args...)...)
	}
	// exitIP makes the same request tethered traffic would, as uid user.
	exitIP := func(user string) (string, error) {
		out, err := inBatter(user, "/app/batter", "vpn-exit-ip", echoURL)
		return strings.TrimSpace(out), err
	}
	mustExitIP := func(t *testing.T, user string) string {
		t.Helper()
		ip, err := exitIP(user)
		if err != nil {
			t.Fatalf("exit IP as uid %q: %v (%s)", user, err, ip)
		}
		return ip
	}
	mustBeBlocked := func(t *testing.T, user string) {
		t.Helper()
		if ip, err := exitIP(user); err == nil {
			t.Fatalf("uid %s reached the internet (from %s) with the tunnel down: leak", user, ip)
		}
	}

	wgIP := containerIP(t, compose, "wgserver")
	batterIP := containerIP(t, compose, "batter")
	t.Logf("VPN server %s, Batter %s", wgIP, batterIP)

	api := newAPI(t, compose)

	t.Run("relay runs as its own uid", func(t *testing.T) {
		out, err := inBatter("", "sh", "-c", `for s in /proc/[0-9]*/status; do grep -q '^Name:.*gnirehtet' $s && grep '^Uid:' $s; done; true`)
		if err != nil || !strings.Contains(out, relayUID) {
			t.Fatalf("gnirehtet relay not running as uid %s: %q %v", relayUID, out, err)
		}
	})

	t.Run("no VPN: tethered traffic goes direct", func(t *testing.T) {
		if got := api.exitIP(t); got != batterIP {
			t.Fatalf("exit IP %q, want Batter's own %s", got, batterIP)
		}
	})

	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.99.0.2/32
DNS = 10.99.0.1

[Peer]
PublicKey = %s
Endpoint = wgserver:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 5
`, clientKey, serverPub)

	t.Run("enable: relay uid exits via the tunnel, nothing else does", func(t *testing.T) {
		info := api.put(t, map[string]any{"config": conf, "enabled": true})
		if info.ApplyError != "" {
			t.Fatalf("apply error: %s", info.ApplyError)
		}
		if got := api.exitIP(t); got != wgIP {
			t.Fatalf("API exit IP %q, want the VPN server's %s", got, wgIP)
		}
		if got := mustExitIP(t, relayUID); got != wgIP {
			t.Fatalf("relay uid exits from %q, want %s", got, wgIP)
		}
		if got := mustExitIP(t, "root"); got != batterIP {
			t.Fatalf("root exits from %q, want direct %s", got, batterIP)
		}
		if got := mustExitIP(t, "1000"); got != batterIP {
			t.Fatalf("uid 1000 exits from %q, want direct %s", got, batterIP)
		}
	})

	t.Run("status reports the handshake and never the private key", func(t *testing.T) {
		raw := api.get(t)
		if strings.Contains(raw, clientKey) {
			t.Fatal("GET /vpn returned the private key")
		}
		var info vpnInfo
		if err := json.Unmarshal([]byte(raw), &info); err != nil {
			t.Fatal(err)
		}
		if info.Status == nil || !info.Status.Up || len(info.Status.Peers) != 1 ||
			info.Status.Peers[0].LatestHandshake == "" || info.Status.Peers[0].RxBytes == 0 {
			t.Fatalf("status: %s", raw)
		}
		if mode, _ := inBatter("", "stat", "-c", "%a", "/app/data/wireguard.json"); strings.TrimSpace(mode) != "600" {
			t.Fatalf("config file mode %q, want 600", mode)
		}
	})

	t.Run("kill switch: tunnel down blocks the relay uid only", func(t *testing.T) {
		if out, err := inBatter("", "ip", "link", "set", "wg0", "down"); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		mustBeBlocked(t, relayUID)
		if got := api.exitIPErr(t); got == "" {
			t.Fatal("API check succeeded with the tunnel down")
		}
		if got := mustExitIP(t, "root"); got != batterIP {
			t.Fatalf("root exits from %q with the tunnel down, want direct %s", got, batterIP)
		}

		if out, err := inBatter("", "ip", "link", "del", "wg0"); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		mustBeBlocked(t, relayUID)
	})

	t.Run("re-applying restores the tunnel", func(t *testing.T) {
		if info := api.put(t, map[string]any{"enabled": true}); info.ApplyError != "" {
			t.Fatalf("apply error: %s", info.ApplyError)
		}
		if got := mustExitIP(t, relayUID); got != wgIP {
			t.Fatalf("relay uid exits from %q, want %s", got, wgIP)
		}
	})

	t.Run("config is re-applied on restart", func(t *testing.T) {
		// Wipe the tunnel and routing first so only Batter's startup can
		// bring them back, whether or not the restart keeps the netns.
		for _, cmd := range [][]string{
			{"ip", "link", "del", "wg0"},
			{"ip", "rule", "del", "uidrange", relayUID + "-" + relayUID},
			{"ip", "route", "flush", "table", "51820"},
		} {
			if out, err := inBatter("", cmd...); err != nil {
				t.Fatalf("%v: %v %s", cmd, err, out)
			}
		}
		if got := mustExitIP(t, relayUID); got != batterIP {
			t.Fatalf("wipe didn't take: relay uid exits from %q", got)
		}
		if out, err := compose(5*time.Minute, "restart", "batter"); err != nil {
			t.Fatalf("restart: %v\n%s", err, out)
		}
		api.waitHealthy(t)
		if got := mustExitIP(t, relayUID); got != wgIP {
			t.Fatalf("after restart the relay uid exits from %q, want %s", got, wgIP)
		}
	})

	t.Run("disabled: tethered traffic goes direct again", func(t *testing.T) {
		if info := api.put(t, map[string]any{"enabled": false}); info.ApplyError != "" {
			t.Fatalf("apply error: %s", info.ApplyError)
		}
		if got := mustExitIP(t, relayUID); got != batterIP {
			t.Fatalf("relay uid exits from %q with the VPN off, want direct %s", got, batterIP)
		}
		if out, _ := inBatter("", "ip", "rule", "show"); strings.Contains(out, "uidrange") {
			t.Fatalf("policy rule left behind:\n%s", out)
		}
	})

	t.Run("kill switch: dead VPN server blocks the relay uid", func(t *testing.T) {
		if info := api.put(t, map[string]any{"enabled": true}); info.ApplyError != "" {
			t.Fatalf("apply error: %s", info.ApplyError)
		}
		if got := mustExitIP(t, relayUID); got != wgIP {
			t.Fatalf("relay uid exits from %q, want %s", got, wgIP)
		}
		if out, err := compose(2*time.Minute, "stop", "wgserver"); err != nil {
			t.Fatalf("stop wgserver: %v\n%s", err, out)
		}
		mustBeBlocked(t, relayUID)
		if got := mustExitIP(t, "root"); got != batterIP {
			t.Fatalf("root exits from %q, want direct %s", got, batterIP)
		}
	})
}

type vpnInfo struct {
	Configured bool   `json:"configured"`
	Enabled    bool   `json:"enabled"`
	ApplyError string `json:"apply_error"`
	Status     *struct {
		Up    bool `json:"up"`
		Peers []struct {
			LatestHandshake string `json:"latest_handshake"`
			RxBytes         int64  `json:"rx_bytes"`
		} `json:"peers"`
	} `json:"status"`
}

type api struct {
	compose func(time.Duration, ...string) (string, error)
	base    string
	token   string
}

func newAPI(t *testing.T, compose func(time.Duration, ...string) (string, error)) *api {
	t.Helper()
	a := &api{compose: compose}
	a.waitHealthy(t)

	creds := map[string]string{"username": "admin", "password": "vpn-it-password"}
	if code, body := a.do(t, "POST", "/api/v1/admin/setup", creds); code != http.StatusCreated {
		t.Fatalf("setup: %d %s", code, body)
	}
	code, body := a.do(t, "POST", "/api/v1/auth/login", creds)
	var login struct {
		AccessToken string `json:"access_token"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &login) != nil || login.AccessToken == "" {
		t.Fatalf("login: %d %s", code, body)
	}
	a.token = login.AccessToken
	return a
}

// waitHealthy waits for the API, looking up its host port afresh: it
// changes when the container restarts.
func (a *api) waitHealthy(t *testing.T) {
	t.Helper()
	out, err := a.compose(time.Minute, "port", "batter", "8080")
	if err != nil {
		t.Fatalf("compose port: %v\n%s", err, out)
	}
	a.base = "http://" + strings.TrimSpace(out)
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(a.base + "/health"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatal("Batter didn't become healthy")
}

func (a *api) do(t *testing.T, method, path string, body any) (int, string) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.base+path, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (a *api) get(t *testing.T) string {
	t.Helper()
	code, body := a.do(t, "GET", "/api/v1/vpn", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /vpn: %d %s", code, body)
	}
	return body
}

func (a *api) put(t *testing.T, req map[string]any) vpnInfo {
	t.Helper()
	code, body := a.do(t, "PUT", "/api/v1/vpn", req)
	if code != http.StatusOK {
		t.Fatalf("PUT /vpn: %d %s", code, body)
	}
	var info vpnInfo
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		t.Fatal(err)
	}
	return info
}

func (a *api) check(t *testing.T) (ip, errMsg string) {
	t.Helper()
	code, body := a.do(t, "POST", "/api/v1/vpn/check", nil)
	var res struct {
		ExitIP string `json:"exit_ip"`
		Error  string `json:"error"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &res) != nil {
		t.Fatalf("POST /vpn/check: %d %s", code, body)
	}
	return res.ExitIP, res.Error
}

func (a *api) exitIP(t *testing.T) string {
	t.Helper()
	ip, errMsg := a.check(t)
	if errMsg != "" {
		t.Fatalf("exit IP check failed: %s", errMsg)
	}
	return ip
}

func (a *api) exitIPErr(t *testing.T) string {
	t.Helper()
	ip, errMsg := a.check(t)
	if errMsg == "" {
		t.Logf("exit IP check succeeded: %s", ip)
	}
	return errMsg
}

func containerIP(t *testing.T, compose func(time.Duration, ...string) (string, error), service string) string {
	t.Helper()
	id, err := compose(time.Minute, "ps", "-q", service)
	if err != nil || strings.TrimSpace(id) == "" {
		t.Fatalf("container id for %s: %v %s", service, err, id)
	}
	out := run(t, time.Minute, "docker", "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", strings.TrimSpace(id))
	return strings.TrimSpace(out)
}

// keypair returns a WireGuard private key and its public key.
func keypair(t *testing.T) (priv, pub string) {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	p, err := curve25519.X25519(k, curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k), base64.StdEncoding.EncodeToString(p)
}

func run(t *testing.T, timeout time.Duration, name string, args ...string) string {
	t.Helper()
	out, err := runEnv(timeout, nil, name, args...)
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

func runEnv(timeout time.Duration, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
