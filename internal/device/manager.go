package device

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// SessionTier represents the quality tier for a scrcpy session.
type SessionTier string

const (
	TierThumbnail SessionTier = "thumbnail" // 360p, 5fps — grid view
	TierFull      SessionTier = "full"      // 1024p, 30fps — focused view
)

// TierOptions returns SessionOptions for the given tier.
func TierOptions(tier SessionTier) SessionOptions {
	switch tier {
	case TierThumbnail:
		return SessionOptions{MaxSize: 360, MaxFPS: 5}
	case TierFull:
		return SessionOptions{MaxSize: 1024, MaxFPS: 30}
	default:
		return SessionOptions{MaxSize: 1024, MaxFPS: 30}
	}
}

// ManagerConfig holds configuration for the device manager.
type ManagerConfig struct {
	ScrcpyServerPath   string
	ScrcpyVersion      string
	ScreenshotCacheDir string // DataDir for screenshot cache (e.g. "./data")
	Logger             *slog.Logger
}

// Manager manages ADB devices and scrcpy sessions.
type Manager struct {
	adb          *ADB
	sessions     map[string]*Session
	sessionTiers map[string]SessionTier
	fullViewers  map[string]int // reference count of full-quality viewers per serial
	// mu guards the maps above and is only ever held briefly. Slow ADB work
	// (session start/stop, which can take seconds or hang) is serialized per
	// device by deviceLocks instead, so one stuck device can't stall the rest.
	mu               sync.RWMutex
	deviceLocksMu    sync.Mutex
	deviceLocks      map[string]*sync.Mutex
	scrcpyServerPath string
	scrcpyVersion    string
	screenshotCache  *ScreenshotCache
	logger           *slog.Logger
}

// NewManager creates a new device manager.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	logger := cfg.Logger.With("component", "device-manager")

	adb, err := NewADB(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ADB: %w", err)
	}

	// Validate scrcpy-server binary exists
	if cfg.ScrcpyServerPath == "" {
		return nil, fmt.Errorf("scrcpy-server path is required")
	}
	if _, err := os.Stat(cfg.ScrcpyServerPath); err != nil {
		return nil, fmt.Errorf("scrcpy-server not found at %s: %w", cfg.ScrcpyServerPath, err)
	}

	logger.Info("device manager initialized",
		"scrcpy_server", cfg.ScrcpyServerPath,
		"adb_path", adb.adbPath,
	)

	scrcpyVersion := cfg.ScrcpyVersion
	if scrcpyVersion == "" {
		scrcpyVersion = "2.7"
	}

	var ssCache *ScreenshotCache
	if cfg.ScreenshotCacheDir != "" {
		sc, err := NewScreenshotCache(cfg.ScreenshotCacheDir)
		if err != nil {
			logger.Warn("failed to init screenshot cache, continuing without", "error", err)
		} else {
			ssCache = sc
			logger.Info("screenshot cache initialized", "dir", cfg.ScreenshotCacheDir)
		}
	}

	return &Manager{
		adb:              adb,
		sessions:         make(map[string]*Session),
		sessionTiers:     make(map[string]SessionTier),
		fullViewers:      make(map[string]int),
		scrcpyServerPath: cfg.ScrcpyServerPath,
		scrcpyVersion:    scrcpyVersion,
		screenshotCache:  ssCache,
		logger:           logger,
	}, nil
}

// ADB returns the manager's adb client, for features (reverse tethering)
// that drive devices outside of scrcpy sessions.
func (m *Manager) ADB() *ADB {
	return m.adb
}

// ListDevices returns all connected ADB devices with session status.
func (m *Manager) ListDevices(ctx context.Context) ([]DeviceInfo, error) {
	devices, err := m.adb.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []DeviceInfo
	for _, d := range devices {
		info := DeviceInfo{
			Serial:  d.Serial,
			State:   d.State,
			Model:   d.Model,
			Product: d.Product,
		}
		if s, ok := m.sessions[d.Serial]; ok {
			info.HasSession = true
			info.Width = s.Width
			info.Height = s.Height
		}
		if tier, ok := m.sessionTiers[d.Serial]; ok {
			info.SessionTier = tier
		}
		result = append(result, info)
	}
	return result, nil
}

