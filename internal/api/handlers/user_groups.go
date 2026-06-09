package handlers

import (
	"log/slog"
	"net/http"

	"github.com/XpertaDK/batter/internal/api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserGroupHandler handles user group (team) API requests.
type UserGroupHandler struct {
	db     *pgxpool.Pool
	logger *slog.Logger
}

// NewUserGroupHandler creates a new user group handler.
func NewUserGroupHandler(db *pgxpool.Pool, logger *slog.Logger) *UserGroupHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &UserGroupHandler{
		db:     db,
		logger: logger.With("handler", "user_groups"),
	}
}

// ListUserGroups returns all user groups with member counts.
func (h *UserGroupHandler) ListUserGroups(c *gin.Context) {
	rows, err := h.db.Query(c.Request.Context(), `
		SELECT g.id, g.name, COALESCE(g.description, ''), g.created_at,
			(SELECT count(*) FROM user_group_members m WHERE m.group_id = g.id)
		FROM user_groups g
		ORDER BY g.name
	`)
	if err != nil {
		h.logger.Error("failed to list user groups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list user groups"})
		return
	}
	defer rows.Close()

	var groups []gin.H
	for rows.Next() {
		var id, name, description string
		var createdAt interface{}
		var memberCount int
		if err := rows.Scan(&id, &name, &description, &createdAt, &memberCount); err != nil {
			continue
		}
		groups = append(groups, gin.H{
			"id":           id,
			"name":         name,
			"description":  description,
			"member_count": memberCount,
			"created_at":   createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("failed to read user groups", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list user groups"})
		return
	}

	if groups == nil {
		groups = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"groups": groups})
}

type createUserGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// CreateUserGroup creates a new user group.
func (h *UserGroupHandler) CreateUserGroup(c *gin.Context) {
	var req createUserGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	userID, _ := c.Get(middleware.ContextKeyUserID)

	var groupID string
	err := h.db.QueryRow(c.Request.Context(),
		"INSERT INTO user_groups (name, description, created_by) VALUES ($1, $2, $3) RETURNING id",
		req.Name, req.Description, userID,
	).Scan(&groupID)
	if err != nil {
		h.logger.Error("failed to create user group", "error", err)
		c.JSON(http.StatusConflict, gin.H{"error": "group name already exists"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":   groupID,
		"name": req.Name,
	})
}

// UpdateUserGroupRequest represents a request to update a user group.
type updateUserGroupRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// UpdateUserGroup updates a user group's name and/or description.
func (h *UserGroupHandler) UpdateUserGroup(c *gin.Context) {
	groupID := c.Param("id")

	var req updateUserGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Name != nil {
		if *req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name cannot be empty"})
			return
		}
		_, err := h.db.Exec(c.Request.Context(),
			"UPDATE user_groups SET name = $1 WHERE id = $2", *req.Name, groupID,
		)
		if err != nil {
			h.logger.Error("failed to update user group name", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update group"})
			return
		}
	}

	if req.Description != nil {
		_, err := h.db.Exec(c.Request.Context(),
			"UPDATE user_groups SET description = $1 WHERE id = $2", *req.Description, groupID,
		)
		if err != nil {
			h.logger.Error("failed to update user group description", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update group"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "group updated"})
}

// DeleteUserGroup deletes a user group.
func (h *UserGroupHandler) DeleteUserGroup(c *gin.Context) {
	groupID := c.Param("id")

	result, err := h.db.Exec(c.Request.Context(), "DELETE FROM user_groups WHERE id = $1", groupID)
	if err != nil {
		h.logger.Error("failed to delete user group", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete group"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "group deleted"})
}

// ListMembers returns all members of a user group.
func (h *UserGroupHandler) ListMembers(c *gin.Context) {
	groupID := c.Param("id")

	rows, err := h.db.Query(c.Request.Context(), `
		SELECT u.id, u.username, COALESCE(u.display_name, u.username), u.role
		FROM user_group_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.group_id = $1
		ORDER BY u.username
	`, groupID)
	if err != nil {
		h.logger.Error("failed to list members", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}
	defer rows.Close()

	var members []gin.H
	for rows.Next() {
		var id, username, displayName, role string
		if err := rows.Scan(&id, &username, &displayName, &role); err != nil {
			continue
		}
		members = append(members, gin.H{
			"user_id":      id,
			"username":     username,
			"display_name": displayName,
			"role":         role,
		})
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("failed to read members", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}

	if members == nil {
		members = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"members": members})
}

type addMemberRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

// AddMember adds a user to a group.
func (h *UserGroupHandler) AddMember(c *gin.Context) {
	groupID := c.Param("id")

	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	_, err := h.db.Exec(c.Request.Context(),
		"INSERT INTO user_group_members (user_id, group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		req.UserID, groupID,
	)
	if err != nil {
		h.logger.Error("failed to add member", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add member"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "member added"})
}

// RemoveMember removes a user from a group.
func (h *UserGroupHandler) RemoveMember(c *gin.Context) {
	groupID := c.Param("id")
	userID := c.Param("userId")

	result, err := h.db.Exec(c.Request.Context(),
		"DELETE FROM user_group_members WHERE user_id = $1 AND group_id = $2",
		userID, groupID,
	)
	if err != nil {
		h.logger.Error("failed to remove member", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove member"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "member not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "member removed"})
}

// ListAccess returns all access grants for a user group.
func (h *UserGroupHandler) ListAccess(c *gin.Context) {
	groupID := c.Param("id")

	rows, err := h.db.Query(c.Request.Context(), `
		SELECT a.id, a.device_serial, a.group_id,
			COALESCE(dg.name, ''), a.permission, a.created_at
		FROM user_group_access a
		LEFT JOIN device_groups dg ON dg.id = a.group_id
		WHERE a.user_group_id = $1
		ORDER BY a.created_at
	`, groupID)
	if err != nil {
		h.logger.Error("failed to list access", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list access"})
		return
	}
	defer rows.Close()

	var grants []gin.H
	for rows.Next() {
		var id, permission string
		var deviceSerial, deviceGroupID, groupName *string
		var createdAt interface{}
		if err := rows.Scan(&id, &deviceSerial, &deviceGroupID, &groupName, &permission, &createdAt); err != nil {
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
		if deviceGroupID != nil {
			grant["group_id"] = *deviceGroupID
			if groupName != nil {
				grant["group_name"] = *groupName
			}
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		h.logger.Error("failed to read access grants", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list access"})
		return
	}

	if grants == nil {
		grants = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"grants": grants})
}

type grantUserGroupAccessRequest struct {
	DeviceSerial string `json:"device_serial"`
	GroupID      string `json:"group_id"`
	Permission   string `json:"permission" binding:"required"`
}

// GrantAccess grants device or device-group access to a user group.
func (h *UserGroupHandler) GrantAccess(c *gin.Context) {
	userGroupID := c.Param("id")

	var req grantUserGroupAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "permission is required"})
		return
	}

	if (req.DeviceSerial == "" && req.GroupID == "") || (req.DeviceSerial != "" && req.GroupID != "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exactly one of device_serial or group_id is required"})
		return
	}

	grantedBy, _ := c.Get(middleware.ContextKeyUserID)

	var deviceSerial, groupID *string
	if req.DeviceSerial != "" {
		deviceSerial = &req.DeviceSerial
	}
	if req.GroupID != "" {
		groupID = &req.GroupID
	}

	var accessID string
	err := h.db.QueryRow(c.Request.Context(), `
		INSERT INTO user_group_access (user_group_id, device_serial, group_id, permission, granted_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, userGroupID, deviceSerial, groupID, req.Permission, grantedBy).Scan(&accessID)
	if err != nil {
		h.logger.Error("failed to grant access", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to grant access"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": accessID, "message": "access granted"})
}

// RevokeAccess removes an access grant from a user group.
func (h *UserGroupHandler) RevokeAccess(c *gin.Context) {
	userGroupID := c.Param("id")
	accessID := c.Param("accessId")

	result, err := h.db.Exec(c.Request.Context(),
		"DELETE FROM user_group_access WHERE id = $1 AND user_group_id = $2",
		accessID, userGroupID,
	)
	if err != nil {
		h.logger.Error("failed to revoke access", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke access"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "access grant not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "access revoked"})
}
