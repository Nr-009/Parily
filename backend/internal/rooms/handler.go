package rooms

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"

	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
	"parily.dev/app/internal/redis"
)

type Handler struct {
	db      *pgxpool.Pool
	mongoDB *mongo.Database
	rdb     *redis.Client
}

func NewHandler(db *pgxpool.Pool, mongoDB *mongo.Database, rdb *redis.Client) *Handler {
	return &Handler{db: db, mongoDB: mongoDB, rdb: rdb}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("", h.CreateRoom)
	rg.GET("", h.ListRooms)
	rg.GET("/:roomID/role", h.GetRole)
	rg.GET("/:roomID/files", h.GetFiles)
	rg.PATCH("/:roomID/files/:fileID", h.UpdateFile)
	rg.POST("/:roomID/files/:fileID/state", h.SaveState)
	rg.GET("/:roomID/files/:fileID/state", h.LoadState)
	rg.POST("/:roomID/members", h.AddMember)
	rg.GET("/:roomID/members", h.ListMembers)
	rg.DELETE("/:roomID/members/:userID", h.RemoveMember)
	rg.PATCH("/:roomID/members/:userID", h.UpdateMemberRole)
	rg.DELETE("/:roomID", h.DeleteRoom)
}

// publishPermission publishes a permission event to Redis.
// Uses the existing Publish method — just a different channel string.
func (h *Handler) publishPermission(roomID string, event map[string]string) {
	data, _ := json.Marshal(event)
	h.rdb.Publish("room:"+roomID+":permissions", data)
}

type createRoomRequest struct {
	Name string `json:"name" binding:"required,min=1,max=100"`
}

func (h *Handler) CreateRoom(c *gin.Context) {
	var req createRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ownerID := c.GetString("userID")
	room, fileID, err := pg.CreateRoom(c.Request.Context(), h.db, strings.TrimSpace(req.Name), ownerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create room"})
		return
	}
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	if err := docRepo.CreateDocument(c.Request.Context(), fileID, room.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create document"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"room": gin.H{
			"id":         room.ID,
			"name":       room.Name,
			"owner_id":   room.OwnerID,
			"role":       "owner",
			"created_at": room.CreatedAt.Format(time.RFC3339),
		},
		"file_id": fileID,
	})
}

func (h *Handler) ListRooms(c *gin.Context) {
	userID := c.GetString("userID")
	rooms, err := pg.ListRoomsForUser(c.Request.Context(), h.db, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list rooms"})
		return
	}
	result := make([]gin.H, 0, len(rooms))
	for _, r := range rooms {
		result = append(result, gin.H{
			"id":         r.ID,
			"name":       r.Name,
			"owner_id":   r.OwnerID,
			"role":       r.Role,
			"created_at": r.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"rooms": result})
}

func (h *Handler) GetRole(c *gin.Context) {
	roomID := c.Param("roomID")
	userID := c.GetString("userID")
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"role": role})
}

func (h *Handler) GetFiles(c *gin.Context) {
	roomID := c.Param("roomID")
	userID := c.GetString("userID")
	_, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	files, err := pg.GetFilesForRoom(c.Request.Context(), h.db, roomID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get files"})
		return
	}
	result := make([]gin.H, 0, len(files))
	for _, f := range files {
		result = append(result, gin.H{
			"id":       f.ID,
			"name":     f.Name,
			"language": f.Language,
		})
	}
	c.JSON(http.StatusOK, gin.H{"files": result})
}

type updateFileRequest struct {
	Name     string `json:"name"     binding:"required,min=1,max=255"`
	Language string `json:"language" binding:"required"`
}

func (h *Handler) UpdateFile(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	var req updateFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot rename files"})
		return
	}
	file, err := pg.UpdateFile(c.Request.Context(), h.db, fileID, req.Name, req.Language)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update file"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"file": gin.H{
			"id":         file.ID,
			"name":       file.Name,
			"language":   file.Language,
			"updated_at": file.UpdatedAtStr(),
		},
	})
}

func (h *Handler) SaveState(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot save"})
		return
	}
	state, err := io.ReadAll(c.Request.Body)
	if err != nil || len(state) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty state"})
		return
	}
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	if err := docRepo.SaveDocument(c.Request.Context(), fileID, state); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save state"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "saved"})
}

func (h *Handler) LoadState(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	_, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	doc, err := docRepo.LoadDocument(c.Request.Context(), fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load state"})
		return
	}
	if doc == nil || len(doc.YjsState) == 0 {
		c.Status(http.StatusNoContent)
		return
	}
	c.Data(http.StatusOK, "application/octet-stream", doc.YjsState)
}

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

// Also add to RegisterRoutes: rg.DELETE("/:roomID", h.DeleteRoom)

func (h *Handler) DeleteRoom(c *gin.Context) {
	roomID := c.Param("roomID")
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
		c.JSON(http.StatusForbidden, gin.H{"error": "only owner can delete the room"})
		return
	}

	// Delete MongoDB documents first (no CASCADE there)
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	if err := docRepo.DeleteDocumentsByRoom(c.Request.Context(), roomID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete documents"})
		return
	}

	// Single DELETE — CASCADE handles files and room_members automatically
	if err := pg.DeleteRoom(c.Request.Context(), h.db, roomID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete room"})
		return
	}

	// Notify anyone currently in the room
	h.publishPermission(roomID, map[string]string{
		"type": "room_deleted",
	})

	c.JSON(http.StatusOK, gin.H{"message": "room deleted"})
}