// lockDevice serializes session lifecycle operations on one device and
// returns the unlock function. Locks are never freed; there is one per
// device serial ever seen, which is bounded by the fleet.
func (m *Manager) lockDevice(serial string) func() {
	m.deviceLocksMu.Lock()
	if m.deviceLocks == nil {
		m.deviceLocks = make(map[string]*sync.Mutex)
	}
	l, ok := m.deviceLocks[serial]
	if !ok {
		l = &sync.Mutex{}
		m.deviceLocks[serial] = l
	}
	m.deviceLocksMu.Unlock()

	l.Lock()
	return l.Unlock
}

// detachSession removes serial's session from the maps and returns it (nil
// if none). The caller closes it outside m.mu, since Close can block.
func (m *Manager) detachSession(serial string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[serial]
	delete(m.sessions, serial)
	delete(m.sessionTiers, serial)
	return s
}

// killDeviceServer force-kills any lingering scrcpy-server on the device and
// removes scrcpy's reverse tunnels so a new session can bind its abstract
// socket. Other tunnels (reverse tethering) are left in place.
func (m *Manager) killDeviceServer(ctx context.Context, serial string) {
	_, _ = m.adb.Shell(ctx, serial, "pkill", "-9", "-f", "app_process.*scrcpy")
	specs, err := m.adb.ListReverse(ctx, serial)
	if err != nil {
		m.logger.Debug("list reverse tunnels", "serial", serial, "error", err)
		return
	}
	for _, spec := range specs {
		if name, ok := strings.CutPrefix(spec, "localabstract:"); ok && strings.HasPrefix(name, "scrcpy_") {
			_ = m.adb.RemoveReverse(ctx, serial, name)
		}
	}
}

// StartSession starts a scrcpy session for a device. If a dead session exists, it is replaced.
func (m *Manager) StartSession(ctx context.Context, serial string, opts SessionOptions) (*Session, error) {
	defer m.lockDevice(serial)()

	// If session exists and is alive, return it
	if s := m.GetSession(serial); s != nil {
		if s.IsAlive() {
			return s, nil
		}
		// Dead session — clean up before starting a new one
		m.logger.Info("replacing dead session", "serial", serial)
		if s := m.detachSession(serial); s != nil {
			s.Close()
		}
		m.killDeviceServer(ctx, serial)
		// Brief delay for the device to fully release the process
		time.Sleep(500 * time.Millisecond)
	}

	return m.startSessionLocked(serial, opts)
}

// startSessionLocked starts a new session. The caller holds serial's device lock.
func (m *Manager) startSessionLocked(serial string, opts SessionOptions) (*Session, error) {
	session, err := newSession(m.adb, serial, m.scrcpyServerPath, m.scrcpyVersion, opts, m.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to start session for %s: %w", serial, err)
	}

	// Determine tier from options
	tier := TierFull
	if opts.MaxSize > 0 && opts.MaxSize <= 360 {
		tier = TierThumbnail
	}

	m.mu.Lock()
	m.sessions[serial] = session
	m.sessionTiers[serial] = tier
	m.mu.Unlock()

	m.logger.Info("session started", "serial", serial, "width", session.Width, "height", session.Height, "tier", tier)
	return session, nil
}

