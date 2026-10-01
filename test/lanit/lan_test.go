// Package lanit is a Docker integration test of the phone network: the real
// Batter image with the compose file as shipped (pid: host), stand-in phones
// on a stand-in switch, and two throwaway WireGuard "VPN providers" out on
// the "internet". The phone network's port is chosen through the API, as an
// admin does in the UI.
//
// Topology. The stack runs in a docker-in-docker container that plays the
// box (lungo): its uplink is this test's network, where the VPN servers, an
// IP echo server ("the internet") and another host ("the box's LAN behind
// NIC1") live. Inside the box a bridge is the phones' switch. The box's
// candidate phone-network ports (nic2, nic3) are veth pairs whose other end
// is a switch port, so the box reaches the switch exactly as a NIC plugged
// into it would: phones on the switch can reach the box's NIC (its IPv6
// link-local address, broadcasts, multicast) unless Batter takes it. Each
// phone is a container whose eth0 is a veth end on the switch; it gets an
// address from Batter with busybox udhcpc and listens on adb's port 5555.
// The box runs stand-ins for sshd and Caddy (:22, :443 on :: and 0.0.0.0)
// and logs UDP broadcast/multicast it receives. Veths stand in for
// physical NICs (BATTER_LAN_ALLOW_VETH=1 lets Batter offer them); unlike a
// physical NIC a veth is destroyed, not returned to the box, when Batter's
// container stops, so a restart is followed by a re-plug here.
//
// It proves, through the real HostNet/nsenter path, firewall, routing, DHCP
// server and API:
//
//   - off is a no-op: the box's links, addresses, rules, routes, nft and
//     sysctls don't change, and phones get nothing;
//   - the box-reachability probes work: with nic2 up on the box (as the old
//     macvlan design left it, or a box that manages the port), phones reach
//     the box's :22 over link-local IPv6 and the box hears their broadcast
//     and multicast;
//   - unusable ports (the box's own uplink, a port with addresses) are
//     refused;
//   - on then off restores the box exactly (normalized snapshot);
//   - with the port taken, nothing on the switch reaches the box on any
//     protocol, Batter has no IPv6 on it, and the container's forwarding was
//     off until then;
//   - phones get sticky leases with Batter as gateway and DNS; the switch's
//     own management interface gets a lease but nothing else and is listed
//     as a non-phone client, never as a device;
//   - adb identifies a phone and binds its lease; each phone exits (and
//     resolves) through its own profile; no phone reaches Batter, Postgres,
//     the box's dockerd, the box's LAN or the internet directly (each target
//     has a control showing it's up); Batter's connections to a phone's
//     5555 work; profile switches and removals apply at once;
//   - switching the port to nic3 gives nic2 back to the box down and phones
//     keep working; a re-plugged port (same MAC) is taken again;
//   - a downed tunnel or wiped routing blocks only that phone;
//   - after a restart Batter takes the port again and all checks hold;
//   - turning the network off gives the port back down and leaves no table,
//     rule or forwarding in Batter.
//
// It needs Docker with kernel WireGuard and nf_tables, and runs only when
// asked:
//
//	BATTER_DOCKER_IT=1 go test ./test/lanit/ -v -timeout 60m
//
// BATTER_IMAGE=<tag> skips building the image from this checkout. Every
// Docker resource it creates carries a per-run suffix and only those are
// removed afterwards.
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
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
)

const (
	batterLAN = "10.77.0.1"
	// What each VPN provider's resolver answers for probe.test.
	dnsA = "192.0.2.1"
	dnsB = "192.0.2.2"
	// The box's candidate phone-network ports.
	mac2 = "02:00:00:00:77:02"
	mac3 = "02:00:00:00:77:03"
	mac4 = "02:00:00:00:77:04"
)

