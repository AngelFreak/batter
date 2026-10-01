package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/XpertaDK/batter/internal/tether"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TetherHandler switches reverse tethering on and off per device.
type TetherHandler struct {
	db     *pgxpool.Pool
	tether *tether.Controller // nil when gnirehtet isn't installed
	logger *slog.Logger
}

// NewTetherHandler creates a tether handler. ctrl may be nil.
func NewTetherHandler(db *pgxpool.Pool, ctrl *tether.Controller, logger *slog.Logger) *TetherHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TetherHandler{db: db, tether: ctrl, logger: logger.With("handler", "tether")}
}

// SetTetherRequest is the body of PUT /devices/:serial/tether.
type SetTetherRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// applyTimeout covers installing the client app on first enable.
const applyTimeout = 90 * time.Second

// SetTether stores the device's reverse tethering setting and applies it.
// The setting is kept even when applying fails (e.g. the device is
// unplugged); the reconcile loop applies it once the device is connected.
// Apply failures are reported in apply_error rather than as an HTTP error.
func (h *TetherHandler) SetTether(c *gin.Context) {
	serial := c.Param("serial")

	var req SetTetherRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `"enabled" (bool) is required`})
		return
	}
	enabled := *req.Enabled

	tag, err := h.db.Exec(c.Request.Context(),
		"UPDATE devices SET reverse_tether = $1, updated_at = now() WHERE serial = $2",
		enabled, serial,
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

	resp := gin.H{"serial": serial, "reverse_tether": enabled}
	if err := h.apply(c.Request.Context(), serial, enabled); err != nil {
		h.logger.Warn("tether setting stored but not applied", "serial", serial, "enabled", enabled, "error", err)
		resp["apply_error"] = err.Error()
	}
	c.JSON(http.StatusOK, resp)
}

// TetherEnabledSerials returns the devices whose reverse tethering is on.
func TetherEnabledSerials(ctx context.Context, db *pgxpool.Pool) ([]string, error) {
	rows, err := db.Query(ctx, "SELECT serial FROM devices WHERE reverse_tether ORDER BY serial")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (h *TetherHandler) apply(ctx context.Context, serial string, enabled bool) error {
	if h.tether == nil {
		return errTetherUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, applyTimeout)
	defer cancel()
	if enabled {
		return h.tether.Enable(ctx, serial)
	}
	return h.tether.Disable(ctx, serial)
}

var errTetherUnavailable = errors.New("reverse tethering is unavailable on this server (gnirehtet not installed)")