// StopSession stops a running scrcpy session.
func (m *Manager) StopSession(serial string) error {
	defer m.lockDevice(serial)()

	if m.GetSession(serial) == nil {
		return fmt.Errorf("no session for device %s", serial)
	}

	// Best-effort: capture a screenshot before stopping the session
	if m.screenshotCache != nil {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if png, err := m.adb.Screenshot(ctx, serial); err == nil {
				if err := m.screenshotCache.Save(serial, png); err != nil {
					m.logger.Warn("failed to cache screenshot on stop", "serial", serial, "error", err)
				}
			} else {
				m.logger.Warn("failed to capture screenshot on stop", "serial", serial, "error", err)
			}
		}()
	}

	s := m.detachSession(serial)
	m.mu.Lock()
	delete(m.fullViewers, serial)
	m.mu.Unlock()
	if s != nil {
		s.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.killDeviceServer(ctx, serial)

	m.logger.Info("session stopped", "serial", serial)
	return nil
}

// RestartSession stops any existing session and starts a fresh one.
func (m *Manager) RestartSession(ctx context.Context, serial string, opts SessionOptions) (*Session, error) {
	defer m.lockDevice(serial)()

	if s := m.detachSession(serial); s != nil {
		s.Close()
	}
	m.killDeviceServer(ctx, serial)
	time.Sleep(500 * time.Millisecond)

	return m.startSessionLocked(serial, opts)
}

// UpgradeSession switches a device session from thumbnail to full quality.
// Returns the new session. Increments the full-viewer reference count.
func (m *Manager) UpgradeSession(ctx context.Context, serial string) (*Session, error) {
	m.mu.Lock()
	currentTier := m.sessionTiers[serial]
	m.fullViewers[serial]++
	count := m.fullViewers[serial]
	m.mu.Unlock()

	m.logger.Info("upgrade requested", "serial", serial, "current_tier", currentTier, "full_viewers", count)

	if currentTier == TierFull {
		// Already full quality, just return existing session
		s := m.GetSession(serial)
		if s != nil {
			return s, nil
		}
	}

	return m.RestartSession(ctx, serial, TierOptions(TierFull))
}

// DowngradeSession decrements the full-viewer reference count.
// When count reaches 0, switches back to thumbnail quality.
func (m *Manager) DowngradeSession(ctx context.Context, serial string) (*Session, error) {
	m.mu.Lock()
	m.fullViewers[serial]--
	if m.fullViewers[serial] < 0 {
		m.fullViewers[serial] = 0
	}
	count := m.fullViewers[serial]
	m.mu.Unlock()

	m.logger.Info("downgrade requested", "serial", serial, "full_viewers", count)

	if count > 0 {
		// Other viewers still need full quality
		s := m.GetSession(serial)
		if s != nil {
			return s, nil
		}
	}

	return m.RestartSession(ctx, serial, TierOptions(TierThumbnail))
}

// GetSessionTier returns the current tier for a device session.
func (m *Manager) GetSessionTier(serial string) SessionTier {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionTiers[serial]
}

// GetSession returns an active session for a device.
func (m *Manager) GetSession(serial string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[serial]
}

// WakeScreen wakes the device screen via ADB shell key events.
func (m *Manager) WakeScreen(ctx context.Context, serial string) error {
	if _, err := m.adb.Shell(ctx, serial, "input", "keyevent", "KEYCODE_WAKEUP"); err != nil {
		return fmt.Errorf("send WAKEUP: %w", err)
	}
	return nil
}

// SetKeepAwake enables or disables stay-awake mode on the device.
func (m *Manager) SetKeepAwake(ctx context.Context, serial string, enabled bool) error {
	if enabled {
		_, _ = m.adb.Shell(ctx, serial, "settings", "put", "system", "screen_off_timeout", "2147483647")
		_, _ = m.adb.Shell(ctx, serial, "settings", "put", "global", "stay_on_while_plugged_in", "3")
	} else {
		_, _ = m.adb.Shell(ctx, serial, "settings", "put", "system", "screen_off_timeout", "30000")
		_, _ = m.adb.Shell(ctx, serial, "settings", "put", "global", "stay_on_while_plugged_in", "0")
	}
	return nil
}

