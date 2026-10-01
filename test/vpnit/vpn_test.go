// Package vpnit is a Docker-based integration test of VPN profiles: the
// real Batter image, configured through its API, against two throwaway
// WireGuard servers, without a phone network (test/lanit covers phones on
// one). It proves the routing claims unit tests can't:
//
//   - each profile's tunnel and table work: its exit-IP checker uid's
//     traffic exits through that profile's server; other traffic (root,
//     other uids) stays direct;
//   - checker uids can't reach anything local to the container (Batter's
//     backend and web app, loopback) or, without a tunnel, anything at all;
//   - the kill switch: a profile whose tunnel is down, that is disabled, or
//     whose server is gone gets no route out at all rather than leaking
//     directly, and other profiles are unaffected;
//   - deleting a profile unassigns its phones and removes its routing;
//   - without a phone network Batter changes nothing for phones (no LAN
//     firewall, routing or forwarding) and says so when one is assigned;
//   - profiles survive a restart and are re-applied on startup, and the
//     single-tunnel version's config is imported as profile "Default".
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
	echoURL = "http://echo/cgi-bin/ip"
	// Exit-IP checker uids of the first two profile slots (31416 + slot).
	uidA = "31416"
	uidB = "31417"
)

func TestVPNProfiles(t *testing.T) {
	if os.Getenv("BATTER_DOCKER_IT") != "1" {
		t.Skip("set BATTER_DOCKER_IT=1 to run the Docker integration test")
	}
	// A per-run compose project: concurrent runs never share resources, and
	// `compose down` removes only this run's.
	project := fmt.Sprintf("batter-vpn-it-%d", time.Now().UnixNano()%1_000_000_000)
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	repo := filepath.Join(dir, "..", "..")

	image := os.Getenv("BATTER_IMAGE")
	if image == "" {
		image = "batter-vpn-it:latest"
		t.Log("building the Batter image (slow the first time)")
		run(t, 60*time.Minute, "docker", "build", "-q", "-t", image, repo)
	}

	serverKeyA, serverPubA := keypair(t)
	clientKeyA, clientPubA := keypair(t)
	serverKeyB, serverPubB := keypair(t)
	clientKeyB, clientPubB := keypair(t)
	env := append(os.Environ(), "BATTER_IMAGE="+image,
		"SERVER_KEY_A="+serverKeyA, "CLIENT_PUB_A="+clientPubA,
		"SERVER_KEY_B="+serverKeyB, "CLIENT_PUB_B="+clientPubB)
	compose := func(timeout time.Duration, args ...string) (string, error) {
		args = append([]string{"compose", "-p", project, "-f", filepath.Join(dir, "compose.yml")}, args...)
		return runEnv(timeout, env, "docker", args...)
	}
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := compose(time.Minute, "logs", "--no-color", "--tail", "80", "batter")
			t.Logf("batter logs:\n%s", logs)
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
	var echoIP string // set once the stack is up
	// exitIP makes the exit-IP check's request as uid user.
	exitIP := func(user string) (string, error) {
		// Checker uids can't use Docker's DNS (it's local to the container),
		// so connect to the echo server by address, as Batter's own check
		// does after resolving.
		out, err := inBatter(user, "/app/batter", "vpn-exit-ip", echoURL, "--connect", echoIP)
		return strings.TrimSpace(out), err
	}
	mustExit := func(t *testing.T, user, want, what string) {
		t.Helper()
		ip, err := exitIP(user)
		if err != nil {
			t.Fatalf("uid %s (%s): no internet: %v (%s)", user, what, err, ip)
		}
		if ip != want {
			t.Fatalf("uid %s (%s) exits from %s, want %s", user, what, ip, want)
		}
	}
	mustBeBlocked := func(t *testing.T, user, why string) {
		t.Helper()
		if ip, err := exitIP(user); err == nil {
			t.Fatalf("uid %s reached the internet (from %s) with %s: leak", user, ip, why)
		}
	}
	wipe := func(t *testing.T, uid, iface, table string) {
		t.Helper()
		for _, cmd := range [][]string{
			{"ip", "link", "del", iface},
			{"ip", "rule", "del", "uidrange", uid + "-" + uid},
			{"ip", "route", "flush", "table", table},
		} {
			if out, err := inBatter("", cmd...); err != nil {
				t.Fatalf("%v: %v %s", cmd, err, out)
			}
		}
	}

	// tcpOK reports whether user can open a TCP connection to host:port.
	tcpOK := func(user, host string, port int) bool {
		_, err := inBatter(user, "node", "-e",
			`const s=require("net").connect(+process.argv[2],process.argv[1]);`+
				`s.setTimeout(3000,()=>process.exit(2));s.on("connect",()=>process.exit(0));s.on("error",()=>process.exit(1))`,
			host, fmt.Sprint(port))
		return err == nil
	}
	// mustBeConfined checks a checker uid reaches nothing local to the
	// container (no tunnel goes there), while root still does.
	mustBeConfined := func(t *testing.T, uid, batterIP string) {
		t.Helper()
		for _, dst := range []struct {
			host string
			port int
		}{{batterIP, 8080}, {batterIP, 3000}, {"127.0.0.1", 8080}, {"127.0.0.1", 3000}} {
			if tcpOK(uid, dst.host, dst.port) {
				t.Errorf("checker uid %s reached %s:%d without a tunnel", uid, dst.host, dst.port)
			}
		}
		if !tcpOK("root", batterIP, 8080) || !tcpOK("root", "127.0.0.1", 8080) {
			t.Error("other traffic to the backend broken")
		}
	}

	echoIP = containerIP(t, compose, "echo")
	ipA := containerIP(t, compose, "wgserver-a")
	ipB := containerIP(t, compose, "wgserver-b")
	batterIP := containerIP(t, compose, "batter")
	t.Logf("VPN servers A %s, B %s; Batter %s", ipA, ipB, batterIP)

	api := newAPI(t, compose)

	confA := clientConf(clientKeyA, serverPubA, "10.99.0", "wgserver-a")
	confB := clientConf(clientKeyB, serverPubB, "10.98.0", "wgserver-b")
	var profileA, profileB string

	t.Run("no phone network: Batter leaves phones' networking alone", func(t *testing.T) {
		if out, err := inBatter("", "nft", "list", "table", "inet", "batter_lan"); err == nil {
			t.Fatalf("phone LAN firewall installed without a phone network:\n%s", out)
		}
		if out, _ := inBatter("", "ip", "rule", "show"); strings.Contains(out, "9900:") || strings.Contains(out, "9000:") {
			t.Fatalf("phone LAN routing installed without a phone network:\n%s", out)
		}
	})

	t.Run("two profiles: each checker uid exits via its own server", func(t *testing.T) {
		a := api.createProfile(t, "A", confA)
		b := api.createProfile(t, "B", confB)
		profileA, profileB = a.ID, b.ID
		if a.ApplyError != "" || b.ApplyError != "" {
			t.Fatalf("apply errors: %q %q", a.ApplyError, b.ApplyError)
		}
		mustExit(t, uidA, ipA, "profile A")
		mustExit(t, uidB, ipB, "profile B")
		mustExit(t, "root", batterIP, "not a checker")
		mustExit(t, "1000", batterIP, "not a checker")
		if got := api.exitIP(t, profileA); got != ipA {
			t.Fatalf("API check for A: %q, want %s", got, ipA)
		}
		if got := api.exitIP(t, profileB); got != ipB {
			t.Fatalf("API check for B: %q, want %s", got, ipB)
		}
	})

	t.Run("status reports handshakes and never private keys", func(t *testing.T) {
		code, raw := api.do(t, "GET", "/api/v1/vpn/profiles", nil)
		if code != http.StatusOK {
			t.Fatalf("list: %d %s", code, raw)
		}
		if strings.Contains(raw, clientKeyA) || strings.Contains(raw, clientKeyB) {
			t.Fatal("profile list returned a private key")
		}
		var list struct {
			Profiles []profileInfo `json:"profiles"`
		}
		if err := json.Unmarshal([]byte(raw), &list); err != nil || len(list.Profiles) != 2 {
			t.Fatalf("list: %v %s", err, raw)
		}
		for _, p := range list.Profiles {
			if p.Status == nil || !p.Status.Up || len(p.Status.Peers) != 1 ||
				p.Status.Peers[0].LatestHandshake == "" || p.Status.Peers[0].RxBytes == 0 {
				t.Fatalf("profile %s status: %s", p.Name, raw)
			}
		}
	})

	t.Run("checker uids can't reach the container's own services", func(t *testing.T) {
		mustBeConfined(t, uidA, batterIP)
		mustBeConfined(t, uidB, batterIP)
		mustExit(t, uidA, ipA, "profile A")
	})

	t.Run("kill switch: A's tunnel down blocks only A", func(t *testing.T) {
		if out, err := inBatter("", "ip", "link", "set", "wg0", "down"); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		mustBeBlocked(t, uidA, "A's tunnel down")
		if _, errMsg := api.check(t, profileA); errMsg == "" {
			t.Fatal("API check for A succeeded with its tunnel down")
		}
		mustExit(t, uidB, ipB, "profile B")
		mustExit(t, "root", batterIP, "not a checker")
	})

	t.Run("disabled profile is blocked; re-enabling restores it", func(t *testing.T) {
		api.updateProfile(t, profileA, map[string]any{"enabled": false})
		mustBeBlocked(t, uidA, "A disabled")
		// With no tunnel the checker uid reaches nothing at all: not the
		// container, not postgres, not the host, not the "internet".
		pg := containerIP(t, compose, "postgres")
		gw := strings.TrimSpace(run(t, time.Minute, "docker", "network", "inspect", "-f",
			"{{(index .IPAM.Config 0).Gateway}}", project+"_default"))
		for _, dst := range []struct {
			host string
			port int
		}{{batterIP, 8080}, {"127.0.0.1", 8080}, {pg, 5432}, {gw, 80}, {containerIP(t, compose, "echo"), 80}} {
			if tcpOK(uidA, dst.host, dst.port) {
				t.Errorf("disabled profile's uid reached %s:%d", dst.host, dst.port)
			}
		}
		if p := api.updateProfile(t, profileA, map[string]any{"enabled": true}); p.ApplyError != "" {
			t.Fatalf("apply error: %s", p.ApplyError)
		}
		mustExit(t, uidA, ipA, "profile A re-enabled")
	})

	t.Run("kill switch: B's server gone blocks only B", func(t *testing.T) {
		if out, err := compose(2*time.Minute, "stop", "wgserver-b"); err != nil {
			t.Fatalf("stop wgserver-b: %v\n%s", err, out)
		}
		mustBeBlocked(t, uidB, "B's server stopped")
		mustExit(t, uidA, ipA, "profile A")
		mustExit(t, "root", batterIP, "not a checker")
	})

	t.Run("deleting a profile unassigns its phones and removes its routing", func(t *testing.T) {
		if code, body := api.do(t, "POST", "/api/v1/devices", map[string]string{"serial": "ITPHONE"}); code != http.StatusCreated {
			t.Fatalf("register device: %d %s", code, body)
		}
		// The phone network is off, so applying fails; the assignment is
		// stored and the reason reported.
		code, body := api.do(t, "PUT", "/api/v1/devices/ITPHONE/tether", map[string]any{"profile_id": profileB})
		if code != http.StatusOK || !strings.Contains(body, "phone network is off") {
			t.Fatalf("assign: %d %s", code, body)
		}
		code, body = api.do(t, "DELETE", "/api/v1/vpn/profiles/"+profileB, nil)
		if code != http.StatusOK || !strings.Contains(body, "ITPHONE") {
			t.Fatalf("delete B: %d %s (want ITPHONE turned off)", code, body)
		}
		_, dev := api.do(t, "GET", "/api/v1/devices/ITPHONE", nil)
		if strings.Contains(dev, "vpn_profile_id") {
			t.Fatalf("ITPHONE still has a profile: %s", dev)
		}
		if out, _ := inBatter("", "ip", "rule", "show"); strings.Contains(out, "uidrange "+uidB) {
			t.Fatalf("B's policy rule left behind:\n%s", out)
		}
		mustExit(t, uidA, ipA, "profile A")
	})

	t.Run("restart re-applies profiles and imports the old single-tunnel config", func(t *testing.T) {
		// Wipe A's tunnel and routing so only Batter's startup can bring them
		// back, whether or not the restart keeps the netns.
		wipe(t, uidA, "wg0", "51820")
		// Even with its routing gone, the firewall keeps the checker uid off
		// the direct route.
		mustBeBlocked(t, uidA, "its routing wiped")
		// The old config is B's (no profile uses B's keys any more).
		if out, err := compose(2*time.Minute, "start", "wgserver-b"); err != nil {
			t.Fatalf("start wgserver-b: %v\n%s", err, out)
		}
		ipB := containerIP(t, compose, "wgserver-b")
		legacy, _ := json.Marshal(map[string]any{"enabled": true, "config": confB})
		if out, err := inBatter("", "sh", "-c", "umask 077; cat > /app/data/wireguard.json <<'EOF'\n"+string(legacy)+"\nEOF"); err != nil {
			t.Fatalf("write legacy config: %v %s", err, out)
		}
		if out, err := compose(5*time.Minute, "restart", "batter"); err != nil {
			t.Fatalf("restart: %v\n%s", err, out)
		}
		api.waitHealthy(t)
		mustExit(t, uidA, ipA, "profile A after restart")
		// "Default" took the free slot 1, so its checker is uid 31417.
		mustExit(t, uidB, ipB, "imported Default profile")
		if out, err := inBatter("", "test", "-e", "/app/data/wireguard.json"); err == nil {
			t.Fatalf("legacy config not removed after import: %s", out)
		}
		mustBeConfined(t, uidA, batterIP)
		mustBeConfined(t, uidB, batterIP)
		_, names := api.do(t, "GET", "/api/v1/vpn/profile-names", nil)
		if !strings.Contains(names, `"name":"Default"`) || !strings.Contains(names, `"name":"A"`) {
			t.Fatalf("profiles after restart: %s", names)
		}
	})
}

