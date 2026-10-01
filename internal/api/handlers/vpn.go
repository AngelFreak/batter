package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/XpertaDK/batter/internal/vpn"
	"github.com/gin-gonic/gin"
)

// VPNHandler manages VPN profiles (admin only, except Names). Responses
// describe configs without their private or preshared keys.
type VPNHandler struct {
	vpn    *vpn.Service
	lan    PhoneLAN // nil without a phone network
	logger *slog.Logger
}

// NewVPNHandler creates a VPN handler. lan may be nil.
func NewVPNHandler(svc *vpn.Service, lan PhoneLAN, logger *slog.Logger) *VPNHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &VPNHandler{vpn: svc, lan: lan, logger: logger.With("handler", "vpn")}
}

// reload re-applies the phone LAN's routing (profiles' DNS servers and
// marks) after a profile change. Without a phone network there's nothing
// to do.
func (h *VPNHandler) reload(c *gin.Context) {
	if h.lan == nil {
		return
	}
	if err := reloadLAN(c.Request.Context(), h.lan); err != nil {
		h.logger.Error("phone LAN routing not re-applied after a VPN profile change", "error", err)
	}
}

// ProfileRequest is the body of POST and PUT /vpn/profiles. On PUT, omitted
// fields are kept (so the config, and its key, needn't be sent again).
type ProfileRequest struct {
	Name    *string `json:"name"`
	Config  *string `json:"config"`
	Enabled *bool   `json:"enabled"`
}

// ListProfiles returns every profile with its status.
func (h *VPNHandler) ListProfiles(c *gin.Context) {
	profiles, err := h.vpn.List(c.Request.Context())
	if err != nil {
		h.fail(c, "list VPN profiles", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"profiles": profiles})
}

// ProfileNames lists profile ids and names for anyone choosing a phone's
// profile.
func (h *VPNHandler) ProfileNames(c *gin.Context) {
	names, err := h.vpn.Names(c.Request.Context())
	if err != nil {
		h.fail(c, "list VPN profile names", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"profiles": names})
}

// GetProfile returns one profile with its status.
func (h *VPNHandler) GetProfile(c *gin.Context) {
	p, err := h.vpn.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "get VPN profile", err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// CreateProfile stores and applies a new profile (enabled unless told
// otherwise). Failing to apply isn't an HTTP error; apply_error reports it.
func (h *VPNHandler) CreateProfile(c *gin.Context) {
	var req ProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == nil || req.Config == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `"name" and "config" are required`})
		return
	}
	enabled := req.Enabled == nil || *req.Enabled
	p, err := h.vpn.Create(c.Request.Context(), *req.Name, *req.Config, enabled)
	if err != nil {
		h.fail(c, "create VPN profile", err)
		return
	}
	h.reload(c)
	c.JSON(http.StatusCreated, p)
}

// UpdateProfile changes a profile's name, config or enabled flag.
func (h *VPNHandler) UpdateProfile(c *gin.Context) {
	var req ProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	p, err := h.vpn.Update(c.Request.Context(), c.Param("id"),
		vpn.ProfileUpdate{Name: req.Name, Config: req.Config, Enabled: req.Enabled})
	if err != nil {
		h.fail(c, "update VPN profile", err)
		return
	}
	h.reload(c)
	c.JSON(http.StatusOK, p)
}

// DeleteProfile removes a profile; its phones are left without internet.
func (h *VPNHandler) DeleteProfile(c *gin.Context) {
	serials, err := h.vpn.Delete(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "delete VPN profile", err)
		return
	}
	h.reload(c)
	c.JSON(http.StatusOK, gin.H{"devices_turned_off": serials})
}

// CheckExitIP reports the public IP the profile's phones exit from, by
// making a request routed like them. An error with the profile enabled
// usually means its tunnel is down and the kill switch is holding.
func (h *VPNHandler) CheckExitIP(c *gin.Context) {
	ip, err := h.vpn.CheckExit(c.Request.Context(), c.Param("id"))
	if errors.Is(err, vpn.ErrNotFound) {
		h.fail(c, "check exit IP", err)
		return
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"exit_ip": ip})
}

// fail maps service errors to responses.
func (h *VPNHandler) fail(c *gin.Context, what string, err error) {
	var cfgErr *vpn.ConfigError
	switch {
	case errors.As(err, &cfgErr):
		c.JSON(http.StatusBadRequest, gin.H{"error": cfgErr.Error()})
	case errors.Is(err, vpn.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		h.logger.Error("failed to "+what, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to " + what})
	}
}