// HealthStatus reports the readiness of the device subsystem.
type HealthStatus struct {
	ADBAvailable  bool   `json:"adb_available"`
	ADBVersion    string `json:"adb_version,omitempty"`
	ScrcpyServer  bool   `json:"scrcpy_server"`
	ScrcpyVersion string `json:"scrcpy_version,omitempty"`
	DeviceCount   int    `json:"device_count"`
}

// Health checks whether ADB, scrcpy-server, and connected devices are available.
func (m *Manager) Health(ctx context.Context) HealthStatus {
	status := HealthStatus{
		ScrcpyServer:  m.scrcpyServerPath != "",
		ScrcpyVersion: m.scrcpyVersion,
	}

	// Check ADB
	out, err := m.adb.run(ctx, "version")
	if err == nil {
		status.ADBAvailable = true
		if lines := strings.SplitN(string(out), "\n", 2); len(lines) > 0 {
			status.ADBVersion = strings.TrimSpace(lines[0])
		}
	}

	// Count connected devices
	devices, err := m.adb.ListDevices(ctx)
	if err == nil {
		status.DeviceCount = len(devices)
	}

	return status
}

// Screenshot captures the device screen. On success, caches the result to disk.
func (m *Manager) Screenshot(ctx context.Context, serial string) ([]byte, error) {
	png, err := m.adb.Screenshot(ctx, serial)
	if err != nil {
		return nil, err
	}

	// Cache in background
	if m.screenshotCache != nil {
		go func() {
			if err := m.screenshotCache.Save(serial, png); err != nil {
				m.logger.Warn("failed to cache screenshot", "serial", serial, "error", err)
			}
		}()
	}

	return png, nil
}

// PushFile pushes a local file to the device via ADB.
func (m *Manager) PushFile(ctx context.Context, serial, localPath, remotePath string) error {
	return m.adb.Push(ctx, serial, localPath, remotePath)
}

// InstallAPK installs an APK file on the device via ADB.
func (m *Manager) InstallAPK(ctx context.Context, serial, apkPath string) error {
	out, err := m.adb.Install(ctx, serial, apkPath)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}
	if !strings.Contains(string(out), "Success") {
		return fmt.Errorf("install failed: %s", string(out))
	}
	return nil
}

// ScreenshotCache returns the screenshot cache (may be nil if not configured).
func (m *Manager) ScreenshotCache() *ScreenshotCache {
	return m.screenshotCache
}

// Shutdown stops all sessions.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = make(map[string]*Session)
	m.sessionTiers = make(map[string]SessionTier)
	m.fullViewers = make(map[string]int)
	m.mu.Unlock()

	for serial, s := range sessions {
		s.Close()
		m.logger.Info("session closed during shutdown", "serial", serial)
	}
}

// DeviceInfo extends ADBDevice with session and registration status.
type DeviceInfo struct {
	Serial         string      `json:"serial"`
	State          string      `json:"state"`
	Status         string      `json:"status"`
	Model          string      `json:"model"`
	Product        string      `json:"product"`
	Nickname       string      `json:"nickname,omitempty"`
	AndroidVersion string      `json:"android_version,omitempty"`
	HasSession     bool        `json:"has_session"`
	Width          int         `json:"width,omitempty"`
	Height         int         `json:"height,omitempty"`
	SessionTier    SessionTier `json:"session_tier,omitempty"`
	LastSeenAt     *time.Time  `json:"last_seen_at,omitempty"`
	ReverseTether  bool        `json:"reverse_tether"`
}

// ValidateDevice checks whether a device is reachable via ADB and returns its state.
func (m *Manager) ValidateDevice(ctx context.Context, serial string) (string, error) {
	return m.adb.GetState(ctx, serial)
}

// GetDeviceProperties retrieves device properties via ADB shell getprop.
func (m *Manager) GetDeviceProperties(ctx context.Context, serial string) (map[string]string, error) {
	return m.adb.GetProperties(ctx, serial)
}
