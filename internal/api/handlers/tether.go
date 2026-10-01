package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/XpertaDK/batter/internal/lan"
	"github.com/XpertaDK/batter/internal/vpn"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PhoneLAN is the phone network's controller (see internal/lan).
type PhoneLAN interface {
	// Reload re-applies the phones' routing after an assignment or profile
	// change; lan.ErrOff when the phone network is off.
	Reload(ctx context.Context) error
	// Status, Interfaces and SetPort back the admin's port picker.
	Status(ctx context.Context) (lan.Status, error)
	Interfaces(ctx context.Context) ([]lan.HostNIC, error)
	SetPort(ctx context.Context, mac string) (lan.Status, error)
	// Provision switches a USB-connected phone's adb to TCP, ready for its
	// move to an ethernet adapter.
	Provision(ctx context.Context, serial string) error
	// NeedsReprovision reports a phone on the LAN (at addr) whose adb over
	// TCP is off, e.g. after a reboot.
	NeedsReprovision(serial string) (addr netip.Addr, ok bool)
}

// errNoPhoneLAN is reported when the phone network can't be used at all.
var errNoPhoneLAN = lan.ErrOff

// lanApplyTimeout bounds re-applying the phone LAN's routing.
const lanApplyTimeout = 30 * time.Second

// reloadLAN re-applies the phone LAN's routing even if the caller goes
// away: the database already changed.
func reloadLAN(ctx context.Context, lan PhoneLAN) error {
	if lan == nil {
		return errNoPhoneLAN
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lanApplyTimeout)
	defer cancel()
	return lan.Reload(ctx)
}

// TetherHandler assigns devices' VPN profiles. A phone on the phone LAN has
// internet exactly when it has a profile, and only through it.
type TetherHandler struct {
	db     *pgxpool.Pool
	lan    PhoneLAN // nil without a phone network
	vpn    *vpn.Service
	logger *slog.Logger
}

// NewTetherHandler creates a tether handler. lan may be nil.
func NewTetherHandler(db *pgxpool.Pool, lan PhoneLAN, svc *vpn.Service, logger *slog.Logger) *TetherHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TetherHandler{db: db, lan: lan, vpn: svc, logger: logger.With("handler", "tether")}
}

// SetTether sets the device's VPN profile ({"profile_id": "<uuid>"}) or
// takes its internet away ({"profile_id": null}), then re-applies the phone
// LAN's routing. The setting is kept even when applying fails; apply_error
// reports why rather than an HTTP error.
func (h *TetherHandler) SetTether(c *gin.Context) {
	serial := c.Param("serial")

	// A missing profile_id must not read as null (off), so look for the key.
	var body map[string]json.RawMessage
	var profileID *string
	err := c.ShouldBindJSON(&body)
	raw, ok := body["profile_id"]
	if err != nil || !ok || json.Unmarshal(raw, &profileID) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `"profile_id" (a profile id, or null for off) is required`})
		return
	}

	if profileID != nil {
		if err := h.vpn.Exists(c.Request.Context(), *profileID); errors.Is(err, vpn.ErrNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown VPN profile"})
			return
		} else if err != nil {
			h.logger.Error("failed to look up VPN profile", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update device"})
			return
		}
	}

	tag, err := h.db.Exec(c.Request.Context(),
		"UPDATE devices SET vpn_profile_id = $1, updated_at = now() WHERE serial = $2",
		profileID, serial,
	)
	if err != nil {
		h.logger.Error("failed to store tether setting", "serial", serial, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update device"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	resp := gin.H{"serial": serial, "vpn_profile_id": profileID}
	if err := reloadLAN(c.Request.Context(), h.lan); err != nil {
		h.logger.Warn("phone internet setting stored but not applied", "serial", serial, "profile", profileID, "error", err)
		resp["apply_error"] = err.Error()
	}
	c.JSON(http.StatusOK, resp)
}
