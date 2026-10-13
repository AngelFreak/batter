// Package proxyit tests the real compose stack's HTTP path: caddy (host
// networking, HTTPS) routing /api and /ws straight to the Go backend. The
// stack runs inside a docker-in-docker container, so caddy gets that
// container's ports 80/443 (not this machine's) and the topology matches
// production: caddy on the "host", batter's API published on its
// 127.0.0.1. The phone is test/fakeadb (no USB).
//
// It proves, with the Next.js server killed so nothing can go through its
// proxy, that an API call, a slow upload taking over 30s and a WebSocket
// video stream all work through caddy; that the same-origin check still
// works; and that Go sees (and rate-limits on) the real client IP.
//
// Runs only when asked (needs Docker and the docker:29-dind image):
//
//	BATTER_DOCKER_IT=1 go test ./test/proxyit/ -v -timeout 60m
//
// BATTER_IMAGE=<tag> skips building the image from this checkout.
package proxyit

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	ws "github.com/gorilla/websocket"
)

const (
	network = "batter-proxyit-net"
	dind    = "batter-proxyit-dind"
)

func TestCaddyRoutesAPIAndWebSocketsStraightToGo(t *testing.T) {
	if os.Getenv("BATTER_DOCKER_IT") != "1" {
		t.Skip("set BATTER_DOCKER_IT=1 to run the Docker integration test")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Join(filepath.Dir(file), "..", "..")

	image := os.Getenv("BATTER_IMAGE")
	if image == "" {
		image = "batter-proxy-it:latest"
		t.Log("building the Batter image (slow the first time)")
		run(t, 60*time.Minute, "docker", "build", "-q", "-t", image, repo)
	}

	// The compose files as shipped, plus an override that swaps USB for the
	// simulated phone and uses the prebuilt image.
	stack := t.TempDir()
	copyFile(t, filepath.Join(repo, "docker-compose.yml"), filepath.Join(stack, "docker-compose.yml"))
	if err := os.MkdirAll(filepath.Join(stack, "caddy"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(repo, "caddy", "Caddyfile"), filepath.Join(stack, "caddy", "Caddyfile"))
	build := exec.Command("go", "build", "-o", filepath.Join(stack, "adb"), "./test/fakeadb")
	build.Dir, build.Env = repo, append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fakeadb: %v\n%s", err, out)
	}
	override := `services:
  batter:
    image: ` + image + `
    pull_policy: never
    build: !reset null
    volumes: !override
      - batter_data:/app/data
      - ./adb:/usr/bin/adb:ro
`
	if err := os.WriteFile(filepath.Join(stack, "it.yml"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if t.Failed() {
			out, _ := runErr(time.Minute, "docker", "exec", "-w", "/stack", dind, "docker", "compose", "-p", "batter",
				"-f", "docker-compose.yml", "-f", "it.yml", "logs", "--no-color", "--tail", "60")
			t.Logf("stack logs:\n%s", out)
		}
		if os.Getenv("BATTER_IT_KEEP") == "1" {
			return // leave the stack up for debugging
		}
		_, _ = runErr(2*time.Minute, "docker", "rm", "-f", "-v", dind)
		_, _ = runErr(time.Minute, "docker", "network", "rm", network)
	})
	run(t, time.Minute, "docker", "network", "create", network)
	run(t, time.Minute, "docker", "run", "-d", "--privileged", "--name", dind, "--network", network,
		"-e", "DOCKER_TLS_CERTDIR=", "docker:29-dind")
	waitFor(t, 2*time.Minute, "dind docker", func() bool {
		_, err := runErr(10*time.Second, "docker", "exec", dind, "docker", "info")
		return err == nil
	})
	run(t, 20*time.Minute, "sh", "-c",
		"docker save "+image+" postgres:16-alpine caddy:2.11-alpine | docker exec -i "+dind+" docker load")
	run(t, time.Minute, "docker", "cp", stack+"/.", dind+":/stack")
	compose := func(timeout time.Duration, args ...string) string {
		return run(t, timeout, "docker", append([]string{"exec", "-w", "/stack", dind,
			"docker", "compose", "-p", "batter", "-f", "docker-compose.yml", "-f", "it.yml"}, args...)...)
	}
	compose(10*time.Minute, "up", "-d", "--wait")

	host := strings.TrimSpace(run(t, time.Minute, "docker", "inspect", "-f",
		"{{(index .NetworkSettings.Networks \""+network+"\").IPAddress}}", dind))
	// This test talks to dind from the network's gateway address.
	clientIP := strings.TrimSpace(run(t, time.Minute, "docker", "network", "inspect", "-f",
		"{{(index .IPAM.Config 0).Gateway}}", network))
	base := "https://" + host
	c := &client{base: base, http: &http.Client{
		Timeout:   5 * time.Minute,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}}
	waitFor(t, 2*time.Minute, "API through caddy", func() bool {
		code, _ := c.try("GET", "/api/v1/auth/needs-setup", nil)
		return code == http.StatusOK
	})

	// From here on nothing can go through Next.js's proxy.
	compose(time.Minute, "exec", "-T", "batter", "pkill", "-f", "next-server")
	waitFor(t, 30*time.Second, "Next.js to stop", func() bool {
		code, _ := c.try("GET", "/", nil)
		return code == http.StatusBadGateway
	})

	t.Run("API calls go straight to Go", func(t *testing.T) {
		creds := map[string]string{"username": "admin", "password": "proxy-it-password"}
		c.must(t, "POST", "/api/v1/admin/setup", creds, http.StatusCreated)
		var login struct {
			AccessToken string `json:"access_token"`
		}
		json.Unmarshal([]byte(c.must(t, "POST", "/api/v1/auth/login", creds, http.StatusOK)), &login)
		c.token = login.AccessToken
		c.must(t, "GET", "/api/v1/auth/me", nil, http.StatusOK)
	})

	t.Run("Go sees the real client IP", func(t *testing.T) {
		c.must(t, "POST", "/api/v1/devices", map[string]string{"serial": "FAKE01"}, http.StatusCreated)
		got := strings.TrimSpace(compose(time.Minute, "exec", "-T", "postgres", "psql", "-U", "batter", "-tAc",
			"SELECT details->>'client_ip' FROM audit_log ORDER BY id DESC LIMIT 1"))
		if got != clientIP {
			t.Fatalf("audit log recorded client IP %q, want the real client %s", got, clientIP)
		}
	})

	t.Run("rate limit keys on the real client IP", func(t *testing.T) {
		bad := map[string]string{"username": "admin", "password": "wrong"}
		limited := false
		for range 12 {
			if code, _ := c.try("POST", "/api/v1/auth/login", bad); code == http.StatusTooManyRequests {
				limited = true
				break
			}
		}
		if !limited {
			t.Fatal("login never rate-limited")
		}
		// Another client on the same network isn't affected.
		out := run(t, 2*time.Minute, "docker", "run", "--rm", "--network", network,
			"-e", "NODE_TLS_REJECT_UNAUTHORIZED=0", "--entrypoint", "node", image, "-e",
			`fetch("`+base+`/api/v1/auth/login",{method:"POST",headers:{"content-type":"application/json"},`+
				`body:JSON.stringify({username:"admin",password:"wrong"})}).then(r=>console.log(r.status))`)
		if !strings.Contains(out, "401") {
			t.Fatalf("other client got %q, want 401 (its own rate-limit bucket)", out)
		}
	})

	t.Run("WebSocket video streams through caddy", func(t *testing.T) {
		c.must(t, "POST", "/api/v1/devices/FAKE01/session/start", nil, http.StatusOK)
		dialer := ws.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, HandshakeTimeout: 10 * time.Second}
		url := "wss://" + host + "/ws/device/FAKE01/video?token=" + c.token

		// A browser on this origin: caddy keeps Host, so same-origin holds.
		conn, resp, err := dialer.Dial(url, http.Header{"Origin": {base}})
		if err != nil {
			t.Fatalf("dial: %v (%v)", err, resp)
		}
		defer conn.Close()
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("status %d, want 101", resp.StatusCode)
		}
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("no keyframe over the WebSocket: %v", err)
			}
			// Keyframe flag (bit 61), not the config packet (bit 62)
			// that a new viewer is sent first.
			if len(msg) >= 12 && msg[0]&0x20 != 0 && msg[0]&0x40 == 0 {
				break
			}
		}

		// A page on another site is still refused.
		if _, resp, err := dialer.Dial(url, http.Header{"Origin": {"https://evil.example"}}); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin WebSocket not refused: %v %v", err, resp)
		}
	})

	t.Run("slow upload over 30s isn't cut off", func(t *testing.T) {
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		go func() {
			part, _ := mw.CreateFormFile("file", "slow.bin")
			chunk := bytes.Repeat([]byte("x"), 1024)
			for range 36 { // ~36s of trickle
				if _, err := part.Write(chunk); err != nil {
					pw.CloseWithError(err)
					return
				}
				time.Sleep(time.Second)
			}
			mw.Close()
			pw.Close()
		}()
		start := time.Now()
		req, _ := http.NewRequest("POST", base+"/api/v1/devices/FAKE01/push", pr)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err := c.http.Do(req)
		if err != nil {
			t.Fatalf("upload: %v after %v", err, time.Since(start))
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "file pushed") {
			t.Fatalf("upload after %v: %d %s", time.Since(start), resp.StatusCode, body)
		}
		if d := time.Since(start); d < 30*time.Second {
			t.Fatalf("upload took only %v; the test needs over 30s", d)
		}
	})
}

type client struct {
	base, token string
	http        *http.Client
}

func (c *client) try(method, path string, body any) (int, string) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (c *client) must(t *testing.T, method, path string, body any, want int) string {
	t.Helper()
	code, out := c.try(method, path, body)
	if code != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, code, out, want)
	}
	return out
}

func waitFor(t *testing.T, timeout time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Second)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, timeout time.Duration, name string, args ...string) string {
	t.Helper()
	out, err := runErr(timeout, name, args...)
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

func runErr(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("timed out after %v", timeout)
	}
	return string(out), err
}
