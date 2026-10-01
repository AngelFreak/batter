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
	reverses  map[string]string // device side -> host side
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
	if f.reverses == nil {
		f.reverses = map[string]string{}
	}
	f.reverses["localabstract:"+name] = fmt.Sprintf("tcp:%d", port)
	return nil
}

func (f *fakeADB) RemoveReverse(_ context.Context, serial, name string) error {
	f.record("%s reverse --remove localabstract:%s", serial, name)
	delete(f.reverses, "localabstract:"+name)
	return nil
}

func (f *fakeADB) ListReverse(_ context.Context, serial string) (map[string]string, error) {
	f.record("%s reverse --list", serial)
	return f.reverses, nil
}

const (
	startCmd = "S1 shell am start -a com.genymobile.gnirehtet.START -n com.genymobile.gnirehtet/.GnirehtetActivity"
	stopCmd  = "S1 shell am start -a com.genymobile.gnirehtet.STOP -n com.genymobile.gnirehtet/.GnirehtetActivity"
)

var profileA = Target{Port: 31416}

func newController(adb *fakeADB) *Controller {
	return &Controller{ADB: adb, APK: "/apk/gnirehtet.apk", Logger: quiet}
}

func started(adb *fakeADB) bool {
	return slices.ContainsFunc(adb.calls, func(s string) bool { return strings.Contains(s, ".START") })
}

