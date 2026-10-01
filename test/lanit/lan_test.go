// Package lanit is a Docker integration test of the phone network: the real
// Batter image and the compose files as shipped (docker-compose.yml plus
// docker-compose.lan.yml: macvlan phone network, gw_priority, PHONE_LAN),
// with stand-in phones on that network and two throwaway WireGuard "VPN
// providers" out on the "internet".
//
// Topology. The stack runs in a docker-in-docker container, which plays
// the box (lungo): its uplink is this test's network, where the VPN
// servers, an IP echo server ("the internet") and another host ("the
// box's LAN behind NIC1") live. The phone network's macvlan parent is a
// dummy interface inside the box, standing in for NIC2: macvlan in bridge
// mode switches between its sub-interfaces as the real switch would, and
// the box itself has no address there (as NIC2 must not). Each phone is
// an alpine container on that network that drops Docker's address, gets
// one from Batter with busybox udhcpc, and listens on adb's port 5555.
//
// It proves, through the real firewall, routing, DHCP server and API:
//
//   - the container starts with forwarding off and Batter turns it on only
//     with the firewall in;
//   - phones get sticky leases from Batter (gateway and DNS = Batter);
//   - a phone is identified over adb and bound to its lease (adb is the
//     fake phone; its TCP side is the stand-in's port 5555);
//   - a phone without a profile reaches nothing; each phone with one exits
//     through its own profile's server, and its DNS answers come from that
//     server (a resolver only reachable through the tunnel);
//   - phones reach neither Batter (:8080/:3000, on any address), Postgres,
//     the box (its dockerd on :2375, on every address it has), nor the
//     box's LAN, nor the internet directly: the VPN servers forward only to
//     the echo server, so anything else a phone reaches is a leak. Each of
//     these checks has a control showing the target is up;
//   - Batter's own connections to a phone (adb's port) work;
//   - switching a phone's profile takes effect at once (exit IP and DNS);
//   - a tunnel going down, or the routing being wiped, blocks its phones
//     (and only them) instead of leaking;
//   - all of it holds after Batter restarts.
//
// It needs Docker with kernel WireGuard, macvlan and nf_tables, and runs
// only when asked:
//
//	BATTER_DOCKER_IT=1 go test ./test/lanit/ -v -timeout 60m
//
// BATTER_IMAGE=<tag> skips building the image from this checkout.
package lanit

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
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
	prefix    = "batter-lanit"
	network   = prefix + "-net"
	dind      = prefix + "-dind"
	echo      = prefix + "-echo"
	lanHost   = prefix + "-lanhost"
	wgA       = prefix + "-wg-a"
	wgB       = prefix + "-wg-b"
	batterLAN = "10.77.0.1"
	// What each VPN provider's resolver answers for probe.test.
	dnsA = "192.0.2.1"
	dnsB = "192.0.2.2"
)

