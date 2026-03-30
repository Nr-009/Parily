package rooms

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/mongo"
	"parily.dev/app/internal/kafka"
	"parily.dev/app/internal/redis"
	wshandler "parily.dev/app/internal/websocket"
)

type Handler struct {
	db           *pgxpool.Pool
	mongoDB      *mongo.Database
	rdb          *redis.Client
	notifyHub    *wshandler.NotifyHub
	kafka        *kafka.Producer
	kafkaBroker  string          // stored separately for creating readers in history handlers
}

func NewHandler(db *pgxpool.Pool, mongoDB *mongo.Database, rdb *redis.Client, notifyHub *wshandler.NotifyHub, kafka *kafka.Producer, kafkaBroker string) *Handler {
	return &Handler{
		db:          db,
		mongoDB:     mongoDB,
		rdb:         rdb,
		notifyHub:   notifyHub,
		kafka:       kafka,
		kafkaBroker: kafkaBroker,
	}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("", h.CreateRoom)
	rg.GET("", h.ListRooms)
	rg.GET("/:roomID/role", h.GetRole)
	rg.DELETE("/:roomID", h.DeleteRoom)
	rg.PATCH("/:roomID/name", h.RenameRoom)
	rg.DELETE("/:roomID/leave", h.LeaveRoom)

	rg.GET("/:roomID/files", h.GetFiles)
	rg.POST("/:roomID/files", h.CreateFile)
	rg.PATCH("/:roomID/files/:fileID", h.UpdateFile)
	rg.PATCH("/:roomID/files/:fileID/toggle", h.ToggleFile)
	rg.DELETE("/:roomID/files/:fileID/permanent", h.PermanentDeleteFile)
	rg.POST("/:roomID/files/:fileID/state", h.SaveState)
	rg.GET("/:roomID/files/:fileID/state", h.LoadState)

	// history routes — 6.5
	rg.GET("/:roomID/files/:fileID/history", h.GetHistory)
	rg.GET("/:roomID/files/:fileID/history/:version", h.GetHistoryAtVersion)
	rg.POST("/:roomID/files/:fileID/restore", h.RestoreVersion)

	rg.POST("/:roomID/members", h.AddMember)
	rg.GET("/:roomID/members", h.ListMembers)
	rg.DELETE("/:roomID/members/:userID", h.RemoveMember)
	rg.PATCH("/:roomID/members/:userID", h.UpdateMemberRole)
	rg.GET("/:roomID/files/:fileID/execution", h.GetLastExecution)
}