// The box's setup: tools, IPv6 on (Docker turns it off in containers; a
// real box has it), the switch, three candidate ports (nic4 addressed, so
// unusable), and stand-ins for the box's services.
const boxSetup = `set -e
apk add -q --no-cache iproute2 socat >/dev/null
sysctl -qw net.ipv6.conf.all.disable_ipv6=0 net.ipv6.conf.default.disable_ipv6=0
ip link add phonesw type bridge
sysctl -qw net.ipv6.conf.phonesw.disable_ipv6=1
ip link set phonesw up
plug() { ip link add "$1" address "$2" type veth peer name "sw-$1"; ip link set "sw-$1" master phonesw; sysctl -qw "net.ipv6.conf.sw-$1.disable_ipv6=1"; ip link set "sw-$1" up; }
plug nic2 ` + mac2 + `
plug nic3 ` + mac3 + `
plug nic4 ` + mac4 + `
ip address add 192.168.99.1/24 dev nic4
for p in 22 443; do socat TCP6-LISTEN:$p,fork,reuseaddr,ipv6only=0 SYSTEM:'echo BOX' & done
socat -u UDP4-RECVFROM:9999,broadcast,fork,reuseaddr OPEN:/tmp/box-heard,creat,append &
socat -u UDP6-RECVFROM:9999,fork,reuseaddr,ipv6only=1 OPEN:/tmp/box-heard,creat,append &
sleep 1
`

// replug re-creates a port's veth (same MAC), as re-plugging a USB NIC.
func replugCmd(name, mac string) string {
	return fmt.Sprintf(`ip link del sw-%[1]s 2>/dev/null; ip link add %[1]s address %[2]s type veth peer name sw-%[1]s && `+
		`ip link set sw-%[1]s master phonesw && sysctl -qw net.ipv6.conf.sw-%[1]s.disable_ipv6=1 && ip link set sw-%[1]s up`, name, mac)
}

// The box's networking as compared before and after: interface indexes and
// peer references are stripped (a returned NIC keeps its index, but veth
// peer annotations depend on where the peer is).
const snapshotCmd = `ip -d link show; ip -d addr show; ip rule show; ip -6 rule show; ` +
	`ip route show table all; ip -6 route show table all; nft list ruleset; ` +
	`sysctl -a 2>/dev/null | grep -E '^net\.(ipv4\.(ip_forward|conf\.)|ipv6\.conf\.)' | grep -v 'stable_secret'`

