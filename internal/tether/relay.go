// Package tether provides reverse tethering: Android devices reach the
// internet through this host over adb, using gnirehtet
// (https://github.com/Genymobile/gnirehtet). A relay server runs here; each
// device runs gnirehtet's VPN app, which tunnels its traffic to the relay
// through `adb reverse`.
package tether

import (
	"context"
	"log/slog"
	"os/exec"
	"syscall"
	"time"
)

// RelayUID runs the relay (and nothing else). Policy routing matches on it to
// send tethered devices' traffic, and only that, through the VPN.
const RelayUID = 31416

// RelayPort is gnirehtet's default relay port; devices reach it through
// `adb reverse localabstract:gnirehtet tcp:RelayPort`.
const RelayPort = 31416

// Relay supervises the gnirehtet relay server process.
type Relay struct {
	// Path to the gnirehtet binary.
	Path string
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
	cmd := exec.CommandContext(ctx, r.Path, "relay")
	if r.UID != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: r.UID, Gid: r.GID},
		}
	}
	r.Logger.Info("starting gnirehtet relay", "uid", r.UID, "port", RelayPort)
	return cmd.Run()
}

// relayBackoff doubles from 1s up to 30s.
func relayBackoff(attempt int) time.Duration {
	d := time.Second << min(attempt, 5)
	return min(d, 30*time.Second)
}
