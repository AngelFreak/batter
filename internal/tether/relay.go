// Package tether provides reverse tethering: Android devices reach the
// internet through this host over adb, using gnirehtet
// (https://github.com/Genymobile/gnirehtet). A relay server runs here; each
// device runs gnirehtet's VPN app, which tunnels its traffic to the relay
// through `adb reverse`.
package tether

import (
	"cmp"
	"context"
	"log/slog"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// RelayUID is the first relay uid. Each VPN profile's relay runs as its own
// uid (and nothing else does), so policy routing can send exactly that
// relay's traffic, the traffic of the phones on that profile, through the
// profile's tunnel.
const RelayUID = 31416

// RelayPort is gnirehtet's default relay port and the first profile's;
// devices reach their relay through `adb reverse localabstract:gnirehtet
// tcp:<port>`.
const RelayPort = 31416

// Relay supervises the gnirehtet relay server process.
type Relay struct {
	// Path to the gnirehtet binary.
	Path string
	// Port the relay listens on; 0 = gnirehtet's default (RelayPort).
	Port int
	// UID/GID to run the relay as. The relay's outbound connections carry
	// all tethered devices' traffic; giving it its own uid lets policy
	// routing send exactly that traffic through a VPN. 0 = don't switch.
	UID, GID uint32
	Logger   *slog.Logger

	backoff func(attempt int) time.Duration // overridable in tests
}

// Run starts the relay and restarts it whenever it exits, until ctx is done.
func (r *Relay) Run(ctx context.Context) {
	backoff := r.backoff
	if backoff == nil {
		backoff = relayBackoff
	}
	attempt := 0
	for {
		started := time.Now()
		err := r.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		// A relay that ran for a while was healthy; restart it promptly.
		if time.Since(started) > time.Minute {
			attempt = 0
		}
		delay := backoff(attempt)
		attempt++
		r.Logger.Warn("gnirehtet relay exited; restarting", "error", err, "in", delay.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (r *Relay) runOnce(ctx context.Context) error {
	args := []string{"relay"}
	if r.Port != 0 {
		args = append(args, "-p", strconv.Itoa(r.Port))
	}
	cmd := exec.CommandContext(ctx, r.Path, args...)
	if r.UID != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: r.UID, Gid: r.GID},
		}
	}
	r.Logger.Info("starting gnirehtet relay", "uid", r.UID, "port", cmp.Or(r.Port, RelayPort))
	return cmd.Run()
}

// relayBackoff doubles from 1s up to 30s.
func relayBackoff(attempt int) time.Duration {
	d := time.Second << min(attempt, 5)
	return min(d, 30*time.Second)
}
