package rooms

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	pg "parily.dev/app/internal/postgres"
)

type addMemberRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role"  binding:"required,oneof=editor viewer"`
}

func (h *Handler) AddMember(c *gin.Context) {
	roomID := c.Param("roomID")
	callerID := c.GetString("userID")
	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, callerID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the owner can invite members"})
		return
	}
	target, err := pg.GetUserByEmail(c.Request.Context(), h.db, strings.ToLower(req.Email))
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "no user found with that email"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if err := pg.AddMember(c.Request.Context(), h.db, roomID, target.ID, req.Role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not add member"})
		return
	}
	// get room name for the notification
	var roomNameForNotify string
	_ = h.db.QueryRow(c.Request.Context(), `SELECT name FROM rooms WHERE id = $1`, roomID).Scan(&roomNameForNotify)

	// notify the invited user so their dashboard updates instantly
	h.publishNotification(target.ID, map[string]any{
		"type":      "room_invited",
		"room_id":   roomID,
		"room_name": roomNameForNotify,
		"role":      req.Role,
	})

	c.JSON(http.StatusOK, gin.H{
		"message": "member added",
		"user":    gin.H{"id": target.ID, "email": target.Email, "name": target.Name},
		"role":    req.Role,
	})
}

func (h *Handler) ListMembers(c *gin.Context) {
	roomID := c.Param("roomID")
	userID := c.GetString("userID")
	_, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	members, err := pg.ListMembers(c.Request.Context(), h.db, roomID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list members"})
		return
	}
	result := make([]gin.H, 0, len(members))
	for _, m := range members {
		result = append(result, gin.H{
			"user_id": m.UserID,
			"email":   m.Email,
			"name":    m.Name,
			"role":    m.Role,
		})
	}
	c.JSON(http.StatusOK, gin.H{"members": result})
}

func (h *Handler) RemoveMember(c *gin.Context) {
	roomID := c.Param("roomID")
	targetID := c.Param("userID")
	callerID := c.GetString("userID")
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, callerID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "only owner can remove members"})
		return
	}
	if targetID == callerID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot remove yourself"})
		return
	}
	if err := pg.RemoveMember(c.Request.Context(), h.db, roomID, targetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not remove member"})
		return
	}
	h.publishPermission(roomID, map[string]string{
		"type":    "removed",
		"user_id": targetID,
	})
	c.JSON(http.StatusOK, gin.H{"message": "member removed"})
}

type updateRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=editor viewer"`
}

func (h *Handler) UpdateMemberRole(c *gin.Context) {
	roomID := c.Param("roomID")
	targetID := c.Param("userID")
	callerID := c.GetString("userID")
	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, callerID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "only owner can change roles"})
		return
	}
	if err := pg.UpdateMemberRole(c.Request.Context(), h.db, roomID, targetID, req.Role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update role"})
		return
	}
	h.publishPermission(roomID, map[string]string{
		"type":    "role_changed",
		"user_id": targetID,
		"role":    req.Role,
	})
	c.JSON(http.StatusOK, gin.H{"message": "role updated", "role": req.Role})
}
