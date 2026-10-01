package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/XpertaDK/batter/internal/api/middleware"
	"github.com/XpertaDK/batter/internal/device"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeviceHandler handles device-related API requests.
type DeviceHandler struct {
	deviceManager   *device.Manager
	screenshotCache *device.ScreenshotCache
	db              *pgxpool.Pool
	logger          *slog.Logger
}

// NewDeviceHandler creates a new device handler.
func NewDeviceHandler(dm *device.Manager, db *pgxpool.Pool, logger *slog.Logger) *DeviceHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeviceHandler{
		deviceManager:   dm,
		screenshotCache: dm.ScreenshotCache(),
		db:              db,
		logger:          logger.With("handler", "devices"),
	}
}

// DeviceHealth returns the status of device prerequisites (ADB, scrcpy, connected devices).
func (h *DeviceHandler) DeviceHealth(c *gin.Context) {
	status := h.deviceManager.Health(c.Request.Context())
	c.JSON(http.StatusOK, status)
}

// mergeDevices fetches all registered devices from DB, merges with live ADB state,
// overlays session info, and filters by RBAC.
func (h *DeviceHandler) mergeDevices(c *gin.Context) ([]device.DeviceInfo, error) {
	ctx := c.Request.Context()

	// 1. Fetch all registered devices from DB
	rows, err := h.db.Query(ctx,
		`SELECT serial, model, product, COALESCE(nickname, ''), COALESCE(android_version, ''),
		        status, last_seen_at
		 FROM devices ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type dbDevice struct {
		Serial         string
		Model          string
		Product        string
		Nickname       string
		AndroidVersion string
		Status         string
		LastSeenAt     *time.Time
	}
	var dbDevices []dbDevice
	dbMap := make(map[string]*dbDevice)

	for rows.Next() {
		var d dbDevice
		if err := rows.Scan(&d.Serial, &d.Model, &d.Product, &d.Nickname, &d.AndroidVersion,
			&d.Status, &d.LastSeenAt); err != nil {
			continue
		}
		dbDevices = append(dbDevices, d)
		dbMap[d.Serial] = &dbDevices[len(dbDevices)-1]
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. Get live ADB state
	adbDevices, err := h.deviceManager.ListDevices(ctx)
	if err != nil {
		h.logger.Warn("failed to list ADB devices, using DB only", "error", err)
		adbDevices = nil
	}

	adbMap := make(map[string]*device.DeviceInfo)
	for i := range adbDevices {
		adbMap[adbDevices[i].Serial] = &adbDevices[i]
	}

	// 3. Merge: compute status for each DB device
	now := time.Now()
	var statusUpdates []struct {
		serial string
		status string
	}

	var result []device.DeviceInfo
	for i := range dbDevices {
		d := &dbDevices[i]
		info := device.DeviceInfo{
			Serial:         d.Serial,
			Model:          d.Model,
			Product:        d.Product,
			Nickname:       d.Nickname,
			AndroidVersion: d.AndroidVersion,
			LastSeenAt:     d.LastSeenAt,
			Status:         d.Status,
		}

		if adb, ok := adbMap[d.Serial]; ok {
			// Device is visible to ADB
			info.State = adb.State
			info.HasSession = adb.HasSession
			info.Width = adb.Width
			info.Height = adb.Height
			info.SessionTier = adb.SessionTier

			// Update model/product from ADB if DB has empty values
			if info.Model == "" && adb.Model != "" {
				info.Model = adb.Model
			}
			if info.Product == "" && adb.Product != "" {
				info.Product = adb.Product
			}

			// Compute status from ADB state
			var newStatus string
			switch adb.State {
			case "device":
				newStatus = "connected"
			case "offline":
				newStatus = "offline"
			case "unauthorized":
				newStatus = "unauthorized"
			default:
				newStatus = "disconnected"
			}

			if newStatus != d.Status {
				statusUpdates = append(statusUpdates, struct {
					serial string
					status string
				}{d.Serial, newStatus})
			}
			info.Status = newStatus
			t := now
			info.LastSeenAt = &t
		} else {
			// Not visible to ADB
			info.State = ""
			info.Status = "disconnected"
			if d.Status != "disconnected" {
				statusUpdates = append(statusUpdates, struct {
					serial string
					status string
				}{d.Serial, "disconnected"})
			}
		}

		result = append(result, info)
	}

	// 4. Batch-update changed statuses in DB
	for _, u := range statusUpdates {
		_, err := h.db.Exec(ctx,
			"UPDATE devices SET status = $1, last_seen_at = $2, updated_at = now() WHERE serial = $3",
			u.status, now, u.serial,
		)
		if err != nil {
			h.logger.Warn("failed to update device status", "serial", u.serial, "error", err)
		}
	}

	// 5. RBAC filter
	userID, _ := c.Get(middleware.ContextKeyUserID)
	role, _ := c.Get(middleware.ContextKeyRole)
	accessibleSerials, err := middleware.GetAccessibleSerials(c, h.db, userID.(string), role.(string))
	if err != nil {
		return nil, err
	}

	if accessibleSerials != nil {
		// Non-admin: filter to accessible devices
		accessSet := make(map[string]bool, len(accessibleSerials))
		for _, s := range accessibleSerials {
			accessSet[s] = true
		}
		var filtered []device.DeviceInfo
		for _, d := range result {
			if accessSet[d.Serial] {
				filtered = append(filtered, d)
			}
		}
		result = filtered
	}

	if result == nil {
		result = []device.DeviceInfo{}
	}

	return result, nil
}

// DiscoverDevices returns raw ADB-visible devices for the registration wizard.
// This is intentionally separate from ListDevices which only returns DB-registered devices.
func (h *DeviceHandler) DiscoverDevices(c *gin.Context) {
	adbDevices, err := h.deviceManager.ListDevices(c.Request.Context())
	if err != nil {
		h.logger.Error("failed to discover devices", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to discover devices"})
		return
	}
	if adbDevices == nil {
		adbDevices = []device.DeviceInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"devices": adbDevices})
}

// ListDevices returns all registered devices merged with live ADB state.
func (h *DeviceHandler) ListDevices(c *gin.Context) {
	devices, err := h.mergeDevices(c)
	if err != nil {
		h.logger.Error("failed to list devices", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list devices"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"devices": devices})
}

// GetDevice returns info for a single registered device.
func (h *DeviceHandler) GetDevice(c *gin.Context) {
	serial := c.Param("serial")

	devices, err := h.mergeDevices(c)
	if err != nil {
		h.logger.Error("failed to list devices", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list devices"})
		return
	}

	for _, d := range devices {
		if d.Serial == serial {
			c.JSON(http.StatusOK, d)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
}

// RegisterDeviceRequest represents a request to register a new device.
type RegisterDeviceRequest struct {
	Serial   string `json:"serial" binding:"required"`
	Nickname string `json:"nickname"`
}

// RegisterDevice inserts a new device into the database.
func (h *DeviceHandler) RegisterDevice(c *gin.Context) {
	var req RegisterDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial is required"})
		return
	}

	userID, _ := c.Get(middleware.ContextKeyUserID)

	_, err := h.db.Exec(c.Request.Context(),
		`INSERT INTO devices (serial, nickname, registered_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (serial) DO UPDATE SET
		   nickname = COALESCE(NULLIF($2, ''), devices.nickname),
		   registered_by = COALESCE(devices.registered_by, $3),
		   updated_at = now()`,
		req.Serial, req.Nickname, userID,
	)
	if err != nil {
		h.logger.Error("failed to register device", "serial", req.Serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register device"})
		return
	}

	h.logger.Info("device registered", "serial", req.Serial, "registered_by", userID)
	c.JSON(http.StatusCreated, gin.H{"serial": req.Serial, "message": "device registered"})
}

// ValidateDevice checks whether a device is reachable via ADB.
func (h *DeviceHandler) ValidateDevice(c *gin.Context) {
	serial := c.Param("serial")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	state, err := h.deviceManager.ValidateDevice(ctx, serial)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"serial":    serial,
			"reachable": false,
			"state":     "",
			"error":     err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"serial":    serial,
		"reachable": true,
		"state":     state,
	})
}

// ProbeDevice retrieves device properties via ADB and updates the DB record.
func (h *DeviceHandler) ProbeDevice(c *gin.Context) {
	serial := c.Param("serial")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	props, err := h.deviceManager.GetDeviceProperties(ctx, serial)
	if err != nil {
		h.logger.Error("failed to probe device", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to probe device"})
		return
	}

	model := props["ro.product.model"]
	product := props["ro.product.name"]
	androidVersion := props["ro.build.version.release"]

	// Update DB record
	_, err = h.db.Exec(c.Request.Context(),
		`UPDATE devices SET model = $1, product = $2, android_version = $3, updated_at = now()
		 WHERE serial = $4`,
		model, product, androidVersion, serial,
	)
	if err != nil {
		h.logger.Warn("failed to update device properties in DB", "serial", serial, "error", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"serial":          serial,
		"model":           model,
		"product":         product,
		"android_version": androidVersion,
	})
}

// UpdateDeviceRequest represents a request to update a device.
type UpdateDeviceRequest struct {
	Nickname string `json:"nickname"`
	Model    string `json:"model"`
	Product  string `json:"product"`
}

// UpdateDevice updates a device's nickname and optionally model/product.
func (h *DeviceHandler) UpdateDevice(c *gin.Context) {
	serial := c.Param("serial")

	var req UpdateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	_, err := h.db.Exec(c.Request.Context(),
		`UPDATE devices SET
		   nickname = $1,
		   model = CASE WHEN $2 = '' THEN model ELSE $2 END,
		   product = CASE WHEN $3 = '' THEN product ELSE $3 END,
		   updated_at = now()
		 WHERE serial = $4`,
		req.Nickname, req.Model, req.Product, serial,
	)
	if err != nil {
		h.logger.Error("failed to update device", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update device"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "device updated"})
}

// DeleteDevice removes a device from the database and stops any active session.
func (h *DeviceHandler) DeleteDevice(c *gin.Context) {
	serial := c.Param("serial")

	// Stop session if running
	_ = h.deviceManager.StopSession(serial)

	_, err := h.db.Exec(c.Request.Context(), "DELETE FROM devices WHERE serial = $1", serial)
	if err != nil {
		h.logger.Error("failed to delete device", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete device"})
		return
	}

	// Clean up cached screenshot
	if h.screenshotCache != nil {
		if err := h.screenshotCache.Delete(serial); err != nil {
			h.logger.Warn("failed to delete cached screenshot", "serial", serial, "error", err)
		}
	}

	h.logger.Info("device deleted", "serial", serial)
	c.JSON(http.StatusOK, gin.H{"message": "device deleted"})
}

// StartSessionRequest represents a request to start a scrcpy session.
type StartSessionRequest struct {
	MaxSize int `json:"max_size"`
	MaxFPS  int `json:"max_fps"`
}

// StartSession starts a scrcpy session for a device.
func (h *DeviceHandler) StartSession(c *gin.Context) {
	serial := c.Param("serial")

	var req StartSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = StartSessionRequest{}
	}

	session, err := h.deviceManager.StartSession(c.Request.Context(), serial, device.SessionOptions{
		MaxSize: req.MaxSize,
		MaxFPS:  req.MaxFPS,
	})
	if err != nil {
		h.logger.Error("failed to start session", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"serial":       session.Serial,
		"device_name":  session.DeviceName,
		"width":        session.Width,
		"height":       session.Height,
		"session_tier": h.deviceManager.GetSessionTier(serial),
	})
}

// StopSession stops a running scrcpy session.
func (h *DeviceHandler) StopSession(c *gin.Context) {
	serial := c.Param("serial")

	if err := h.deviceManager.StopSession(serial); err != nil {
		h.logger.Error("failed to stop session", "serial", serial, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "session stopped"})
}

// UpgradeSession switches a device session to full quality.
func (h *DeviceHandler) UpgradeSession(c *gin.Context) {
	serial := c.Param("serial")

	session, err := h.deviceManager.UpgradeSession(c.Request.Context(), serial)
	if err != nil {
		h.logger.Error("failed to upgrade session", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"serial":       session.Serial,
		"width":        session.Width,
		"height":       session.Height,
		"session_tier": h.deviceManager.GetSessionTier(serial),
	})
}

// DowngradeSession switches a device session back to thumbnail quality.
func (h *DeviceHandler) DowngradeSession(c *gin.Context) {
	serial := c.Param("serial")

	session, err := h.deviceManager.DowngradeSession(c.Request.Context(), serial)
	if err != nil {
		h.logger.Error("failed to downgrade session", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"serial":       session.Serial,
		"width":        session.Width,
		"height":       session.Height,
		"session_tier": h.deviceManager.GetSessionTier(serial),
	})
}

// WakeScreen wakes the device screen via ADB.
func (h *DeviceHandler) WakeScreen(c *gin.Context) {
	serial := c.Param("serial")

	if err := h.deviceManager.WakeScreen(c.Request.Context(), serial); err != nil {
		h.logger.Error("failed to wake screen", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to wake screen"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "screen woken"})
}

// PushFile pushes an uploaded file to the device's /sdcard/Download/ directory.
func (h *DeviceHandler) PushFile(c *gin.Context) {
	serial := c.Param("serial")

	// Limit request body to 500MB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 500*1024*1024)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	// Sanitize filename — strip path components
	filename := filepath.Base(header.Filename)
	if filename == "." || filename == "/" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "batter-push-*")
	if err != nil {
		h.logger.Error("failed to create temp file", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process file"})
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.ReadFrom(file); err != nil {
		tmpFile.Close()
		h.logger.Error("failed to write temp file", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process file"})
		return
	}
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	remotePath := "/sdcard/Download/" + filename
	if err := h.deviceManager.PushFile(ctx, serial, tmpPath, remotePath); err != nil {
		h.logger.Error("failed to push file", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to push file to device"})
		return
	}

	h.logger.Info("file pushed to device", "serial", serial, "filename", filename)
	c.JSON(http.StatusOK, gin.H{"message": "file pushed", "remote_path": remotePath})
}

// InstallAPK installs an uploaded APK file on the device.
func (h *DeviceHandler) InstallAPK(c *gin.Context) {
	serial := c.Param("serial")

	// Limit request body to 500MB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 500*1024*1024)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "APK file is required"})
		return
	}
	defer file.Close()

	filename := filepath.Base(header.Filename)
	if !strings.HasSuffix(strings.ToLower(filename), ".apk") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file must be an APK"})
		return
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "batter-install-*.apk")
	if err != nil {
		h.logger.Error("failed to create temp file", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process file"})
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.ReadFrom(file); err != nil {
		tmpFile.Close()
		h.logger.Error("failed to write temp file", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process file"})
		return
	}
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	if err := h.deviceManager.InstallAPK(ctx, serial, tmpPath); err != nil {
		h.logger.Error("failed to install APK", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("APK installed on device", "serial", serial, "filename", filename)
	c.JSON(http.StatusOK, gin.H{"message": "APK installed", "filename": filename})
}

// Screenshot captures the device screen. Falls back to a cached screenshot if live capture fails.
func (h *DeviceHandler) Screenshot(c *gin.Context) {
	serial := c.Param("serial")

	png, err := h.deviceManager.Screenshot(c.Request.Context(), serial)
	if err != nil {
		// Fall back to cached screenshot
		if h.screenshotCache != nil {
			if cached, cacheErr := h.screenshotCache.Load(serial); cacheErr == nil {
				c.Data(http.StatusOK, "image/png", cached)
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "no screenshot available"})
		return
	}

	c.Data(http.StatusOK, "image/png", png)
}