func clientConf(key, serverPub, subnet, server string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s.2/32
DNS = %s.1

[Peer]
PublicKey = %s
Endpoint = %s:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 5
`, key, subnet, subnet, serverPub, server)
}

type profileInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
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

func (a *api) createProfile(t *testing.T, name, config string) profileInfo {
	t.Helper()
	code, body := a.do(t, "POST", "/api/v1/vpn/profiles", map[string]any{"name": name, "config": config})
	var p profileInfo
	if code != http.StatusCreated || json.Unmarshal([]byte(body), &p) != nil {
		t.Fatalf("create profile %s: %d %s", name, code, body)
	}
	return p
}

func (a *api) updateProfile(t *testing.T, id string, changes map[string]any) profileInfo {
	t.Helper()
	code, body := a.do(t, "PUT", "/api/v1/vpn/profiles/"+id, changes)
	var p profileInfo
	if code != http.StatusOK || json.Unmarshal([]byte(body), &p) != nil {
		t.Fatalf("update profile: %d %s", code, body)
	}
	return p
}

func (a *api) check(t *testing.T, id string) (ip, errMsg string) {
	t.Helper()
	code, body := a.do(t, "POST", "/api/v1/vpn/profiles/"+id+"/check", nil)
	var res struct {
		ExitIP string `json:"exit_ip"`
		Error  string `json:"error"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &res) != nil {
		t.Fatalf("check: %d %s", code, body)
	}
	return res.ExitIP, res.Error
}

func (a *api) exitIP(t *testing.T, id string) string {
	t.Helper()
	ip, errMsg := a.check(t, id)
	if errMsg != "" {
		t.Fatalf("exit IP check failed: %s", errMsg)
	}
	return ip
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
