package tether

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	appPackage  = "com.genymobile.gnirehtet"
	appActivity = appPackage + "/.GnirehtetActivity"
	// abstractName is the device-side socket the app connects to; adb
	// reverse forwards it to the relay.
	abstractName = "gnirehtet"
)

// ADB is the subset of adb operations tethering needs (*device.ADB).
type ADB interface {
	Shell(ctx context.Context, serial string, args ...string) ([]byte, error)
	Install(ctx context.Context, serial, apkPath string) ([]byte, error)
	Reverse(ctx context.Context, serial, abstractName string, localPort int) error
	RemoveReverse(ctx context.Context, serial, abstractName string) error
	ListReverse(ctx context.Context, serial string) ([]string, error)
}

// Controller turns reverse tethering on and off per device.
type Controller struct {
	ADB    ADB
	APK    string // path to gnirehtet.apk
	Logger *slog.Logger
	// DNS returns the DNS servers devices should use (e.g. the VPN's), or
	// nil for gnirehtet's default (8.8.8.8). Queries go through the relay
	// either way.
	DNS func() []string

	// mu serializes changes so a toggle and a reconcile pass don't drive the
	// same device at once.
	mu sync.Mutex
}

// Enable installs the client if needed, connects it to the relay and starts
// it. The first start on a device shows Android's VPN permission prompt.
func (c *Controller) Enable(ctx context.Context, serial string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enable(ctx, serial)
}

func (c *Controller) enable(ctx context.Context, serial string) error {
	out, err := c.ADB.Shell(ctx, serial, "pm", "path", appPackage)
	if err != nil {
		return fmt.Errorf("check gnirehtet app: %w", err)
	}
	if !strings.Contains(string(out), "package:") {
		res, err := c.ADB.Install(ctx, serial, c.APK)
		if err != nil {
			return fmt.Errorf("install gnirehtet app: %w", err)
		}
		if !strings.Contains(string(res), "Success") {
			return fmt.Errorf("install gnirehtet app: %s", strings.TrimSpace(string(res)))
		}
	}
	if err := c.ADB.Reverse(ctx, serial, abstractName, RelayPort); err != nil {
		return fmt.Errorf("adb reverse: %w", err)
	}
	args := []string{"am", "start", "-a", appPackage + ".START", "-n", appActivity}
	if c.DNS != nil {
		if dns := c.DNS(); len(dns) > 0 {
			args = append(args, "--esa", "dnsServers", strings.Join(dns, ","))
		}
	}
	if _, err := c.ADB.Shell(ctx, serial, args...); err != nil {
		return fmt.Errorf("start gnirehtet client: %w", err)
	}
	c.Logger.Info("reverse tethering enabled", "serial", serial)
	return nil
}

// Disable stops the client and removes its tunnel (and only its tunnel).
func (c *Controller) Disable(ctx context.Context, serial string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disable(ctx, serial)
}

func (c *Controller) disable(ctx context.Context, serial string) error {
	if _, err := c.ADB.Shell(ctx, serial, "am", "start", "-a", appPackage+".STOP", "-n", appActivity); err != nil {
		return fmt.Errorf("stop gnirehtet client: %w", err)
	}
	if err := c.ADB.RemoveReverse(ctx, serial, abstractName); err != nil {
		return fmt.Errorf("remove adb reverse: %w", err)
	}
	c.Logger.Info("reverse tethering disabled", "serial", serial)
	return nil
}

// Reconcile brings connected devices in line with their setting. Devices
// that should be tethered but lost their tunnel (replugged, adb or Batter
// restarted) are re-enabled; devices still tunnelled with the setting off (a
// toggle raced a pass, or a disable failed) are disabled. Devices already in
// the right state are left alone, so their VPN isn't bounced.
func (c *Controller) Reconcile(ctx context.Context, enabled, connected []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, serial := range connected {
		reverses, err := c.ADB.ListReverse(ctx, serial)
		if err != nil {
			c.Logger.Warn("tether reconcile: list reverse", "serial", serial, "error", err)
			continue
		}
		want := slices.Contains(enabled, serial)
		if slices.Contains(reverses, "localabstract:"+abstractName) == want {
			continue
		}
		if want {
			err = c.enable(ctx, serial)
		} else {
			err = c.disable(ctx, serial)
		}
		if err != nil {
			c.Logger.Warn("tether reconcile", "serial", serial, "enable", want, "error", err)
		}
	}
}

// reconcileTimeout bounds one reconcile pass (which may install the app).
const reconcileTimeout = 2 * time.Minute

// Watch reconciles now and then every interval until ctx is done. state
// reports the serials that should be tethered and those connected; when it
// fails the pass is skipped rather than guessing.
func (c *Controller) Watch(ctx context.Context, interval time.Duration, state func(context.Context) (enabled, connected []string, err error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		passCtx, cancel := context.WithTimeout(ctx, reconcileTimeout)
		if enabled, connected, err := state(passCtx); err != nil {
			c.Logger.Warn("tether reconcile: device state unavailable", "error", err)
		} else {
			c.Reconcile(passCtx, enabled, connected)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
