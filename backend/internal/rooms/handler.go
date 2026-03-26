package rooms

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"

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
	rg.DELETE("/:roomID", h.DeleteRoom)

	rg.GET("/:roomID/files", h.GetFiles)
	rg.POST("/:roomID/files", h.CreateFile)
	rg.PATCH("/:roomID/files/:fileID", h.UpdateFile)
	rg.PATCH("/:roomID/files/:fileID/toggle", h.ToggleFile)
	rg.POST("/:roomID/files/:fileID/state", h.SaveState)
	rg.GET("/:roomID/files/:fileID/state", h.LoadState)

	rg.POST("/:roomID/members", h.AddMember)
	rg.GET("/:roomID/members", h.ListMembers)
	rg.DELETE("/:roomID/members/:userID", h.RemoveMember)
	rg.PATCH("/:roomID/members/:userID", h.UpdateMemberRole)
}
