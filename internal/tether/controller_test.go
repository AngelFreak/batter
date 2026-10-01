package tether

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeADB records calls and simulates one device's package and reverse state.
type fakeADB struct {
	calls     []string
	installed bool
	reverses  []string // device-side specs, e.g. "localabstract:gnirehtet"
}

func (f *fakeADB) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeADB) Shell(_ context.Context, serial string, args ...string) ([]byte, error) {
	f.record("%s shell %s", serial, strings.Join(args, " "))
	if len(args) >= 2 && args[0] == "pm" && args[1] == "path" {
		if f.installed {
			return []byte("package:/data/app/gnirehtet/base.apk\n"), nil
		}
		return nil, nil
	}
	return nil, nil
}

func (f *fakeADB) Install(_ context.Context, serial, apk string) ([]byte, error) {
	f.record("%s install %s", serial, apk)
	f.installed = true
	return []byte("Success\n"), nil
}

func (f *fakeADB) Reverse(_ context.Context, serial, name string, port int) error {
	f.record("%s reverse localabstract:%s tcp:%d", serial, name, port)
	f.reverses = append(f.reverses, "localabstract:"+name)
	return nil
}

func (f *fakeADB) RemoveReverse(_ context.Context, serial, name string) error {
	f.record("%s reverse --remove localabstract:%s", serial, name)
	f.reverses = slices.DeleteFunc(f.reverses, func(s string) bool { return s == "localabstract:"+name })
	return nil
}

func (f *fakeADB) ListReverse(_ context.Context, serial string) ([]string, error) {
	f.record("%s reverse --list", serial)
	return f.reverses, nil
}

const startCmd = "S1 shell am start -a com.genymobile.gnirehtet.START -n com.genymobile.gnirehtet/.GnirehtetActivity"

func newController(adb *fakeADB, dns ...string) *Controller {
	return &Controller{ADB: adb, APK: "/apk/gnirehtet.apk", Logger: quiet, DNS: func() []string { return dns }}
}

