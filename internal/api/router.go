package api

import (
	"log/slog"

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
	deviceWSHandler := handlers.NewDeviceWSHandler(cfg.DeviceManager, cfg.Logger)
	userHandler := handlers.NewUserHandler(cfg.DB, cfg.Logger)
	groupHandler := handlers.NewGroupHandler(cfg.DB, cfg.DeviceManager, cfg.Logger)
	userGroupHandler := handlers.NewUserGroupHandler(cfg.DB, cfg.Logger)

	// API v1
	v1 := r.Group("/api/v1")
	{
		// Public auth routes
		v1.GET("/auth/needs-setup", authHandler.NeedsSetup)
		v1.POST("/admin/setup", authHandler.Setup)
		v1.POST("/auth/login", authHandler.Login)
		v1.POST("/auth/refresh", authHandler.Refresh)
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

				// Per-device endpoints require at least "view" permission
				deviceBySerial := devices.Group("/:serial")
				deviceBySerial.Use(middleware.RequireDevicePermission(cfg.DB, "view"))
				{
					deviceBySerial.GET("", deviceHandler.GetDevice)
					deviceBySerial.GET("/screenshot", deviceHandler.Screenshot)
					deviceBySerial.POST("/session/start", deviceHandler.StartSession)
					deviceBySerial.POST("/session/stop", deviceHandler.StopSession)
					deviceBySerial.POST("/session/upgrade", deviceHandler.UpgradeSession)
					deviceBySerial.POST("/session/downgrade", deviceHandler.DowngradeSession)
					deviceBySerial.POST("/wake", deviceHandler.WakeScreen)
					deviceBySerial.POST("/push", deviceHandler.PushFile)
					deviceBySerial.POST("/install", deviceHandler.InstallAPK)
					deviceBySerial.PUT("", deviceHandler.UpdateDevice)
					deviceBySerial.DELETE("", deviceHandler.DeleteDevice)
				}
			}

			// Groups
			groups := protected.Group("/groups")
			{
				groups.GET("", groupHandler.ListGroups)
				groups.POST("", groupHandler.CreateGroup)
				groups.PUT("/:id", groupHandler.UpdateGroup)
				groups.DELETE("/:id", groupHandler.DeleteGroup)
				groups.GET("/:id/devices", groupHandler.GetGroupDevices)
				groups.POST("/:id/devices", groupHandler.AddDevices)
				groups.DELETE("/:id/devices/:serial", groupHandler.RemoveDevice)
				groups.POST("/:id/batch/start", groupHandler.BatchStart)
				groups.POST("/:id/batch/stop", groupHandler.BatchStop)
				groups.GET("/:id/access", groupHandler.GetGroupAccess)
				groups.DELETE("/:id/access/:accessId", groupHandler.RevokeGroupAccess)
				groups.GET("/:id/team-access", groupHandler.GetGroupTeamAccess)
				groups.POST("/:id/team-access", groupHandler.GrantGroupTeamAccess)
				groups.DELETE("/:id/team-access/:accessId", groupHandler.RevokeGroupTeamAccess)
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
