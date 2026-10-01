package handlers

import (
	"errors"
	"net/http"

	"github.com/XpertaDK/batter/internal/device"
	"github.com/gin-gonic/gin"
)

// ScreenLock reports the device's lock-screen state (none, PIN, ...).
func (h *DeviceHandler) ScreenLock(c *gin.Context) {
	state, err := h.deviceManager.ScreenLock(c.Request.Context(), c.Param("serial"))
	if err != nil {
		h.logger.Warn("read screen lock", "serial", c.Param("serial"), "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "couldn't read the phone's screen lock; is it connected?"})
		return
	}
	c.JSON(http.StatusOK, state)
}

// RemoveScreenLockRequest carries the current PIN/password/pattern. It's
// sent in the body (never the URL), so it stays out of request and audit logs.
type RemoveScreenLockRequest struct {
	Credential string `json:"credential"`
}

// RemoveScreenLock clears the device's lock so remote users aren't blocked by
// it. Intended for phones dedicated to Batter.
func (h *DeviceHandler) RemoveScreenLock(c *gin.Context) {
	serial := c.Param("serial")
	var req RemoveScreenLockRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Credential) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	state, err := h.deviceManager.RemoveScreenLock(c.Request.Context(), serial, req.Credential)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, state)
	case errors.Is(err, device.ErrCredentialRequired):
		c.JSON(http.StatusBadRequest, gin.H{"error": "enter the phone's current PIN, password or pattern"})
	case errors.Is(err, device.ErrWrongCredential):
		h.logger.Warn("screen lock removal: wrong credential", "serial", serial)
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "that didn't match; it counts as a failed unlock attempt on the phone",
		})
	default:
		// err never contains the credential (see device.RemoveScreenLock).
		h.logger.Warn("screen lock removal failed", "serial", serial, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "couldn't remove the screen lock; is the phone connected and unlocked?"})
	}
}
