package rooms
import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
)

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
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	if err := docRepo.DeleteDocumentsByRoom(c.Request.Context(), roomID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete documents"})
		return
	}
	if err := pg.DeleteRoom(c.Request.Context(), h.db, roomID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete room"})
		return
	}
	h.publishPermission(roomID, map[string]string{"type": "room_deleted"})
	c.JSON(http.StatusOK, gin.H{"message": "room deleted"})
}
