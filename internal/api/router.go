package api

import (
	"log/slog"
	"time"

	"github.com/XpertaDK/batter/internal/api/handlers"
	"github.com/XpertaDK/batter/internal/api/middleware"
	"github.com/XpertaDK/batter/internal/auth"
	"github.com/XpertaDK/batter/internal/device"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RouterConfig holds dependencies for setting up routes.
type RouterConfig struct {
	DeviceManager  *device.Manager
	DB             *pgxpool.Pool
	JWTManager     *auth.JWTManager
	Logger         *slog.Logger
	AllowedOrigins []string
}

// NewRouter creates and configures the Gin router with all routes.
func NewRouter(cfg RouterConfig) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// Global middleware
	r.Use(gin.Recovery())
	r.Use(middleware.Logger(cfg.Logger))
	r.Use(middleware.CORS(middleware.CORSConfig{
		AllowedOrigins: cfg.AllowedOrigins,
	}))

	// Health check (unauthenticated)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Handlers
	authHandler := handlers.NewAuthHandler(cfg.DB, cfg.JWTManager, cfg.Logger)
	deviceHandler := handlers.NewDeviceHandler(cfg.DeviceManager, cfg.DB, cfg.Logger)
	deviceWSHandler := handlers.NewDeviceWSHandler(cfg.DeviceManager, cfg.Logger, cfg.AllowedOrigins)
	userHandler := handlers.NewUserHandler(cfg.DB, cfg.Logger)
	groupHandler := handlers.NewGroupHandler(cfg.DB, cfg.DeviceManager, cfg.Logger)
	userGroupHandler := handlers.NewUserGroupHandler(cfg.DB, cfg.Logger)

	// API v1
	v1 := r.Group("/api/v1")
	{
		// Public auth routes. Rate-limit the unauthenticated ones per client IP
		// to blunt brute-force / credential-stuffing and token-refresh abuse.
		loginLimit := middleware.RateLimit(10, time.Minute)   // login attempts
		refreshLimit := middleware.RateLimit(30, time.Minute) // token refreshes
		v1.GET("/auth/needs-setup", authHandler.NeedsSetup)
		v1.POST("/admin/setup", loginLimit, authHandler.Setup)
		v1.POST("/auth/login", loginLimit, authHandler.Login)
		v1.POST("/auth/refresh", refreshLimit, authHandler.Refresh)
		v1.POST("/auth/logout", authHandler.Logout)

		// Protected routes
		protected := v1.Group("")
		protected.Use(middleware.Auth(cfg.JWTManager))
		protected.Use(middleware.AuditLog(cfg.DB))
		{
			// Current user
			protected.GET("/auth/me", authHandler.Me)

			// Devices
			devices := protected.Group("/devices")
			{
				devices.GET("", deviceHandler.ListDevices)
				devices.GET("/health", deviceHandler.DeviceHealth)

				// Pre-registration endpoints (operator+, no device-specific RBAC)
				preReg := devices.Group("")
				preReg.Use(middleware.RequireRole("operator"))
				{
					preReg.POST("", deviceHandler.RegisterDevice)
					preReg.GET("/discover", deviceHandler.DiscoverDevices)
					preReg.POST("/validate/:serial", deviceHandler.ValidateDevice)
					preReg.POST("/probe/:serial", deviceHandler.ProbeDevice)
				}

				// Per-device endpoints. "view" covers watching (which needs a
				// running session); "control" covers acting on the device;
				// "manage" covers changing Batter's record of it.
				view := middleware.RequireDevicePermission(cfg.DB, "view")
				control := middleware.RequireDevicePermission(cfg.DB, "control")
				manage := middleware.RequireDevicePermission(cfg.DB, "manage")
				deviceBySerial := devices.Group("/:serial")
				{
					deviceBySerial.GET("", view, deviceHandler.GetDevice)
					deviceBySerial.GET("/screenshot", view, deviceHandler.Screenshot)
					deviceBySerial.POST("/session/start", view, deviceHandler.StartSession)
					deviceBySerial.POST("/session/upgrade", view, deviceHandler.UpgradeSession)
					deviceBySerial.POST("/session/downgrade", view, deviceHandler.DowngradeSession)
					deviceBySerial.POST("/session/stop", control, deviceHandler.StopSession)
					deviceBySerial.POST("/wake", control, deviceHandler.WakeScreen)
					deviceBySerial.POST("/push", control, deviceHandler.PushFile)
					deviceBySerial.POST("/install", control, deviceHandler.InstallAPK)
					deviceBySerial.PUT("", manage, deviceHandler.UpdateDevice)
					deviceBySerial.DELETE("", manage, deviceHandler.DeleteDevice)
				}
			}

			// Device groups. Anyone may list them; changing a group's
			// membership or grants changes who can reach which device, so
			// that is admin-only. Batch ops filter to devices the caller
			// holds the needed permission on (see GroupHandler).
			groups := protected.Group("/groups")
			{
				groups.GET("", groupHandler.ListGroups)
				groups.GET("/:id/devices", groupHandler.GetGroupDevices)
				groups.POST("/:id/batch/start", groupHandler.BatchStart)
				groups.POST("/:id/batch/stop", groupHandler.BatchStop)

				adminGroups := groups.Group("")
				adminGroups.Use(middleware.RequireRole("admin"))
				{
					adminGroups.POST("", groupHandler.CreateGroup)
					adminGroups.PUT("/:id", groupHandler.UpdateGroup)
					adminGroups.DELETE("/:id", groupHandler.DeleteGroup)
					adminGroups.POST("/:id/devices", groupHandler.AddDevices)
					adminGroups.DELETE("/:id/devices/:serial", groupHandler.RemoveDevice)
					adminGroups.GET("/:id/access", groupHandler.GetGroupAccess)
					adminGroups.DELETE("/:id/access/:accessId", groupHandler.RevokeGroupAccess)
					adminGroups.GET("/:id/team-access", groupHandler.GetGroupTeamAccess)
					adminGroups.POST("/:id/team-access", groupHandler.GrantGroupTeamAccess)
					adminGroups.DELETE("/:id/team-access/:accessId", groupHandler.RevokeGroupTeamAccess)
				}
			}

			// Users (admin only)
			users := protected.Group("/users")
			users.Use(middleware.RequireRole("admin"))
			{
				users.GET("", userHandler.ListUsers)
				users.POST("", userHandler.CreateUser)
				users.PUT("/:id", userHandler.UpdateUser)
				users.DELETE("/:id", userHandler.DeleteUser)
				users.GET("/:id/devices", userHandler.ListUserAccess)
				users.POST("/:id/devices", userHandler.GrantAccess)
				users.DELETE("/:id/devices/:accessId", userHandler.RevokeUserAccess)
				users.PUT("/:id/password", userHandler.ResetPassword)
			}

			// User groups / teams (admin only)
			userGroups := protected.Group("/user-groups")
			userGroups.Use(middleware.RequireRole("admin"))
			{
				userGroups.GET("", userGroupHandler.ListUserGroups)
				userGroups.POST("", userGroupHandler.CreateUserGroup)
				userGroups.PUT("/:id", userGroupHandler.UpdateUserGroup)
				userGroups.DELETE("/:id", userGroupHandler.DeleteUserGroup)
				userGroups.GET("/:id/members", userGroupHandler.ListMembers)
				userGroups.POST("/:id/members", userGroupHandler.AddMember)
				userGroups.DELETE("/:id/members/:userId", userGroupHandler.RemoveMember)
				userGroups.GET("/:id/access", userGroupHandler.ListAccess)
				userGroups.POST("/:id/access", userGroupHandler.GrantAccess)
				userGroups.DELETE("/:id/access/:accessId", userGroupHandler.RevokeAccess)
			}
		}
	}

	// WebSocket endpoints (JWT via query param)
	wsGroup := r.Group("/ws")
	wsGroup.Use(middleware.WSAuth(cfg.JWTManager))
	{
		// Video: requires "view" permission
		wsGroup.GET("/device/:serial/video",
			middleware.RequireDevicePermission(cfg.DB, "view"),
			deviceWSHandler.VideoStream,
		)
		// Control: requires "control" permission (viewers can watch but not control)
		wsGroup.GET("/device/:serial/control",
			middleware.RequireDevicePermission(cfg.DB, "control"),
			deviceWSHandler.ControlStream,
		)
	}

	return r
}
