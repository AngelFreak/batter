package vpn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ExitIPSubcommand is the batter argument that runs ExitIPCommand.
const ExitIPSubcommand = "vpn-exit-ip"

// ExitIPCommand fetches args[0], which must answer with an IP address, and
// prints it. Batter runs it as the relay uid (see ExitIPChecker) so the
// request takes tethered devices' route.
func ExitIPCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: batter "+ExitIPSubcommand+" URL")
		return 2
	}
	client := &http.Client{Timeout: 10 * time.Second}
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
		cmd := exec.CommandContext(ctx, exe, ExitIPSubcommand, url)
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
