package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XpertaDK/batter/internal/vpn"
	"github.com/gin-gonic/gin"
)

// VPNHandler manages the WireGuard tunnel tethered devices use (admin only).
// Responses describe the config without its private or preshared keys.
type VPNHandler struct {
	vpn    *vpn.Manager // nil when unavailable
	logger *slog.Logger
}

// NewVPNHandler creates a VPN handler. m may be nil.
func NewVPNHandler(m *vpn.Manager, logger *slog.Logger) *VPNHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &VPNHandler{vpn: m, logger: logger.With("handler", "vpn")}
}

// SetVPNRequest is the body of PUT /vpn. Config may be omitted to change
// only Enabled.
type SetVPNRequest struct {
	Config  string `json:"config"`
	Enabled *bool  `json:"enabled" binding:"required"`
}

func (h *VPNHandler) available(c *gin.Context) bool {
	if h.vpn == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "VPN management is unavailable on this server"})
		return false
	}
	return true
}

// GetVPN returns the redacted config and the tunnel's status.
func (h *VPNHandler) GetVPN(c *gin.Context) {
	if !h.available(c) {
		return
	}
	c.JSON(http.StatusOK, h.vpn.Info(c.Request.Context()))
}

// SetVPN saves the config and/or enabled flag and applies them. Failing to
// apply isn't an HTTP error; the response's apply_error reports it.
func (h *VPNHandler) SetVPN(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var req SetVPNRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `"enabled" (bool) is required`})
		return
	}
	if err := h.vpn.Set(c.Request.Context(), req.Config, *req.Enabled); err != nil {
		var cfgErr *vpn.ConfigError
		if errors.As(err, &cfgErr) {
			c.JSON(http.StatusBadRequest, gin.H{"error": cfgErr.Error()})
			return
		}
		h.logger.Error("failed to save VPN config", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save VPN config"})
		return
	}
	h.logger.Info("VPN config updated", "enabled", *req.Enabled, "new_config", req.Config != "")
	c.JSON(http.StatusOK, h.vpn.Info(c.Request.Context()))
}

// DeleteVPN removes the config; tethered devices go back to the host's
// normal internet.
func (h *VPNHandler) DeleteVPN(c *gin.Context) {
	if !h.available(c) {
		return
	}
	if err := h.vpn.Delete(c.Request.Context()); err != nil {
		h.logger.Error("failed to delete VPN config", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete VPN config"})
		return
	}
	h.logger.Info("VPN config deleted")
	c.JSON(http.StatusOK, h.vpn.Info(c.Request.Context()))
}

// CheckExitIP reports the public IP tethered devices' traffic leaves from,
// by making a request as the relay's uid. An error here with the VPN
// enabled usually means the tunnel is down and the kill switch is holding.
func (h *VPNHandler) CheckExitIP(c *gin.Context) {
	if !h.available(c) {
		return
	}
	if h.vpn.CheckExit == nil {
		c.JSON(http.StatusOK, gin.H{"error": "exit IP check is unavailable on this server"})
		return
	}
	ip, err := h.vpn.CheckExit(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"exit_ip": ip})
}