// snapshotNoise is what the kernel changes by itself, not configuration:
// veth peer references, bridge timers and learning counters, the qdisc a
// NIC gets the first time it's up (noop until then), and a down switch
// port's DOWN vs LOWERLAYERDOWN (which depends on where its peer is).
var snapshotNoise = regexp.MustCompile(`@if\d+|link-netnsid \d+|(gc|hello|tcn|topology_change|forward_delay|message_age|hold)_timer\s+[\d.]+|fdb_n_learned \d+|qdisc \S+|state LOWERLAYERDOWN`)

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

	// Every resource this run creates, by exact name, for cleanup.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	name := func(s string) string { return "batter-lanit-" + suffix + "-" + s }
	network := name("net")
	var containers []string
	dind := name("box")
	t.Cleanup(func() {
		if t.Failed() {
			out, _ := runErr(time.Minute, "docker", "exec", "-w", "/stack", dind,
				"docker", "compose", "-p", "batter", "-f", "docker-compose.yml", "-f", "it.yml",
				"logs", "--no-color", "--tail", "150", "batter")
			t.Logf("batter logs:\n%s", out)
		}
		if os.Getenv("BATTER_IT_KEEP") == "1" {
			t.Logf("kept: %v, network %s", containers, network)
			return
		}
		if len(containers) > 0 {
			_, _ = runErr(2*time.Minute, "docker", append([]string{"rm", "-f", "-v"}, containers...)...)
		}
		_, _ = runErr(time.Minute, "docker", "network", "rm", network)
	})
	run(t, time.Minute, "docker", "network", "create", network)
	start := func(n string, args ...string) {
		t.Helper()
		containers = append(containers, n)
		run(t, time.Minute, "docker", append([]string{"run", "-d", "--name", n, "--network", network}, args...)...)
	}

	// The internet: an IP echo server, two VPN providers that forward to it
	// only, and another host on the box's LAN.
	echo, lanHost, wgA, wgB := name("echo"), name("lanhost"), name("wg-a"), name("wg-b")
	start(echo, "--network-alias", "echo",
		"-v", filepath.Join(repo, "test", "vpnit", "echo-ip.cgi")+":/www/cgi-bin/ip:ro",
		"busybox:1.36-musl", "httpd", "-f", "-p", "80", "-h", "/www")
	start(lanHost, "busybox:1.36-musl", "httpd", "-f", "-p", "80", "-h", "/tmp")
	serverKeyA, serverPubA := keypair(t)
	clientKeyA, clientPubA := keypair(t)
	serverKeyB, serverPubB := keypair(t)
	clientKeyB, clientPubB := keypair(t)
	for _, s := range []struct{ name, key, clientPub, subnet, answer string }{
		{wgA, serverKeyA, clientPubA, "10.99.0", dnsA},
		{wgB, serverKeyB, clientPubB, "10.98.0", dnsB},
	} {
		start(s.name, "--privileged",
			"-e", "SERVER_KEY="+s.key, "-e", "CLIENT_PUB="+s.clientPub, "-e", "SUBNET="+s.subnet,
			"-e", "DNS_ANSWER="+s.answer, "-e", "ONLY_TO=echo",
			"-v", filepath.Join(repo, "test", "vpnit", "wgserver.sh")+":/wgserver.sh:ro",
			"alpine:3.20", "sh", "/wgserver.sh")
	}
	for _, n := range []string{wgA, wgB} {
		waitFor(t, 2*time.Minute, n+" ready", func() bool {
			_, err := runErr(10*time.Second, "docker", "exec", n, "test", "-f", "/ready")
			return err == nil
		})
	}
	echoIP := containerIP(t, network, echo)
	ipA, ipB := containerIP(t, network, wgA), containerIP(t, network, wgB)
	lanHostIP := containerIP(t, network, lanHost)

	// The box.
	start(dind, "--privileged", "-e", "DOCKER_TLS_CERTDIR=", "docker:29-dind")
	waitFor(t, 2*time.Minute, "dind docker", func() bool {
		_, err := runErr(10*time.Second, "docker", "exec", dind, "docker", "info")
		return err == nil
	})
	box := func(timeout time.Duration, args ...string) (string, error) {
		return runErr(timeout, "docker", append([]string{"exec", dind}, args...)...)
	}
	boxSh := func(script string) (string, error) { return box(5*time.Minute, "sh", "-c", script) }
	if out, err := box(5*time.Minute, "sh", "-c", boxSetup+"echo ok"); err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("box setup: %v\n%s", err, out)
	}
	run(t, 20*time.Minute, "sh", "-c",
		"docker save "+image+" postgres:16-alpine alpine:3.20 | docker exec -i "+dind+" docker load")
	boxIP := containerIP(t, network, dind)

	stack := t.TempDir()
	copyFile(t, filepath.Join(repo, "docker-compose.yml"), filepath.Join(stack, "docker-compose.yml"))
	for _, f := range []string{"phone.sh", "udhcpc.sh", "banner.sh"} {
		copyFile(t, filepath.Join(dir, f), filepath.Join(stack, "phone", f))
	}
	build := exec.Command("go", "build", "-o", filepath.Join(stack, "adb"), "./test/fakeadb")
	build.Dir, build.Env = repo, append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fakeadb: %v\n%s", err, out)
	}
	// As shipped, except: the prebuilt image, the API published for this
	// test, the fake phone instead of USB, the echo server for the exit-IP
	// check, and veths allowed to stand in for physical NICs.
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
      BATTER_LAN_ALLOW_VETH: "1"
