package device

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakePhoneManager runs a real Manager against test/fakeadb.
func fakePhoneManager(t *testing.T) *Manager {
	t.Helper()
	bin := t.TempDir()
	if out, err := exec.Command("go", "build", "-o", filepath.Join(bin, "adb"), "../../test/fakeadb").CombinedOutput(); err != nil {
		t.Fatalf("build fakeadb: %v\n%s", err, out)
	}
	t.Setenv("FAKEADB_DIR", t.TempDir())
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	scrcpy := filepath.Join(t.TempDir(), "scrcpy-server")
	if err := os.WriteFile(scrcpy, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(ManagerConfig{ScrcpyServerPath: scrcpy, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Shutdown)
	return m
}

func TestUpgradeReusesSessionAtSameQualityAndRestartsAtAnother(t *testing.T) {
	m := fakePhoneManager(t)
	ctx := context.Background()
	if _, err := m.StartSession(ctx, "FAKE01", TierOptions(TierThumbnail)); err != nil {
		t.Fatal(err)
	}
	if q := m.GetSessionQuality("FAKE01"); q != "" {
		t.Fatalf("thumbnail session reports quality %q", q)
	}

	medium, err := m.UpgradeSession(ctx, "FAKE01", QualityMedium, false)
	if err != nil {
		t.Fatal(err)
	}
	if q := m.GetSessionQuality("FAKE01"); q != QualityMedium {
		t.Fatalf("quality %q after upgrade, want medium", q)
	}
	// A second viewer at the same level shares the running session.
	again, err := m.UpgradeSession(ctx, "FAKE01", QualityMedium, false)
	if err != nil || again != medium {
		t.Fatalf("same-level upgrade restarted the session (%v)", err)
	}
	// A viewer switching level restarts it, for everyone (latest wins), and
	// doesn't count as another viewer.
	high, err := m.UpgradeSession(ctx, "FAKE01", QualityHigh, true)
	if err != nil || high == medium {
		t.Fatalf("level change didn't restart the session (%v)", err)
	}
	if q := m.GetSessionQuality("FAKE01"); q != QualityHigh {
		t.Fatalf("quality %q, want high", q)
	}
	// Two viewers counted, so one leaving keeps full quality.
	if _, err := m.DowngradeSession(ctx, "FAKE01"); err != nil {
		t.Fatal(err)
	}
	if m.GetSessionTier("FAKE01") != TierFull || m.GetSession("FAKE01") != high {
		t.Fatal("one of two viewers leaving dropped full quality")
	}
}
