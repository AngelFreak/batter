package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/XpertaDK/batter/internal/tether"
	"github.com/XpertaDK/batter/internal/vpn"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TetherHandler assigns devices' VPN profiles. A device is tethered (uses
// this host's internet over USB) exactly when it has a profile.
type TetherHandler struct {
	db     *pgxpool.Pool
	tether *tether.Controller // nil when gnirehtet isn't installed
	vpn    *vpn.Service
	logger *slog.Logger
}

// NewTetherHandler creates a tether handler. ctrl may be nil.
func NewTetherHandler(db *pgxpool.Pool, ctrl *tether.Controller, svc *vpn.Service, logger *slog.Logger) *TetherHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TetherHandler{db: db, tether: ctrl, vpn: svc, logger: logger.With("handler", "tether")}
}

// tetherApplyTimeout covers installing the client app on first enable.
const tetherApplyTimeout = 90 * time.Second

// SetTether sets the device's VPN profile ({"profile_id": "<uuid>"}) or
// turns tethering off ({"profile_id": null}), then applies it. The setting
// is kept even when applying fails (e.g. the device is unplugged); the
// reconcile loop applies it once the device is connected. Apply failures
// are reported in apply_error rather than as an HTTP error.
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

	var target tether.Target
	if profileID != nil {
		if target, err = h.vpn.Target(c.Request.Context(), *profileID); errors.Is(err, vpn.ErrNotFound) {
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
	if err := h.apply(c.Request.Context(), serial, profileID != nil, target); err != nil {
		h.logger.Warn("tether setting stored but not applied", "serial", serial, "profile", profileID, "error", err)
		resp["apply_error"] = err.Error()
	}
	c.JSON(http.StatusOK, resp)
}

func (h *TetherHandler) apply(ctx context.Context, serial string, on bool, target tether.Target) error {
	if h.tether == nil {
		return errTetherUnavailable
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tetherApplyTimeout)
	defer cancel()
	if on {
		return h.tether.Enable(ctx, serial, target)
	}
	return h.tether.Disable(ctx, serial)
}

var errTetherUnavailable = errors.New("reverse tethering is unavailable on this server (gnirehtet not installed)")
