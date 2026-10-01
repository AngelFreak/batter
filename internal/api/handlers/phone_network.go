package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/XpertaDK/batter/internal/lan"
	"github.com/gin-gonic/gin"
)

// PhoneNetworkHandler lets admins choose the box NIC the phone network uses,
// or turn it off.
type PhoneNetworkHandler struct {
	lan    PhoneLAN
	logger *slog.Logger
}

// NewPhoneNetworkHandler creates the handler.
func NewPhoneNetworkHandler(l PhoneLAN, logger *slog.Logger) *PhoneNetworkHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &PhoneNetworkHandler{lan: l, logger: logger.With("handler", "phone-network")}
}

// Status reports the phone network's state, its port and the devices on it.
func (h *PhoneNetworkHandler) Status(c *gin.Context) {
	if h.lan == nil {
		c.JSON(http.StatusOK, lan.Status{State: lan.StateUnavailable, Unavailable: lan.ErrNoHostNet.Error(), Clients: []lan.Client{}})
		return
	}
	st, err := h.lan.Status(c.Request.Context())
	if err != nil {
		h.logger.Error("phone network status", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the phone network's state"})
		return
	}
	c.JSON(http.StatusOK, st)
}

// Interfaces lists the box's network ports for the picker.
func (h *PhoneNetworkHandler) Interfaces(c *gin.Context) {
	if h.lan == nil {
		c.JSON(http.StatusConflict, gin.H{"error": lan.ErrNoHostNet.Error()})
		return
	}
	nics, err := h.lan.Interfaces(c.Request.Context())
	if errors.Is(err, lan.ErrNoHostNet) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		h.logger.Error("list the box's network ports", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list the box's network ports"})
		return
	}
	if nics == nil {
		nics = []lan.HostNIC{}
	}
	c.JSON(http.StatusOK, gin.H{"interfaces": nics})
}

// SetPort chooses the port ({"mac": "aa:bb:..."}) or turns the phone
// network off ({"mac": null}), and applies it at once.
func (h *PhoneNetworkHandler) SetPort(c *gin.Context) {
	var body map[string]json.RawMessage
	var mac *string
	err := c.ShouldBindJSON(&body)
	raw, ok := body["mac"]
	if err != nil || !ok || json.Unmarshal(raw, &mac) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `"mac" (a network port's MAC address, or null for off) is required`})
		return
	}
	if h.lan == nil {
		c.JSON(http.StatusConflict, gin.H{"error": lan.ErrNoHostNet.Error()})
		return
	}
	want := ""
	if mac != nil {
		want = *mac
	}
	// Moving a NIC and re-fencing must finish even if the caller goes away.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 2*time.Minute)
	defer cancel()
	st, err := h.lan.SetPort(ctx, want)
	var portErr *lan.PortError
	switch {
	case errors.As(err, &portErr):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, lan.ErrNoHostNet):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case err != nil:
		h.logger.Error("set the phone network's port", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to change the phone network"})
	default:
		h.logger.Info("phone network port changed", "mac", want, "state", st.State)
		c.JSON(http.StatusOK, st)
	}
}