func TestEnableInstallsAppWhenMissingThenTunnelsAndStarts(t *testing.T) {
	adb := &fakeADB{}
	if err := newController(adb).Enable(context.Background(), "S1", Target{Port: 31417}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"S1 shell pm path com.genymobile.gnirehtet",
		"S1 install /apk/gnirehtet.apk",
		"S1 reverse --list",
		"S1 reverse localabstract:gnirehtet tcp:31417",
		startCmd,
	}
	if !slices.Equal(adb.calls, want) {
		t.Fatalf("calls:\n  %s\nwant:\n  %s", strings.Join(adb.calls, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestEnableSkipsInstallWhenPresent(t *testing.T) {
	adb := &fakeADB{installed: true}
	if err := newController(adb).Enable(context.Background(), "S1", profileA); err != nil {
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
	err := newController(adb).Enable(context.Background(), "S1", Target{Port: 31416, DNS: []string{"10.64.0.1", "10.64.0.2"}})
	if err != nil {
		t.Fatal(err)
	}
	want := startCmd + " --esa dnsServers 10.64.0.1,10.64.0.2"
	if !slices.Contains(adb.calls, want) {
		t.Fatalf("no start with DNS servers; calls: %v", adb.calls)
	}
}

// Moving a phone to another profile must restart its client: the running
// one is connected to the old relay and uses the old profile's DNS.
func TestEnableRestartsClientConnectedElsewhere(t *testing.T) {
	adb := &fakeADB{installed: true, reverses: map[string]string{"localabstract:gnirehtet": "tcp:31416"}}
	if err := newController(adb).Enable(context.Background(), "S1", Target{Port: 31417}); err != nil {
		t.Fatal(err)
	}
	stop, start := slices.Index(adb.calls, stopCmd), slices.Index(adb.calls, startCmd)
	if stop < 0 || start < stop {
		t.Fatalf("client not stopped before restart; calls: %v", adb.calls)
	}
	if adb.reverses["localabstract:gnirehtet"] != "tcp:31417" {
		t.Fatalf("tunnel points at %s, want tcp:31417", adb.reverses["localabstract:gnirehtet"])
	}
}

func TestDisableStopsClientAndRemovesOnlyItsTunnel(t *testing.T) {
	adb := &fakeADB{installed: true, reverses: map[string]string{
		"localabstract:gnirehtet": "tcp:31416", "localabstract:scrcpy_1234": "tcp:40000"}}
	if err := newController(adb).Disable(context.Background(), "S1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(adb.calls, stopCmd) {
		t.Fatalf("client not stopped; calls: %v", adb.calls)
	}
	if len(adb.reverses) != 1 || adb.reverses["localabstract:scrcpy_1234"] == "" {
		t.Fatalf("reverses left: %v, want only scrcpy's", adb.reverses)
	}
}

func TestReconcileRestoresLostTunnelsOnly(t *testing.T) {
	healthy := &fakeADB{installed: true, reverses: map[string]string{"localabstract:gnirehtet": "tcp:31416"}}
	replugged := &fakeADB{installed: true} // tunnel gone after replug
	byserial := map[string]*fakeADB{"HEALTHY": healthy, "REPLUGGED": replugged}

	for serial, adb := range byserial {
		want := map[string]Target{serial: profileA, "UNPLUGGED": profileA}
		newController(adb).Reconcile(context.Background(), want, []string{serial})
	}

	if started(healthy) {
		t.Fatalf("restarted a healthy device: %v", healthy.calls)
	}
	if replugged.reverses["localabstract:gnirehtet"] != "tcp:31416" {
		t.Fatalf("tunnel not restored after replug: %v", replugged.calls)
	}
	for _, adb := range byserial {
		if slices.ContainsFunc(adb.calls, func(s string) bool { return strings.HasPrefix(s, "UNPLUGGED") }) {
			t.Fatalf("touched a disconnected device: %v", adb.calls)
		}
	}
}

// A phone whose profile changed while it was unplugged (or whose re-point
// failed) still tunnels to the old relay; reconcile moves it.
func TestReconcileRepointsTunnelToTheProfilesRelay(t *testing.T) {
	adb := &fakeADB{installed: true, reverses: map[string]string{"localabstract:gnirehtet": "tcp:31416"}}
	newController(adb).Reconcile(context.Background(), map[string]Target{"S1": {Port: 31418}}, []string{"S1"})
	if adb.reverses["localabstract:gnirehtet"] != "tcp:31418" || !started(adb) {
		t.Fatalf("tunnel not re-pointed: %v", adb.calls)
	}
}

// A device whose tethering was switched off while a reconcile pass was
// re-enabling it (or whose disable failed, or whose profile was deleted)
// ends up tunnelled with no profile; the next pass turns it off. Devices
// without the tunnel are not poked.
func TestReconcileStopsTetheringThatShouldBeOff(t *testing.T) {
	stale := &fakeADB{installed: true, reverses: map[string]string{
		"localabstract:gnirehtet": "tcp:31416", "localabstract:scrcpy_1234": "tcp:40000"}}
	newController(stale).Reconcile(context.Background(), nil, []string{"S1"})
	if _, ok := stale.reverses["localabstract:gnirehtet"]; ok {
		t.Fatalf("tethering left on for a device with it switched off: %v", stale.calls)
	}
	if _, ok := stale.reverses["localabstract:scrcpy_1234"]; !ok {
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
		newController(adb).Watch(ctx, time.Millisecond, func(context.Context) (map[string]Target, []string, error) {
			passes++
			if passes == 3 {
				cancel()
			}
			return map[string]Target{"S1": profileA}, []string{"S1"}, nil
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
	if adb.reverses["localabstract:gnirehtet"] != "tcp:31416" {
		t.Fatalf("assigned device not tethered: %v", adb.calls)
	}
}

func TestWatchSkipsPassWhenStateUnknown(t *testing.T) {
	adb := &fakeADB{installed: true, reverses: map[string]string{"localabstract:gnirehtet": "tcp:31416"}}
	ctx, cancel := context.WithCancel(context.Background())
	newController(adb).Watch(ctx, time.Hour, func(context.Context) (map[string]Target, []string, error) {
		cancel()
		return nil, []string{"S1"}, fmt.Errorf("db down")
	})
	if len(adb.calls) != 0 {
		t.Fatalf("acted on a device without knowing its setting: %v", adb.calls)
	}
}
