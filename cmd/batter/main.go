package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/XpertaDK/batter/internal/api"
	"github.com/XpertaDK/batter/internal/api/handlers"
	"github.com/XpertaDK/batter/internal/auth"
	"github.com/XpertaDK/batter/internal/config"
	"github.com/XpertaDK/batter/internal/device"
	"github.com/XpertaDK/batter/internal/migrate"
	"github.com/XpertaDK/batter/internal/tether"
	"github.com/XpertaDK/batter/internal/vpn"
)

func main() {
	// Helper run as the relay's uid to see where tethered traffic exits.
	if len(os.Args) > 1 && os.Args[1] == vpn.ExitIPSubcommand {
		os.Exit(vpn.ExitIPCommand(os.Args[2:], os.Stdout, os.Stderr))
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	// Connect to database and bring the schema up to date
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := handlers.ConnectDB(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	if err := migrate.Up(ctx, db, logger); err != nil {
		logger.Error("failed to apply database migrations", "error", err)
		os.Exit(1)
	}

	// Initialize device manager
	dm, err := device.NewManager(device.ManagerConfig{
		ScrcpyServerPath:   cfg.ScrcpyServerPath,
		ScrcpyVersion:      cfg.ScrcpyVersion,
		ScreenshotCacheDir: cfg.DataDir,
		Logger:             logger,
	})
	if err != nil {
		logger.Error("failed to initialize device manager", "error", err)
		os.Exit(1)
	}

	// VPN profiles: tethered phones' traffic goes through one, each
	// profile with its own relay (uid, port) and WireGuard tunnel.
	vpnSvc := &vpn.Service{DB: db, Logger: logger.With("component", "vpn")}
	if exe, err := os.Executable(); err == nil {
		vpnSvc.CheckExitAs = func(ctx context.Context, uid uint32) (string, error) {
			return vpn.ExitIPChecker(exe, cfg.VPNExitIPURL, uid)(ctx)
		}
	}

	// Reverse tethering. Without the relay binary it is off: profiles still
	// manage tunnels, and assigning a phone reports it as unavailable.
	var tetherCtl *tether.Controller
	if _, err := os.Stat(cfg.GnirehtetPath); err == nil {
		tetherCtl = &tether.Controller{ADB: dm.ADB(), APK: cfg.GnirehtetAPK, Logger: logger.With("component", "tether")}
		vpnSvc.StartRelay = func(ctx context.Context, uid uint32, port int) {
			relay := &tether.Relay{
				Path:   cfg.GnirehtetPath,
				UID:    uid,
				GID:    uid,
				Port:   port,
				Logger: logger.With("component", "tether-relay", "port", port),
			}
			go relay.Run(ctx)
		}
	} else {
		logger.Warn("reverse tethering unavailable: gnirehtet not found", "path", cfg.GnirehtetPath)
	}

	// Initialize JWT manager
	jwtManager := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTExpirySecs)

	// Set up router
	router, err := api.NewRouter(api.RouterConfig{
		DeviceManager:  dm,
		DB:             db,
		JWTManager:     jwtManager,
		Logger:         logger,
		AllowedOrigins: cfg.AllowedOrigins,
		TrustedProxies: cfg.TrustedProxies,
		Tether:         tetherCtl,
		VPN:            vpnSvc,
	})
	if err != nil {
		logger.Error("failed to set up router", "error", err)
		os.Exit(1)
	}

	// Start session health checker (cleans up dead sessions every 30s)
	stopHealthCheck := dm.StartHealthChecker(30 * time.Second)

	// Bring up every profile: each relay starts only once its kill switch
	// is in. The single-tunnel version's config becomes profile "Default".
	if err := vpnSvc.ImportLegacy(ctx, filepath.Join(cfg.DataDir, "wireguard.json")); err != nil {
		logger.Error("failed to import the previous version's VPN config", "error", err)
	}
	if err := vpnSvc.Sync(ctx); err != nil {
		logger.Error("failed to apply VPN profiles", "error", err)
	}

	// Re-apply each device's profile after replugs and restarts.
	tetherCtx, stopTether := context.WithCancel(context.Background())
	defer stopTether()
	if tetherCtl != nil {
		go tetherCtl.Watch(tetherCtx, tetherReconcileInterval, func(ctx context.Context) (map[string]tether.Target, []string, error) {
			return tetherState(ctx, vpnSvc, dm)
		})
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("failed to listen", "addr", addr, "error", err)
		os.Exit(1)
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("starting batter server", "addr", addr)
	serveErr := serve(sigCtx, newHTTPServer(router), ln, shutdownDrainTimeout)

	// Teardown order: stop taking requests (done by serve), then device
	// sessions, then the DB pool that in-flight handlers were still using.
	logger.Info("shutting down...")
	stopTether()
	vpnSvc.Stop()
	stopHealthCheck()
	dm.Shutdown()
	db.Close()

	if serveErr != nil {
		logger.Error("server error", "error", serveErr)
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}

// tetherReconcileInterval is how soon a replugged device gets its tethering
// back.
const tetherReconcileInterval = 15 * time.Second

// tetherState reports where each tethered device's traffic should go and
// which devices adb sees as connected and authorized.
func tetherState(ctx context.Context, vpnSvc *vpn.Service, dm *device.Manager) (want map[string]tether.Target, connected []string, err error) {
	if want, err = vpnSvc.TetherTargets(ctx); err != nil {
		return nil, nil, fmt.Errorf("tether settings: %w", err)
	}
	devices, err := dm.ListDevices(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list devices: %w", err)
	}
	for _, d := range devices {
		if d.State == "device" {
			connected = append(connected, d.Serial)
		}
	}
	return want, connected, nil
}

// shutdownDrainTimeout bounds how long in-flight HTTP requests may finish
// after SIGTERM. Keep it under compose's stop_grace_period, leaving room for
// device session teardown.
const shutdownDrainTimeout = 10 * time.Second

// newHTTPServer returns the HTTP server with connection-level timeouts.
// There's deliberately no ReadTimeout/WriteTimeout: those bound the whole
// request, which would cut off large APK uploads and WebSocket streams.
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// serve runs srv on ln until ctx is cancelled, then stops accepting
// connections and waits up to drainTimeout for in-flight requests. Hijacked
// connections (WebSockets) aren't waited for; device teardown ends them.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, drainTimeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err // Serve never returns nil before Shutdown
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("draining requests: %w", err)
	}
	return nil
}
