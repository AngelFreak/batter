package tether

import (
	"context"
	"fmt"
	"log/slog"
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
	ListReverse(ctx context.Context, serial string) (map[string]string, error)
}

// Target is where a tethered device's traffic goes: the port of its VPN
// profile's relay, and the DNS servers its client should use (nil for
// gnirehtet's default, 8.8.8.8). Queries go through the relay either way.
type Target struct {
	Port int
	DNS  []string
}

func (t Target) hostSpec() string { return fmt.Sprintf("tcp:%d", t.Port) }

// Controller turns reverse tethering on and off per device.
type Controller struct {
	ADB    ADB
	APK    string // path to gnirehtet.apk
	Logger *slog.Logger

	// mu serializes changes so a toggle and a reconcile pass don't drive the
	// same device at once.
	mu sync.Mutex
}

// Enable installs the client if needed, connects it to t's relay and starts
// it. A client already tethered (to any relay) is stopped first so it comes
// back on the new relay with t's DNS. The first start on a device shows
// Android's VPN permission prompt.
func (c *Controller) Enable(ctx context.Context, serial string, t Target) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enable(ctx, serial, t)
}

func (c *Controller) enable(ctx context.Context, serial string, t Target) error {
	// Not `pm path`: it exits non-zero when the package is missing, which
	// is indistinguishable from adb failing.
	out, err := c.ADB.Shell(ctx, serial, "pm", "list", "packages", appPackage)
	if err != nil {
		return fmt.Errorf("check gnirehtet app: %w", err)
	}
	if !hasPackage(string(out), appPackage) {
		res, err := c.ADB.Install(ctx, serial, c.APK)
		if err != nil {
			return fmt.Errorf("install gnirehtet app: %w", err)
		}
		if !strings.Contains(string(res), "Success") {
			return fmt.Errorf("install gnirehtet app: %s", strings.TrimSpace(string(res)))
		}
	}
	reverses, err := c.ADB.ListReverse(ctx, serial)
	if err != nil {
		return fmt.Errorf("list adb reverse: %w", err)
	}
	if _, ok := reverses["localabstract:"+abstractName]; ok {
		if err := c.stopClient(ctx, serial); err != nil {
			return err
		}
	}
	if err := c.ADB.Reverse(ctx, serial, abstractName, t.Port); err != nil {
		return fmt.Errorf("adb reverse: %w", err)
	}
	args := []string{"am", "start", "-a", appPackage + ".START", "-n", appActivity}
	if len(t.DNS) > 0 {
		args = append(args, "--esa", "dnsServers", strings.Join(t.DNS, ","))
	}
	if _, err := c.ADB.Shell(ctx, serial, args...); err != nil {
		return fmt.Errorf("start gnirehtet client: %w", err)
	}
	c.Logger.Info("reverse tethering enabled", "serial", serial, "relay_port", t.Port)
	return nil
}

// Disable stops the client and removes its tunnel (and only its tunnel).
func (c *Controller) Disable(ctx context.Context, serial string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disable(ctx, serial)
}

func (c *Controller) disable(ctx context.Context, serial string) error {
	if err := c.stopClient(ctx, serial); err != nil {
		return err
	}
	if err := c.ADB.RemoveReverse(ctx, serial, abstractName); err != nil {
		return fmt.Errorf("remove adb reverse: %w", err)
	}
	c.Logger.Info("reverse tethering disabled", "serial", serial)
	return nil
}

func (c *Controller) stopClient(ctx context.Context, serial string) error {
	if _, err := c.ADB.Shell(ctx, serial, "am", "start", "-a", appPackage+".STOP", "-n", appActivity); err != nil {
		return fmt.Errorf("stop gnirehtet client: %w", err)
	}
	return nil
}

// Reconcile brings connected devices in line with want (serial -> target).
// Devices that should be tethered but lost their tunnel (replugged, adb or
// Batter restarted) or tunnel to another relay (profile changed) are
// (re)enabled; devices still tunnelled that shouldn't be (a toggle raced a
// pass, a disable failed, the profile was deleted) are disabled. Devices
// already in the right state are left alone, so their VPN isn't bounced.
func (c *Controller) Reconcile(ctx context.Context, want map[string]Target, connected []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, serial := range connected {
		reverses, err := c.ADB.ListReverse(ctx, serial)
		if err != nil {
			c.Logger.Warn("tether reconcile: list reverse", "serial", serial, "error", err)
			continue
		}
		host, tunnelled := reverses["localabstract:"+abstractName]
		target, wanted := want[serial]
		switch {
		case wanted && host != target.hostSpec():
			err = c.enable(ctx, serial, target)
		case !wanted && tunnelled:
			err = c.disable(ctx, serial)
		default:
			continue
		}
		if err != nil {
			c.Logger.Warn("tether reconcile", "serial", serial, "enable", wanted, "error", err)
		}
	}
}

// reconcileTimeout bounds one reconcile pass (which may install the app).
const reconcileTimeout = 2 * time.Minute

// Watch reconciles now and then every interval until ctx is done. state
// reports each tethered serial's target and the connected serials; when it
// fails the pass is skipped rather than guessing.
func (c *Controller) Watch(ctx context.Context, interval time.Duration, state func(context.Context) (want map[string]Target, connected []string, err error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		passCtx, cancel := context.WithTimeout(ctx, reconcileTimeout)
		if want, connected, err := state(passCtx); err != nil {
			c.Logger.Warn("tether reconcile: device state unavailable", "error", err)
		} else {
			c.Reconcile(passCtx, want, connected)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// hasPackage reports whether `pm list packages` output lists exactly pkg (the
// filter also matches packages that merely contain the name).
func hasPackage(out, pkg string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "package:"+pkg {
			return true
		}
	}
	return false
}