func TestPhonesOnTheLAN(t *testing.T) {
	if os.Getenv("BATTER_DOCKER_IT") != "1" {
		t.Skip("set BATTER_DOCKER_IT=1 to run the Docker integration test")
	}
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	repo := filepath.Join(dir, "..", "..")

	image := os.Getenv("BATTER_IMAGE")
	if image == "" {
		image = "batter-lan-it:latest"
		t.Log("building the Batter image (slow the first time)")
		run(t, 60*time.Minute, "docker", "build", "-q", "-t", image, repo)
	}

	t.Cleanup(func() {
		if t.Failed() {
			out, _ := runErr(time.Minute, "docker", "exec", "-w", "/stack", "-e", "PHONE_LAN_PARENT=phonesw", dind,
				"docker", "compose", "-p", "batter", "-f", "docker-compose.yml", "-f", "docker-compose.lan.yml",
				"-f", "it.yml", "logs", "--no-color", "--tail", "120", "batter")
			t.Logf("batter logs:\n%s", out)
		}
		if os.Getenv("BATTER_IT_KEEP") == "1" {
			return
		}
		_, _ = runErr(2*time.Minute, "docker", "rm", "-f", "-v", dind, echo, lanHost, wgA, wgB)
		_, _ = runErr(time.Minute, "docker", "network", "rm", network)
	})
	run(t, time.Minute, "docker", "network", "create", network)

	// The internet: an IP echo server, two VPN providers that forward to it
	// only, and another host on the box's LAN.
	run(t, time.Minute, "docker", "run", "-d", "--name", echo, "--network", network, "--network-alias", "echo",
		"-v", filepath.Join(repo, "test", "vpnit", "echo-ip.cgi")+":/www/cgi-bin/ip:ro",
		"busybox:1.36-musl", "httpd", "-f", "-p", "80", "-h", "/www")
	run(t, time.Minute, "docker", "run", "-d", "--name", lanHost, "--network", network,
		"busybox:1.36-musl", "httpd", "-f", "-p", "80", "-h", "/tmp")
	serverKeyA, serverPubA := keypair(t)
	clientKeyA, clientPubA := keypair(t)
	serverKeyB, serverPubB := keypair(t)
	clientKeyB, clientPubB := keypair(t)
	for _, s := range []struct{ name, key, clientPub, subnet, answer string }{
		{wgA, serverKeyA, clientPubA, "10.99.0", dnsA},
		{wgB, serverKeyB, clientPubB, "10.98.0", dnsB},
	} {
		run(t, time.Minute, "docker", "run", "-d", "--privileged", "--name", s.name, "--network", network,
			"-e", "SERVER_KEY="+s.key, "-e", "CLIENT_PUB="+s.clientPub, "-e", "SUBNET="+s.subnet,
			"-e", "DNS_ANSWER="+s.answer, "-e", "ONLY_TO=echo",
			"-v", filepath.Join(repo, "test", "vpnit", "wgserver.sh")+":/wgserver.sh:ro",
			"alpine:3.20", "sh", "/wgserver.sh")
	}
	for _, name := range []string{wgA, wgB} {
		waitFor(t, 2*time.Minute, name+" ready", func() bool {
			_, err := runErr(10*time.Second, "docker", "exec", name, "test", "-f", "/ready")
			return err == nil
		})
	}
	echoIP := containerIP(t, echo)
	ipA, ipB := containerIP(t, wgA), containerIP(t, wgB)
	lanHostIP := containerIP(t, lanHost)

	// The box, with NIC2 (a dummy: no address, nothing else on it).
	run(t, time.Minute, "docker", "run", "-d", "--privileged", "--name", dind, "--network", network,
		"-e", "DOCKER_TLS_CERTDIR=", "docker:29-dind")
	waitFor(t, 2*time.Minute, "dind docker", func() bool {
		_, err := runErr(10*time.Second, "docker", "exec", dind, "docker", "info")
		return err == nil
	})
	run(t, time.Minute, "docker", "exec", dind, "sh", "-c", "ip link add phonesw type dummy && ip link set phonesw up")
	run(t, 20*time.Minute, "sh", "-c",
		"docker save "+image+" postgres:16-alpine alpine:3.20 | docker exec -i "+dind+" docker load")
	boxIP := containerIP(t, dind)

	stack := t.TempDir()
	copyFile(t, filepath.Join(repo, "docker-compose.yml"), filepath.Join(stack, "docker-compose.yml"))
	copyFile(t, filepath.Join(repo, "docker-compose.lan.yml"), filepath.Join(stack, "docker-compose.lan.yml"))
	for _, f := range []string{"phone.sh", "udhcpc.sh", "banner.sh"} {
		copyFile(t, filepath.Join(dir, f), filepath.Join(stack, "phone", f))
	}
	build := exec.Command("go", "build", "-o", filepath.Join(stack, "adb"), "./test/fakeadb")
	build.Dir, build.Env = repo, append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fakeadb: %v\n%s", err, out)
	}
	// As shipped, except: the prebuilt image, the API published for this
	// test, the fake phone instead of USB, and the echo server for the
	// exit-IP check.
	override := `services:
  batter:
    image: ` + image + `
    pull_policy: never
    build: !reset null
    ports: !override
      - "8080:8080"
    volumes: !override
      - batter_data:/app/data
      - ./adb:/usr/bin/adb:ro
    environment:
      VPN_EXIT_IP_URL: http://` + echoIP + `/cgi-bin/ip
`
	if err := os.WriteFile(filepath.Join(stack, "it.yml"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, time.Minute, "docker", "cp", stack+"/.", dind+":/stack")
	box := func(timeout time.Duration, args ...string) (string, error) {
		return runErr(timeout, "docker", append([]string{"exec", dind}, args...)...)
	}
	compose := func(timeout time.Duration, args ...string) (string, error) {
		return runErr(timeout, "docker", append([]string{"exec", "-w", "/stack", "-e", "PHONE_LAN_PARENT=phonesw", dind,
			"docker", "compose", "-p", "batter", "-f", "docker-compose.yml", "-f", "docker-compose.lan.yml", "-f", "it.yml"}, args...)...)
	}
	if out, err := compose(10*time.Minute, "up", "-d", "--wait", "batter"); err != nil {
		t.Fatalf("compose up: %v\n%s", err, out)
	}
	inBatter := func(args ...string) (string, error) {
		return compose(time.Minute, append([]string{"exec", "-T", "batter"}, args...)...)
	}
	inPhone := func(phone string, args ...string) (string, error) {
		return box(time.Minute, append([]string{"docker", "exec", phone}, args...)...)
	}

	api := &api{base: "http://" + boxIP + ":8080"}
	api.waitHealthy(t)
	api.login(t)

	// Phones on the phone network, as Docker attaches them.
	for _, p := range []string{"phone1", "phone2"} {
		if out, err := box(time.Minute, "docker", "run", "-d", "--name", p, "--network", "batter_phones",
			"--cap-add", "NET_ADMIN", "-v", "/stack/phone:/phone:ro", "alpine:3.20", "sh", "/phone/phone.sh"); err != nil {
			t.Fatalf("start %s: %v %s", p, err, out)
		}
	}

	// Probes.
	phoneTCP := func(phone, host string, port int) bool {
		_, err := inPhone(phone, "nc", "-z", "-w", "3", host, fmt.Sprint(port))
		return err == nil
	}
	batterTCP := func(host string, port int) bool {
		_, err := inBatter("node", "-e",
			`const s=require("net").connect(+process.argv[2],process.argv[1]);`+
				`s.setTimeout(3000,()=>process.exit(2));s.on("connect",()=>process.exit(0));s.on("error",()=>process.exit(1))`,
			host, fmt.Sprint(port))
		return err == nil
	}
	exitIP := func(phone string) (string, error) {
		out, err := inPhone(phone, "wget", "-qO-", "-T", "5", "http://"+echoIP+"/cgi-bin/ip")
		return strings.TrimSpace(out), err
	}
	resolve := func(phone string) (string, error) {
		// The test resolvers know only probe.test's A record; busybox
		// nslookup also asks for AAAA and exits 1 on that refusal.
		out, _ := inPhone(phone, "nslookup", "-timeout=3", "probe.test", batterLAN)
		for _, line := range strings.Split(out, "\n") {
			if a, ok := strings.CutPrefix(strings.TrimSpace(line), "Address:"); ok && strings.HasPrefix(strings.TrimSpace(a), "192.0.2.") {
				return strings.TrimSpace(a), nil
			}
		}
		return "", fmt.Errorf("no answer: %s", out)
	}
	mustExit := func(t *testing.T, phone, want, what string) {
		t.Helper()
		eventually(t, 20*time.Second, func() error {
			ip, err := exitIP(phone)
			if err != nil {
				return fmt.Errorf("%s (%s): no internet: %v", phone, what, err)
			}
			if ip != want {
				return fmt.Errorf("%s (%s) exits from %s, want %s", phone, what, ip, want)
			}
			return nil
		})
	}
	mustResolve := func(t *testing.T, phone, want, what string) {
		t.Helper()
		eventually(t, 20*time.Second, func() error {
			got, err := resolve(phone)
			if err != nil {
				return fmt.Errorf("%s (%s): DNS: %v", phone, what, err)
			}
			if got != want {
				return fmt.Errorf("%s (%s) resolved probe.test to %s, want %s (its profile's resolver)", phone, what, got, want)
			}
			return nil
		})
	}
	mustBeOffline := func(t *testing.T, phone, why string) {
		t.Helper()
		if ip, err := exitIP(phone); err == nil {
			t.Fatalf("%s reached the internet (from %s) with %s: leak", phone, ip, why)
		}
		if a, err := resolve(phone); err == nil {
			t.Fatalf("%s resolved a name (%s) with %s", phone, a, why)
		}
	}

	var phone1IP, phone2IP string
	step(t, "forwarding starts off and Batter turns it on once fenced", func(t *testing.T) {
		out, err := box(time.Minute, "docker", "inspect", "-f", "{{index .HostConfig.Sysctls \"net.ipv4.ip_forward\"}}", "batter-batter-1")
		if err != nil || strings.TrimSpace(out) != "0" {
			t.Fatalf("container not started with forwarding off: %q %v", out, err)
		}
		if out, _ := inBatter("sysctl", "-n", "net.ipv4.ip_forward"); strings.TrimSpace(out) != "1" {
			t.Fatalf("Batter didn't turn forwarding on: %q", out)
		}
		if out, err := inBatter("nft", "list", "chain", "inet", "batter_lan", "forward"); err != nil || !strings.Contains(out, "drop") {
			t.Fatalf("no LAN firewall: %v %s", err, out)
		}
	})

	step(t, "phones get sticky leases from Batter", func(t *testing.T) {
		pool := netip.MustParsePrefix("10.77.0.0/24")
		for _, p := range []struct {
			name string
			ip   *string
		}{{"phone1", &phone1IP}, {"phone2", &phone2IP}} {
			eventually(t, time.Minute, func() error {
				out, err := inPhone(p.name, "cat", "/tmp/ip", "/tmp/router", "/tmp/dns")
				if err != nil {
					return fmt.Errorf("%s has no lease: %v %s", p.name, err, out)
				}
				f := strings.Fields(out)
				ip, err := netip.ParseAddr(f[0])
				if err != nil || !pool.Contains(ip) || ip.Less(netip.MustParseAddr("10.77.0.100")) ||
					f[1] != batterLAN || f[2] != batterLAN {
					return fmt.Errorf("%s lease: %q (want a pool address, gateway and DNS %s)", p.name, out, batterLAN)
				}
				*p.ip = f[0]
				return nil
			})
		}
		if phone1IP == phone2IP {
			t.Fatalf("both phones got %s", phone1IP)
		}
		t.Logf("phone1 %s, phone2 %s; VPN servers A %s, B %s; box %s", phone1IP, phone2IP, ipA, ipB, boxIP)
	})

	step(t, "no profile: nothing reachable", func(t *testing.T) {
		mustBeOffline(t, "phone1", "no profile")
		mustBeOffline(t, "phone2", "no profile")
		if phoneTCP("phone1", echoIP, 80) || phoneTCP("phone1", lanHostIP, 80) {
			t.Fatal("phone without a profile reached the internet")
		}
	})

	step(t, "phones are identified over adb and bound to their leases", func(t *testing.T) {
		// The fake phone moves from USB to phone1's adapter.
		if out, err := inBatter("sh", "-c", "mkdir -p /tmp/fakeadb && echo "+phone1IP+":5555 > /tmp/fakeadb/lan && touch /tmp/fakeadb/unplugged"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		api.must(t, "POST", "/api/v1/devices", map[string]string{"serial": "FAKE01"}, http.StatusCreated)
		eventually(t, 30*time.Second, func() error {
			_, dev := api.do(t, "GET", "/api/v1/devices/FAKE01", nil)
			if !strings.Contains(dev, `"connection":"lan"`) || !strings.Contains(dev, `"lan_address":"`+phone1IP+`"`) {
				return fmt.Errorf("FAKE01 not found on the LAN: %s", dev)
			}
			return nil
		})
		// The second stand-in has no adb; bind its lease as discovery would.
		api.must(t, "POST", "/api/v1/devices", map[string]string{"serial": "ITPHONE2"}, http.StatusCreated)
		if out, err := compose(time.Minute, "exec", "-T", "postgres", "psql", "-U", "batter", "-v", "ON_ERROR_STOP=1", "-c",
			"UPDATE lan_leases SET serial = 'ITPHONE2' WHERE ip = '"+phone2IP+"'"); err != nil || !strings.Contains(out, "UPDATE 1") {
			t.Fatalf("bind phone2: %v %s", err, out)
		}
	})

	confA := clientConf(clientKeyA, serverPubA, "10.99.0", ipA)
	confB := clientConf(clientKeyB, serverPubB, "10.98.0", ipB)
	var profileA, profileB string
	step(t, "each phone exits through its own profile, DNS included", func(t *testing.T) {
		profileA = api.createProfile(t, "A", confA)
		profileB = api.createProfile(t, "B", confB)
		api.assign(t, "FAKE01", profileA)
		api.assign(t, "ITPHONE2", profileB)
		mustExit(t, "phone1", ipA, "profile A")
		mustExit(t, "phone2", ipB, "profile B")
		mustResolve(t, "phone1", dnsA, "profile A")
		mustResolve(t, "phone2", dnsB, "profile B")
		if got := api.exitIP(t, profileA); got != ipA {
			t.Fatalf("API exit check for A: %s, want %s", got, ipA)
		}
	})

	fields := func(out string, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return strings.Fields(out)
	}
	pgIP := fields(box(time.Minute, "docker", "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", "batter-postgres-1"))[0]
	batterIPs := fields(inBatter("sh", "-c", "ip -4 -o addr show scope global | awk '{print $4}' | cut -d/ -f1"))
	// The box's own addresses: the gateways of its Docker networks and its
	// uplink address. Its dockerd listens on all of them (:2375).
	gateways := fields(box(time.Minute, "docker", "network", "inspect", "-f",
		"{{range .IPAM.Config}}{{.Gateway}} {{end}}", "batter_default", "bridge"))
	confined := func(t *testing.T, phone string) {
		t.Helper()
		type target struct {
			what string
			host string
			port int
		}
		targets := []target{
			{"Batter's backend on the LAN", batterLAN, 8080},
			{"Batter's web app on the LAN", batterLAN, 3000},
			{"Postgres", pgIP, 5432},
			{"the box's dockerd", boxIP, 2375},
			{"a host on the box's LAN", lanHostIP, 80},
		}
		for _, ip := range batterIPs {
			if ip != batterLAN {
				targets = append(targets, target{"Batter's backend", ip, 8080}, target{"Batter's web app", ip, 3000})
			}
		}
		for _, gw := range gateways {
			targets = append(targets, target{"the box's dockerd (Docker gateway)", gw, 2375})
		}
		for _, tg := range targets {
			if !batterTCP(tg.host, tg.port) {
				t.Errorf("control: %s (%s:%d) isn't up, so this check proves nothing", tg.what, tg.host, tg.port)
				continue
			}
			if phoneTCP(phone, tg.host, tg.port) {
				t.Errorf("%s reached %s (%s:%d)", phone, tg.what, tg.host, tg.port)
			}
		}
	}

	step(t, "phones reach neither Batter, Postgres, the box, its LAN nor the internet directly", func(t *testing.T) {
		confined(t, "phone1")
		confined(t, "phone2")
		mustExit(t, "phone1", ipA, "profile A")
	})

	step(t, "Batter's own connections to a phone work (adb)", func(t *testing.T) {
		out, err := inBatter("node", "-e",
			`const s=require("net").connect(5555,process.argv[1]);s.setTimeout(3000,()=>process.exit(2));`+
				`s.on("data",d=>{process.stdout.write(d);process.exit(0)});s.on("error",()=>process.exit(1))`, phone1IP)
		if err != nil || strings.TrimSpace(out) != "PHONE-OK" {
			t.Fatalf("Batter -> phone1:5555: %q %v", out, err)
		}
	})

	step(t, "switching a phone's profile takes effect at once", func(t *testing.T) {
		api.assign(t, "FAKE01", profileB)
		mustExit(t, "phone1", ipB, "switched to B")
		mustResolve(t, "phone1", dnsB, "switched to B")
		api.assign(t, "FAKE01", profileA)
		mustExit(t, "phone1", ipA, "back on A")
		mustResolve(t, "phone1", dnsA, "back on A")
	})

	step(t, "taking a phone's profile away takes its internet", func(t *testing.T) {
		api.assign(t, "ITPHONE2", "")
		eventually(t, 10*time.Second, func() error {
			if ip, err := exitIP("phone2"); err == nil {
				return fmt.Errorf("unassigned phone2 still exits from %s", ip)
			}
			return nil
		})
		mustBeOffline(t, "phone2", "its profile taken away")
		api.assign(t, "ITPHONE2", profileB)
		mustExit(t, "phone2", ipB, "profile B again")
	})

	step(t, "a tunnel going down blocks only its phones", func(t *testing.T) {
		if out, err := inBatter("ip", "link", "set", "wg0", "down"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		mustBeOffline(t, "phone1", "its tunnel down")
		if phoneTCP("phone1", lanHostIP, 80) || phoneTCP("phone1", pgIP, 5432) {
			t.Fatal("phone1 reached something with its tunnel down")
		}
		mustExit(t, "phone2", ipB, "profile B")
		// Re-applying the profile brings its tunnel (and routes) back.
		api.must(t, "PUT", "/api/v1/vpn/profiles/"+profileA, map[string]any{"enabled": false}, http.StatusOK)
		api.must(t, "PUT", "/api/v1/vpn/profiles/"+profileA, map[string]any{"enabled": true}, http.StatusOK)
		mustExit(t, "phone1", ipA, "tunnel back up")
	})

	step(t, "wiped routing blocks instead of leaking", func(t *testing.T) {
		for _, cmd := range [][]string{
			{"ip", "rule", "del", "from", phone1IP, "lookup", "51820"},
			{"ip", "route", "flush", "table", "51820"},
			{"ip", "rule", "del", "priority", "9900"},
		} {
			if out, err := inBatter(cmd...); err != nil {
				t.Fatalf("%v: %v %s", cmd, err, out)
			}
		}
		mustBeOffline(t, "phone1", "its routing wiped")
		for _, dst := range []struct {
			host string
			port int
		}{{echoIP, 80}, {lanHostIP, 80}, {pgIP, 5432}, {boxIP, 2375}} {
			if phoneTCP("phone1", dst.host, dst.port) {
				t.Errorf("phone1 reached %s:%d with its routing wiped", dst.host, dst.port)
			}
		}
		mustExit(t, "phone2", ipB, "profile B")
	})

	step(t, "all of it holds after Batter restarts", func(t *testing.T) {
		if out, err := compose(5*time.Minute, "restart", "batter"); err != nil {
			t.Fatalf("restart: %v\n%s", err, out)
		}
		api.waitHealthy(t)
		mustExit(t, "phone1", ipA, "profile A after restart")
		mustExit(t, "phone2", ipB, "profile B after restart")
		mustResolve(t, "phone1", dnsA, "profile A after restart")
		mustResolve(t, "phone2", dnsB, "profile B after restart")
		confined(t, "phone1")
		confined(t, "phone2")
		eventually(t, 30*time.Second, func() error {
			_, dev := api.do(t, "GET", "/api/v1/devices/FAKE01", nil)
			if !strings.Contains(dev, `"lan_address":"`+phone1IP+`"`) {
				return fmt.Errorf("FAKE01 not back on the LAN: %s", dev)
			}
			return nil
		})
		// Leases are sticky: a phone asking again gets the same address.
		if out, err := inPhone("phone1", "sh", "-c", "kill -USR1 $(pidof udhcpc) && sleep 3 && cat /tmp/ip"); err != nil ||
			strings.TrimSpace(out) != phone1IP {
			t.Fatalf("phone1 renewed to %q (%v), want %s", out, err, phone1IP)
		}
	})
}

// step runs a subtest and stops the test if it fails: later steps build
// on earlier ones.
func step(t *testing.T, name string, f func(t *testing.T)) {
	t.Helper()
	if !t.Run(name, f) {
		t.FailNow()
	}
}

func clientConf(key, serverPub, subnet, endpoint string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s.2/32
DNS = %s.1

[Peer]
PublicKey = %s
Endpoint = %s:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 5
`, key, subnet, subnet, serverPub, endpoint)
}

type api struct {
	base  string
	token string
}

func (a *api) waitHealthy(t *testing.T) {
	t.Helper()
	waitFor(t, 3*time.Minute, "Batter healthy", func() bool {
		resp, err := http.Get(a.base + "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

func (a *api) login(t *testing.T) {
	t.Helper()
	creds := map[string]string{"username": "admin", "password": "lan-it-password"}
	a.must(t, "POST", "/api/v1/admin/setup", creds, http.StatusCreated)
	body := a.must(t, "POST", "/api/v1/auth/login", creds, http.StatusOK)
	var login struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal([]byte(body), &login) != nil || login.AccessToken == "" {
		t.Fatalf("login: %s", body)
	}
	a.token = login.AccessToken
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
	resp, err := (&http.Client{Timeout: time.Minute}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (a *api) must(t *testing.T, method, path string, body any, want int) string {
	t.Helper()
	code, out := a.do(t, method, path, body)
	if code != want {
		t.Fatalf("%s %s: %d %s (want %d)", method, path, code, out, want)
	}
	return out
}

func (a *api) createProfile(t *testing.T, name, config string) string {
	t.Helper()
	body := a.must(t, "POST", "/api/v1/vpn/profiles", map[string]any{"name": name, "config": config}, http.StatusCreated)
	var p struct {
		ID         string `json:"id"`
		ApplyError string `json:"apply_error"`
	}
	if json.Unmarshal([]byte(body), &p) != nil || p.ID == "" || p.ApplyError != "" {
		t.Fatalf("create profile %s: %s", name, body)
	}
	return p.ID
}

// assign sets serial's profile ("" = none); applying must succeed.
func (a *api) assign(t *testing.T, serial, profile string) {
	t.Helper()
	var id any
	if profile != "" {
		id = profile
	}
	body := a.must(t, "PUT", "/api/v1/devices/"+serial+"/tether", map[string]any{"profile_id": id}, http.StatusOK)
	if strings.Contains(body, "apply_error") {
		t.Fatalf("assign %s: %s", serial, body)
	}
}

func (a *api) exitIP(t *testing.T, id string) string {
	t.Helper()
	body := a.must(t, "POST", "/api/v1/vpn/profiles/"+id+"/check", nil, http.StatusOK)
	var res struct {
		ExitIP string `json:"exit_ip"`
		Error  string `json:"error"`
	}
	if json.Unmarshal([]byte(body), &res) != nil || res.Error != "" {
		t.Fatalf("exit check: %s", body)
	}
	return res.ExitIP
}

func containerIP(t *testing.T, name string) string {
	t.Helper()
	return strings.TrimSpace(run(t, time.Minute, "docker", "inspect", "-f",
		"{{(index .NetworkSettings.Networks \""+network+"\").IPAddress}}", name))
}

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

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(from)
	if err := os.WriteFile(to, b, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

// eventually retries check until it passes or timeout, then fails with its
// last error.
func eventually(t *testing.T, timeout time.Duration, check func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
	}
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
	return string(out), err
}
