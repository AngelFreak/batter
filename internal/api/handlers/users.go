package handlers

import (
	"log/slog"
	"net/http"

	"github.com/XpertaDK/batter/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserHandler handles user management API requests (admin only).
type UserHandler struct {
	db     *pgxpool.Pool
	logger *slog.Logger
}

// NewUserHandler creates a new user handler.
func NewUserHandler(db *pgxpool.Pool, logger *slog.Logger) *UserHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &UserHandler{
		db:     db,
		logger: logger.With("handler", "users"),
	}
}

// ListUsers returns all users.
func (h *UserHandler) ListUsers(c *gin.Context) {
	rows, err := h.db.Query(c.Request.Context(),
		"SELECT id, username, COALESCE(email, ''), COALESCE(display_name, username), role, is_active, last_login_at, created_at FROM users ORDER BY created_at DESC",
	)
	if err != nil {
		h.logger.Error("failed to list users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}
	defer rows.Close()

	var users []gin.H
	for rows.Next() {
		var id, username, email, displayName, role string
		var isActive bool
		var lastLogin, createdAt interface{}
		if err := rows.Scan(&id, &username, &email, &displayName, &role, &isActive, &lastLogin, &createdAt); err != nil {
			continue
		}
		users = append(users, gin.H{
			"id":            id,
			"username":      username,
			"email":         email,
			"display_name":  displayName,
			"role":          role,
			"is_active":     isActive,
			"last_login_at": lastLogin,
			"created_at":    createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("failed to read users", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	if users == nil {
		users = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"users": users})
}

// CreateUserRequest represents a request to create a user.
type CreateUserRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role" binding:"required"`
}

// CreateUser creates a new user (admin only).
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username, password, and role are required"})
		return
	}

	// Validate role
	if req.Role != "admin" && req.Role != "operator" && req.Role != "viewer" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be admin, operator, or viewer"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Username
	}

	var userID string
	err = h.db.QueryRow(c.Request.Context(),
		"INSERT INTO users (username, email, password_hash, display_name, role) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		req.Username, req.Email, hash, displayName, req.Role,
	).Scan(&userID)
	if err != nil {
		h.logger.Error("failed to create user", "error", err)
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	h.logger.Info("user created", "username", req.Username, "role", req.Role, "created_by", c.GetString("username"))

	c.JSON(http.StatusCreated, gin.H{
		"id":       userID,
		"username": req.Username,
		"role":     req.Role,
	})
}

// GrantAccessRequest represents a request to grant device/group access to a user.
type GrantAccessRequest struct {
	DeviceSerial string `json:"device_serial"`
	GroupID      string `json:"group_id"`
	Permission   string `json:"permission" binding:"required"`
}

// GrantAccess grants a user access to a device or group.
func (h *UserHandler) GrantAccess(c *gin.Context) {
	targetUserID := c.Param("id")
	grantedBy, _ := c.Get("user_id")

	var req GrantAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "permission is required"})
		return
	}

	if req.Permission != "view" && req.Permission != "control" && req.Permission != "manage" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "permission must be view, control, or manage"})
		return
	}

	if req.DeviceSerial == "" && req.GroupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "either device_serial or group_id is required"})
		return
	}

	if req.DeviceSerial != "" && req.GroupID != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only one of device_serial or group_id can be set"})
		return
	}

	if req.DeviceSerial != "" {
		_, err := h.db.Exec(c.Request.Context(),
			"INSERT INTO user_device_access (user_id, device_serial, permission, granted_by) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING",
			targetUserID, req.DeviceSerial, req.Permission, grantedBy,
		)
		if err != nil {
			h.logger.Error("failed to grant device access", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to grant access"})
			return
		}
	} else {
		_, err := h.db.Exec(c.Request.Context(),
			"INSERT INTO user_device_access (user_id, group_id, permission, granted_by) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING",
			targetUserID, req.GroupID, req.Permission, grantedBy,
		)
		if err != nil {
			h.logger.Error("failed to grant group access", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to grant access"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "access granted"})
}

// UpdateUserRequest represents a request to update a user.
type UpdateUserRequest struct {
	Role        *string `json:"role"`
	DisplayName *string `json:"display_name"`
	IsActive    *bool   `json:"is_active"`
}

// UpdateUser updates a user's role, display_name, or is_active status.
func (h *UserHandler) UpdateUser(c *gin.Context) {
	targetUserID := c.Param("id")

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Role != nil {
		if *req.Role != "admin" && *req.Role != "operator" && *req.Role != "viewer" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be admin, operator, or viewer"})
			return
		}
		_, err := h.db.Exec(c.Request.Context(),
			"UPDATE users SET role = $1 WHERE id = $2", *req.Role, targetUserID,
		)
		if err != nil {
			h.logger.Error("failed to update user role", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
			return
		}
	}

	if req.DisplayName != nil {
		_, err := h.db.Exec(c.Request.Context(),
			"UPDATE users SET display_name = $1 WHERE id = $2", *req.DisplayName, targetUserID,
		)
		if err != nil {
			h.logger.Error("failed to update display name", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
			return
		}
	}

	if req.IsActive != nil {
		_, err := h.db.Exec(c.Request.Context(),
			"UPDATE users SET is_active = $1 WHERE id = $2", *req.IsActive, targetUserID,
		)
		if err != nil {
			h.logger.Error("failed to update active status", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
			return
		}
	}

	h.logger.Info("user updated", "target_user_id", targetUserID, "updated_by", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"message": "user updated"})
}

// DeleteUser deletes a user.
func (h *UserHandler) DeleteUser(c *gin.Context) {
	targetUserID := c.Param("id")

	// Prevent self-deletion
	currentUserID, _ := c.Get("user_id")
	if targetUserID == currentUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete yourself"})
		return
	}

	result, err := h.db.Exec(c.Request.Context(), "DELETE FROM users WHERE id = $1", targetUserID)
	if err != nil {
		h.logger.Error("failed to delete user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	h.logger.Info("user deleted", "target_user_id", targetUserID, "deleted_by", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

// ListUserAccess returns all direct access grants for a user.
func (h *UserHandler) ListUserAccess(c *gin.Context) {
	targetUserID := c.Param("id")

	rows, err := h.db.Query(c.Request.Context(), `
		SELECT a.id, a.device_serial, a.group_id,
			COALESCE(dg.name, ''), a.permission, a.created_at
		FROM user_device_access a
		LEFT JOIN device_groups dg ON dg.id = a.group_id
		WHERE a.user_id = $1
		ORDER BY a.created_at
	`, targetUserID)
	if err != nil {
		h.logger.Error("failed to list user access", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list user access"})
		return
	}
	defer rows.Close()

	var grants []gin.H
	for rows.Next() {
		var id, permission string
		var deviceSerial, groupID, groupName *string
		var createdAt interface{}
		if err := rows.Scan(&id, &deviceSerial, &groupID, &groupName, &permission, &createdAt); err != nil {
			continue
		}
		grant := gin.H{
			"id":         id,
			"permission": permission,
			"created_at": createdAt,
		}
		if deviceSerial != nil {
			grant["device_serial"] = *deviceSerial
		}
		if groupID != nil {
			grant["group_id"] = *groupID
			if groupName != nil {
				grant["group_name"] = *groupName
			}
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("failed to read user access", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list user access"})
		return
	}

	if grants == nil {
		grants = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"grants": grants})
}

// RevokeUserAccess revokes a specific access grant from a user.
func (h *UserHandler) RevokeUserAccess(c *gin.Context) {
	targetUserID := c.Param("id")
	accessID := c.Param("accessId")

	result, err := h.db.Exec(c.Request.Context(),
		"DELETE FROM user_device_access WHERE id = $1 AND user_id = $2",
		accessID, targetUserID,
	)
	if err != nil {
		h.logger.Error("failed to revoke user access", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke access"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "access grant not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "access revoked"})
}

// ResetPasswordRequest represents a request to reset a user's password.
type ResetPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required"`
}

// ResetPassword resets a user's password (admin only).
func (h *UserHandler) ResetPassword(c *gin.Context) {
	targetUserID := c.Param("id")

	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new_password is required"})
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.db.Exec(c.Request.Context(),
		"UPDATE users SET password_hash = $1 WHERE id = $2", hash, targetUserID,
	)
	if err != nil {
		h.logger.Error("failed to reset password", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset password"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	h.logger.Info("password reset", "target_user_id", targetUserID, "reset_by", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"message": "password reset"})
}