`
	if err := os.WriteFile(filepath.Join(stack, "it.yml"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, time.Minute, "docker", "cp", stack+"/.", dind+":/stack")
	compose := func(timeout time.Duration, args ...string) (string, error) {
		return runErr(timeout, "docker", append([]string{"exec", "-w", "/stack", dind,
			"docker", "compose", "-p", "batter", "-f", "docker-compose.yml", "-f", "it.yml"}, args...)...)
	}
	inBatter := func(args ...string) (string, error) {
		return compose(time.Minute, append([]string{"exec", "-T", "batter"}, args...)...)
	}
	inPhone := func(phone string, args ...string) (string, error) {
		return box(time.Minute, append([]string{"docker", "exec", phone}, args...)...)
	}

	// Phones (and the switch's management interface) on the switch, before
	// Batter starts.
	for _, p := range []struct{ name, hostname string }{{"phone1", ""}, {"phone2", ""}, {"switchmgmt", "usw-flex-mini"}} {
		args := []string{"docker", "run", "-d", "--name", p.name, "--network", "none", "--cap-add", "NET_ADMIN",
			"--sysctl", "net.ipv6.conf.all.disable_ipv6=0", "--sysctl", "net.ipv6.conf.default.disable_ipv6=0",
			"-v", "/stack/phone:/phone:ro"}
		if p.hostname != "" {
			args = append(args, "-e", "DHCP_HOSTNAME="+p.hostname)
		}
		if out, err := box(time.Minute, append(args, "alpine:3.20", "sh", "/phone/phone.sh")...); err != nil {
			t.Fatalf("start %s: %v %s", p.name, err, out)
		}
		script := fmt.Sprintf(`pid=$(docker inspect -f '{{.State.Pid}}' %[1]s) && ip link add p-%[1]s type veth peer name sw-p-%[1]s && `+
			`ip link set p-%[1]s netns $pid name eth0 && ip link set sw-p-%[1]s master phonesw && `+
			`sysctl -qw net.ipv6.conf.sw-p-%[1]s.disable_ipv6=1 && ip link set sw-p-%[1]s up`, p.name)
		if out, err := boxSh(script); err != nil {
			t.Fatalf("plug %s into the switch: %v %s", p.name, err, out)
		}
	}

	if out, err := compose(10*time.Minute, "up", "-d", "--wait", "batter"); err != nil {
		t.Fatalf("compose up: %v\n%s", err, out)
	}
	api := &api{base: "http://" + boxIP + ":8080"}
	api.waitHealthy(t)
	api.login(t)

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
		eventually(t, 60*time.Second, func() error {
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
	snapshot := func(t *testing.T) string {
		t.Helper()
		out, err := boxSh(snapshotCmd)
		if err != nil {
			t.Fatalf("snapshot: %v %s", err, out)
		}
		return snapshotNoise.ReplaceAllStringFunc(out, func(m string) string {
			if m == "state LOWERLAYERDOWN" {
				return "state DOWN"
			}
			return ""
		})
	}
	// sameBox compares snapshots as multisets of lines: listing order (of
	// links, say) isn't state.
	sameBox := func(t *testing.T, before, after, what string) {
		t.Helper()
		count := func(snap string) map[string]int {
			m := map[string]int{}
			for _, l := range strings.Split(snap, "\n") {
				m[strings.TrimSpace(l)]++
			}
			return m
		}
		b, a := count(before), count(after)
		var diff []string
		for l, n := range a {
			if b[l] != n {
				diff = append(diff, fmt.Sprintf("+%d/-%d %s", n, b[l], l))
			}
		}
		for l, n := range b {
			if _, ok := a[l]; !ok {
				diff = append(diff, fmt.Sprintf("-%d %s", n, l))
			}
		}
		if len(diff) > 0 {
			t.Fatalf("%s changed the box:\n%s", what, strings.Join(diff, "\n"))
		}
	}
	status := func(t *testing.T) string {
		t.Helper()
		_, body := api.do(t, "GET", "/api/v1/phone-network", nil)
		return body
	}
	waitState := func(t *testing.T, state string) {
		t.Helper()
		eventually(t, 60*time.Second, func() error {
			if st := status(t); !strings.Contains(st, `"state":"`+state+`"`) {
				return fmt.Errorf("phone network not %s: %s", state, st)
			}
			return nil
		})
	}
	setPort := func(t *testing.T, mac string) string {
		t.Helper()
		var body any = map[string]any{"mac": nil}
		if mac != "" {
			body = map[string]any{"mac": mac}
		}
		return api.must(t, "PUT", "/api/v1/phone-network", body, http.StatusOK)
	}
	onBox := func(nic string) (state string, present bool) {
		out, err := box(time.Minute, "ip", "-br", "link", "show", "dev", nic)
		if err != nil {
			return "", false
		}
		f := strings.Fields(out)
		if len(f) < 2 {
			return "", true
		}
		return f[1], true
	}
	// boxHears reports whether the box's stand-in services were reached
	// from phone: TCP to the box's link-local address (sshd, Caddy), and
	// UDP broadcast and multicast discovery.
	var nic2LL string
	boxHears := func(t *testing.T, phone string) []string {
		t.Helper()
		if _, err := boxSh(": > /tmp/box-heard"); err != nil {
			t.Fatal(err)
		}
		var reached []string
		for _, port := range []int{22, 443} {
			if phoneTCP(phone, nic2LL+"%eth0", port) {
				reached = append(reached, fmt.Sprintf("tcp [%s]:%d", nic2LL, port))
			}
		}
		// IPv4 broadcast to the phone's subnet(s) and to 255.255.255.255,
		// IPv6 multicast to all nodes.
		_, _ = inPhone(phone, "sh", "-c", `for a in $(ip -4 -o addr show dev eth0 | awk '{print $4}'); do `+
			`eval $(ipcalc -b "$a"); echo hello4 | nc -u -b -w 1 "$BROADCAST" 9999; done; `+
			`echo hello4 | nc -u -b -w 1 255.255.255.255 9999; echo hello6 | nc -u -w 1 ff02::1%eth0 9999`)
		time.Sleep(time.Second)
		heard, _ := box(time.Minute, "cat", "/tmp/box-heard")
		for _, h := range []string{"hello4", "hello6"} {
			if strings.Contains(heard, h) {
				reached = append(reached, "udp "+map[string]string{"hello4": "broadcast", "hello6": "multicast"}[h])
			}
		}
		return reached
	}

	var phone1IP, phone2IP, mgmtIP string
	step(t, "off: the box is untouched and phones get nothing", func(t *testing.T) {
		if st := status(t); !strings.Contains(st, `"state":"off"`) {
			t.Fatalf("status %s", st)
		}
		before := snapshot(t)
		time.Sleep(12 * time.Second) // two of Batter's passes
		sameBox(t, before, snapshot(t), "Batter with the phone network off")
		for _, p := range []string{"phone1", "phone2", "switchmgmt"} {
			if out, err := inPhone(p, "cat", "/tmp/ip"); err == nil {
				t.Fatalf("%s got an address (%s) with the phone network off", p, out)
			}
		}
		if out, err := inBatter("nft", "list", "table", "inet", "batter_lan"); err == nil {
			t.Fatalf("phone network firewall installed while off:\n%s", out)
		}
		if out, _ := inBatter("sysctl", "-n", "net.ipv4.ip_forward"); strings.TrimSpace(out) != "0" {
			t.Fatalf("forwarding on while off: %q", out)
		}
	})

	step(t, "a box port on the switch is reachable from phones (what Batter must prevent)", func(t *testing.T) {
		// nic2 up on the box: the old macvlan design's parent, or a box
		// whose NetworkManager manages the port (with an IPv4 link-local
		// address, as avahi-autoipd or a DHCP fallback would add; phone1 gets
		// one too, so it can send IPv4 broadcasts before any lease).
		if out, err := boxSh("ip address add 169.254.7.1/16 dev nic2 && ip link set nic2 up"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		if out, err := inPhone("phone1", "ip", "address", "add", "169.254.7.7/16", "dev", "eth0"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		eventually(t, 20*time.Second, func() error {
			out, _ := box(time.Minute, "sh", "-c", "ip -6 -o addr show dev nic2 scope link | grep -v tentative | awk '{print $4}' | cut -d/ -f1")
			if nic2LL = strings.TrimSpace(out); nic2LL == "" {
				return fmt.Errorf("nic2 has no link-local address yet")
			}
			return nil
		})
		eventually(t, 20*time.Second, func() error {
			if reached := boxHears(t, "phone1"); len(reached) != 4 {
				return fmt.Errorf("with nic2 up on the box, phone1 reached only %v (want TCP :22, :443, broadcast, multicast); the probes can't see a leak", reached)
			}
			return nil
		})
		if out, err := inPhone("phone1", "ip", "address", "del", "169.254.7.7/16", "dev", "eth0"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		if out, err := boxSh("ip address del 169.254.7.1/16 dev nic2 && ip link set nic2 down"); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	})

	step(t, "ports the box uses are refused", func(t *testing.T) {
		body := api.must(t, "GET", "/api/v1/phone-network/interfaces", nil, http.StatusOK)
		var list struct {
			Interfaces []struct {
				Name, MAC, Reason string
				Usable            bool
			} `json:"interfaces"`
		}
		if err := json.Unmarshal([]byte(body), &list); err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		macs := map[string]string{}
		for _, n := range list.Interfaces {
			got[n.Name] = n.Reason
			macs[n.Name] = n.MAC
			if n.Usable {
				got[n.Name] = "usable"
			}
		}
		for nic, want := range map[string]string{"nic2": "usable", "nic3": "usable", "nic4": "addresses", "eth0": "default route"} {
			if !strings.Contains(got[nic], want) {
				t.Errorf("%s: %q, want %q (list: %s)", nic, got[nic], want, body)
			}
		}
		for _, nic := range []string{"eth0", "nic4"} {
			if code, out := api.do(t, "PUT", "/api/v1/phone-network", map[string]any{"mac": macs[nic]}); code != http.StatusBadRequest {
				t.Errorf("choosing %s: %d %s, want 400", nic, code, out)
			}
		}
		if st := status(t); !strings.Contains(st, `"state":"off"`) {
			t.Fatalf("refused choice changed the state: %s", st)
		}
	})

	step(t, "on then off restores the box exactly", func(t *testing.T) {
		before := snapshot(t)
		if st := setPort(t, mac2); !strings.Contains(st, `"state":"active"`) {
			t.Fatalf("on: %s", st)
		}
		if _, present := onBox("nic2"); present {
			t.Fatal("nic2 still on the box with the phone network on")
		}
		if st := setPort(t, ""); !strings.Contains(st, `"state":"off"`) {
			t.Fatalf("off: %s", st)
		}
		if state, present := onBox("nic2"); !present || state != "DOWN" {
			t.Fatalf("nic2 after off: present=%v state=%q, want back on the box, DOWN", present, state)
		}
		sameBox(t, before, snapshot(t), "turning the phone network on and off")
	})

	step(t, "with the port taken, phones get leases and nothing on the switch reaches the box", func(t *testing.T) {
		if st := setPort(t, mac2); !strings.Contains(st, `"state":"active"`) {
			t.Fatalf("on: %s", st)
		}
		pool := netip.MustParsePrefix("10.77.0.0/24")
		for _, p := range []struct {
			name string
			ip   *string
		}{{"phone1", &phone1IP}, {"phone2", &phone2IP}, {"switchmgmt", &mgmtIP}} {
			eventually(t, 90*time.Second, func() error {
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
		if phone1IP == phone2IP || phone1IP == mgmtIP {
			t.Fatalf("duplicate leases: %s %s %s", phone1IP, phone2IP, mgmtIP)
		}
		t.Logf("phone1 %s, phone2 %s, switch %s; VPN servers A %s, B %s; box %s", phone1IP, phone2IP, mgmtIP, ipA, ipB, boxIP)
		for _, p := range []string{"phone1", "phone2", "switchmgmt"} {
			if reached := boxHears(t, p); len(reached) > 0 {
				t.Errorf("%s reached the box: %v", p, reached)
			}
		}
		if out, _ := inBatter("sh", "-c", "ip -6 addr show dev phonelan"); strings.Contains(out, "inet6") {
			t.Errorf("Batter has IPv6 on the phone network:\n%s", out)
		}
		mustBeOffline(t, "phone1", "no profile")
		mustBeOffline(t, "phone2", "no profile")
	})

	step(t, "the switch's own management interface gets nothing and isn't a phone", func(t *testing.T) {
		mustBeOffline(t, "switchmgmt", "not a phone")
		if phoneTCP("switchmgmt", batterLAN, 8080) || phoneTCP("switchmgmt", batterLAN, 3000) || phoneTCP("switchmgmt", lanHostIP, 80) {
			t.Fatal("the switch reached Batter or the box's LAN")
		}
		st := status(t)
		if !strings.Contains(st, `"hostname":"usw-flex-mini"`) {
			t.Fatalf("switch not listed as a client: %s", st)
		}
		var parsed struct {
			Clients []struct{ IP, Serial string }
		}
		_ = json.Unmarshal([]byte(st), &parsed)
		for _, c := range parsed.Clients {
			if c.IP == mgmtIP && c.Serial != "" {
				t.Fatalf("switch bound to a phone: %s", st)
			}
		}
		_, devs := api.do(t, "GET", "/api/v1/devices", nil)
		_, disc := api.do(t, "GET", "/api/v1/devices/discover", nil)
		if strings.Contains(devs, mgmtIP) || strings.Contains(disc, mgmtIP) {
			t.Fatalf("switch listed as a device: %s %s", devs, disc)
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

	step(t, "switching the port to nic3 gives nic2 back and phones keep working", func(t *testing.T) {
		if st := setPort(t, mac3); !strings.Contains(st, `"state":"active"`) || !strings.Contains(st, mac3) {
			t.Fatalf("switch: %s", st)
		}
		if state, present := onBox("nic2"); !present || state != "DOWN" {
			t.Fatalf("nic2 after the switch: present=%v state=%q, want back on the box, DOWN", present, state)
		}
		if _, present := onBox("nic3"); present {
			t.Fatal("nic3 still on the box")
		}
		mustExit(t, "phone1", ipA, "profile A on nic3")
		mustExit(t, "phone2", ipB, "profile B on nic3")
		if reached := boxHears(t, "phone1"); len(reached) > 0 {
			t.Errorf("phone1 reached the box: %v", reached)
		}
	})

	step(t, "a re-plugged port is taken again", func(t *testing.T) {
		if out, err := box(time.Minute, "ip", "link", "del", "sw-nic3"); err != nil {
			t.Fatalf("unplug: %v %s", err, out)
		}
		waitState(t, "missing")
		if out, err := boxSh(replugCmd("nic3", mac3)); err != nil {
			t.Fatalf("replug: %v %s", err, out)
		}
		waitState(t, "active")
		if _, present := onBox("nic3"); present {
			t.Fatal("re-plugged nic3 not taken from the box")
		}
		mustExit(t, "phone1", ipA, "after the re-plug")
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
		// On a graceful stop Batter gives the port back to the box (down)
		// and takes it again on start. Had its namespace gone first, a veth
		// would be destroyed (a physical NIC goes back to the box): then it
		// shows as missing until re-plugged, as a USB NIC would be.
		if state, present := onBox("nic3"); present && state != "DOWN" {
			t.Fatalf("nic3 on the box %s after the restart", state)
		}
		logs, _ := compose(time.Minute, "logs", "--no-color", "batter")
		if !strings.Contains(logs, `"msg":"lan: phone network port given back to the box","component":"lan","name":"nic3"`) {
			t.Errorf("Batter didn't give nic3 back on stop")
		}
		eventually(t, 60*time.Second, func() error {
			st := status(t)
			if strings.Contains(st, `"state":"missing"`) {
				if _, present := onBox("nic3"); !present {
					if out, err := boxSh(replugCmd("nic3", mac3)); err != nil {
						return fmt.Errorf("replug: %v %s", err, out)
					}
				}
			}
			if !strings.Contains(st, `"state":"active"`) {
				return fmt.Errorf("phone network not back: %s", st)
			}
			return nil
		})
		mustExit(t, "phone1", ipA, "profile A after restart")
		mustExit(t, "phone2", ipB, "profile B after restart")
		mustResolve(t, "phone1", dnsA, "profile A after restart")
		mustResolve(t, "phone2", dnsB, "profile B after restart")
		confined(t, "phone1")
		confined(t, "phone2")
		if reached := boxHears(t, "phone1"); len(reached) > 0 {
			t.Errorf("phone1 reached the box after the restart: %v", reached)
		}
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

	step(t, "turning it off gives the port back and leaves nothing behind", func(t *testing.T) {
		if st := setPort(t, ""); !strings.Contains(st, `"state":"off"`) {
			t.Fatalf("off: %s", st)
		}
		if state, present := onBox("nic3"); !present || state != "DOWN" {
			t.Fatalf("nic3 after off: present=%v state=%q, want back on the box, DOWN", present, state)
		}
		if out, err := inBatter("nft", "list", "table", "inet", "batter_lan"); err == nil {
			t.Fatalf("firewall left behind:\n%s", out)
		}
		if out, _ := inBatter("ip", "rule", "show"); strings.Contains(out, "phonelan") {
			t.Fatalf("rules left behind:\n%s", out)
		}
		if out, _ := inBatter("sysctl", "-n", "net.ipv4.ip_forward"); strings.TrimSpace(out) != "0" {
			t.Fatalf("forwarding left on: %q", out)
		}
		mustBeOffline(t, "phone1", "the phone network off")
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

func containerIP(t *testing.T, network, name string) string {
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
