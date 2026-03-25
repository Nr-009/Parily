package websocket

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	pg "parily.dev/app/internal/postgres"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Handler struct {
	hub *Hub
	db  *pgxpool.Pool
	cfg *config.Config
	log *zap.Logger
}

func NewHandler(hub *Hub, db *pgxpool.Pool, cfg *config.Config, log *zap.Logger) *Handler {
	return &Handler{hub: hub, db: db, cfg: cfg, log: log}
}

func (h *Handler) ServeWS(c *gin.Context) {
	roomID := c.Param("roomId")
	fileID := c.Param("fileId")

	claims, err := auth.ParseToken(c, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, claims.UserID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.log.Error("ws upgrade failed", zap.Error(err))
		return
	}

	h.hub.Register(conn, roomID, fileID)
	defer h.hub.Unregister(conn, roomID, fileID)

	h.log.Info("ws client connected",
		zap.String("room", roomID),
		zap.String("file", fileID),
		zap.String("user", claims.UserID),
		zap.String("role", role),
	)

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if role == "viewer" {
			continue
		}
		h.hub.Broadcast(conn, roomID, fileID, msgType, data)
	}
}
