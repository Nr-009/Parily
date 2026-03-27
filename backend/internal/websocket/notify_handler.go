package websocket

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
)

type NotifyHandler struct {
	hub *NotifyHub
	db  *pgxpool.Pool
	cfg *config.Config
	log *zap.Logger
}

func NewNotifyHandler(hub *NotifyHub, db *pgxpool.Pool, cfg *config.Config, log *zap.Logger) *NotifyHandler {
	return &NotifyHandler{hub: hub, db: db, cfg: cfg, log: log}
}

// ServeNotify handles GET /notify-ws
// Validates JWT, upgrades to WebSocket, registers connection under userID.
// Client just listens — no messages sent from client to server.
func (h *NotifyHandler) ServeNotify(c *gin.Context) {
	claims, err := auth.ParseToken(c, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.log.Error("notify ws upgrade failed", zap.Error(err))
		return
	}

	userID := claims.UserID
	h.hub.Register(conn, userID)
	defer h.hub.Unregister(conn, userID)

	h.log.Info("notify ws connected", zap.String("user", userID))

	// keep connection alive — read and discard any incoming messages
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
