package vpn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ExitIPSubcommand is the batter argument that runs ExitIPCommand.
const ExitIPSubcommand = "vpn-exit-ip"

// ExitIPCommand fetches args[0], which must answer with an IP address, and
// prints it. Batter runs it as a profile's checker uid (see ExitIPChecker)
// so the request takes the profile's phones' route.
func ExitIPCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 && (len(args) != 3 || args[1] != "--connect") {
		fmt.Fprintln(stderr, "usage: batter "+ExitIPSubcommand+" URL [--connect IP]")
		return 2
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if len(args) == 3 {
		// Connect to the pre-resolved address; the URL's host still names
		// the server for HTTP and TLS.
		ip := args[2]
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				_, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
			},
		}
	}
	resp, err := client.Get(args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if resp.StatusCode != http.StatusOK || err != nil {
		fmt.Fprintf(stderr, "%s answered %s, not an IP address\n", args[0], resp.Status)
		return 1
	}
	fmt.Fprintln(stdout, ip)
	return 0
}

// ExitIPChecker returns a check that runs `exe vpn-exit-ip url` as uid (0 =
// don't switch) and returns the IP it prints.
func ExitIPChecker(exe, url string, uid uint32) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		// The checker uid can't reach local addresses, Docker's DNS resolver
		// included, so resolve the check's host here (outside the tunnel;
		// only the checker's own hostname leaks this way).
		args := []string{ExitIPSubcommand, url}
		if u, err := neturl.Parse(url); err == nil && net.ParseIP(u.Hostname()) == nil {
			addrs, err := net.DefaultResolver.LookupIP(ctx, "ip4", u.Hostname())
			if err != nil || len(addrs) == 0 {
				return "", fmt.Errorf("resolve %s: %v", u.Hostname(), err)
			}
			args = append(args, "--connect", addrs[0].String())
		}
		cmd := exec.CommandContext(ctx, exe, args...)
		if uid != 0 {
			cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid}}
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return "", fmt.Errorf("%s", msg)
			}
			return "", err
		}
		return strings.TrimSpace(stdout.String()), nil
	}
}
