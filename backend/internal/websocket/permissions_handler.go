package websocket

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	pg "parily.dev/app/internal/postgres"
)

type PermissionsHandler struct {
	hub *PermissionsHub
	db  *pgxpool.Pool
	cfg *config.Config
	log *zap.Logger
}

func NewPermissionsHandler(hub *PermissionsHub, db *pgxpool.Pool, cfg *config.Config, log *zap.Logger) *PermissionsHandler {
	return &PermissionsHandler{hub: hub, db: db, cfg: cfg, log: log}
}

// ServePermissions handles GET /ws/:roomId/permissions
// Same auth pattern as ServeWS:
//  1. Validate JWT
//  2. Check membership
//  3. Upgrade WebSocket
//  4. Register in PermissionsHub
//  5. Read loop (just keeps connection alive — events come from Redis via hub)
func (h *PermissionsHandler) ServePermissions(c *gin.Context) {
	roomID := c.Param("roomId")

	claims, err := auth.ParseToken(c, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	_, err = pg.GetMemberRole(c.Request.Context(), h.db, roomID, claims.UserID)
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
		h.log.Error("permissions ws upgrade failed", zap.Error(err))
		return
	}

	h.hub.Register(conn, roomID)
	defer h.hub.Unregister(conn, roomID)

	h.log.Info("permissions ws connected",
		zap.String("room", roomID),
		zap.String("user", claims.UserID),
	)

	// Keep connection alive — events are pushed from Redis via PermissionsHub
	// Client never sends messages here, only receives
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