func TestEnableInstallsAppWhenMissingThenTunnelsAndStarts(t *testing.T) {
	adb := &fakeADB{}
	if err := newController(adb).Enable(context.Background(), "S1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"S1 shell pm path com.genymobile.gnirehtet",
		"S1 install /apk/gnirehtet.apk",
		"S1 reverse localabstract:gnirehtet tcp:31416",
		startCmd,
	}
	if !slices.Equal(adb.calls, want) {
		t.Fatalf("calls:\n  %s\nwant:\n  %s", strings.Join(adb.calls, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestEnableSkipsInstallWhenPresent(t *testing.T) {
	adb := &fakeADB{installed: true}
	if err := newController(adb).Enable(context.Background(), "S1"); err != nil {
		t.Fatal(err)
	}
	for _, c := range adb.calls {
		if strings.Contains(c, " install ") {
			t.Fatalf("reinstalled an installed app: %v", adb.calls)
		}
	}
}

func TestEnablePassesDNSServers(t *testing.T) {
	adb := &fakeADB{installed: true}
	if err := newController(adb, "10.64.0.1", "10.64.0.2").Enable(context.Background(), "S1"); err != nil {
		t.Fatal(err)
	}
	want := startCmd + " --esa dnsServers 10.64.0.1,10.64.0.2"
	if !slices.Contains(adb.calls, want) {
		t.Fatalf("no start with DNS servers; calls: %v", adb.calls)
	}
}

func TestDisableStopsClientAndRemovesOnlyItsTunnel(t *testing.T) {
	adb := &fakeADB{installed: true, reverses: []string{"localabstract:gnirehtet", "localabstract:scrcpy_1234"}}
	if err := newController(adb).Disable(context.Background(), "S1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(adb.calls, "S1 shell am start -a com.genymobile.gnirehtet.STOP -n com.genymobile.gnirehtet/.GnirehtetActivity") {
		t.Fatalf("client not stopped; calls: %v", adb.calls)
	}
	if !slices.Equal(adb.reverses, []string{"localabstract:scrcpy_1234"}) {
		t.Fatalf("reverses left: %v, want only scrcpy's", adb.reverses)
	}
}

func TestReconcileRestoresLostTunnelsOnly(t *testing.T) {
	healthy := &fakeADB{installed: true, reverses: []string{"localabstract:gnirehtet"}}
	replugged := &fakeADB{installed: true} // tunnel gone after replug
	byserial := map[string]*fakeADB{"HEALTHY": healthy, "REPLUGGED": replugged}

	for serial, adb := range byserial {
		c := newController(adb)
		c.Reconcile(context.Background(), []string{serial, "UNPLUGGED"}, []string{serial})
	}

	if slices.ContainsFunc(healthy.calls, func(s string) bool { return strings.Contains(s, "am start") }) {
		t.Fatalf("restarted a healthy device: %v", healthy.calls)
	}
	if !slices.Contains(replugged.reverses, "localabstract:gnirehtet") {
		t.Fatalf("tunnel not restored after replug: %v", replugged.calls)
	}
	for _, adb := range byserial {
		if slices.ContainsFunc(adb.calls, func(s string) bool { return strings.HasPrefix(s, "UNPLUGGED") }) {
			t.Fatalf("touched a disconnected device: %v", adb.calls)
		}
	}
}

// A device whose tethering was switched off while a reconcile pass was
// re-enabling it (or whose disable failed) ends up tunnelled with the setting
// off; the next pass turns it off. Devices without the tunnel are not poked.
func TestReconcileStopsTetheringThatShouldBeOff(t *testing.T) {
	stale := &fakeADB{installed: true, reverses: []string{"localabstract:gnirehtet", "localabstract:scrcpy_1234"}}
	newController(stale).Reconcile(context.Background(), nil, []string{"S1"})
	if slices.Contains(stale.reverses, "localabstract:gnirehtet") {
		t.Fatalf("tethering left on for a device with it switched off: %v", stale.calls)
	}
	if !slices.Contains(stale.reverses, "localabstract:scrcpy_1234") {
		t.Fatalf("removed scrcpy's tunnel: %v", stale.reverses)
	}

	untouched := &fakeADB{installed: true}
	newController(untouched).Reconcile(context.Background(), nil, []string{"S1"})
	if slices.ContainsFunc(untouched.calls, func(s string) bool { return strings.Contains(s, "am start") }) {
		t.Fatalf("started an activity on a device that isn't tethered: %v", untouched.calls)
	}
}

func TestWatchReconcilesImmediatelyAndOnEachTick(t *testing.T) {
	adb := &fakeADB{installed: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	passes := 0
	done := make(chan struct{})
	go func() {
		newController(adb).Watch(ctx, time.Millisecond, func(context.Context) ([]string, []string, error) {
			passes++
			if passes == 3 {
				cancel()
			}
			return []string{"S1"}, []string{"S1"}, nil
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch didn't stop when its context was cancelled")
	}
	if passes < 3 {
		t.Fatalf("%d passes, want ticks to keep reconciling", passes)
	}
	if !slices.Contains(adb.reverses, "localabstract:gnirehtet") {
		t.Fatalf("enabled device not tethered: %v", adb.calls)
	}
}

func TestWatchSkipsPassWhenStateUnknown(t *testing.T) {
	adb := &fakeADB{installed: true, reverses: []string{"localabstract:gnirehtet"}}
	ctx, cancel := context.WithCancel(context.Background())
	newController(adb).Watch(ctx, time.Hour, func(context.Context) ([]string, []string, error) {
		cancel()
		return nil, []string{"S1"}, fmt.Errorf("db down")
	})
	if len(adb.calls) != 0 {
		t.Fatalf("acted on a device without knowing its setting: %v", adb.calls)
	}
}
