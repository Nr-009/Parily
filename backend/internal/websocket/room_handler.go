package websocket

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"parily.dev/app/internal/auth"
	"parily.dev/app/internal/config"
	pg "parily.dev/app/internal/postgres"
	pb "parily.dev/app/proto"
)

type RoomHandler struct {
	hub            *RoomHub
	db             *pgxpool.Pool
	cfg            *config.Config
	log            *zap.Logger
	executorClient pb.ExecutorServiceClient
}

func NewRoomHandler(
	hub *RoomHub,
	db *pgxpool.Pool,
	cfg *config.Config,
	log *zap.Logger,
	executorClient pb.ExecutorServiceClient,
) *RoomHandler {
	return &RoomHandler{
		hub:            hub,
		db:             db,
		cfg:            cfg,
		log:            log,
		executorClient: executorClient,
	}
}

// incomingMessage is used only to peek at the type field
// before deciding what to do with the message
type incomingMessage struct {
	Type        string `json:"type"`
	FileID      string `json:"file_id"`
	ExecutionID string `json:"execution_id"`
}

func (h *RoomHandler) ServeRoom(c *gin.Context) {
	roomID := c.Param("roomId")

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
		h.log.Error("room ws upgrade failed", zap.Error(err))
		return
	}

	if !h.hub.Register(conn, roomID) {
    conn.WriteMessage(
        websocket.CloseMessage,
        websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
    )
    conn.Close()
    return
}
defer h.hub.Unregister(conn, roomID)

	h.log.Info("room ws connected",
		zap.String("room", roomID),
		zap.String("user", claims.UserID),
	)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg incomingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			// not JSON — publish as-is (heartbeats etc.)
			h.hub.Publish(roomID, data)
			continue
		}

		switch msg.Type {
		case "run_file":
			h.handleRunFile(roomID, claims.UserID, claims.UserID, role, msg)
		default:
			// everything else (heartbeat, etc.) published to Redis as before
			if err := h.hub.Publish(roomID, data); err != nil {
				h.log.Error("room hub publish failed",
					zap.String("room", roomID),
					zap.String("user", claims.UserID),
					zap.Error(err),
				)
			}
		}
	}
}

func (h *RoomHandler) handleRunFile(
	roomID, userID, username, role string,
	msg incomingMessage,
) {
	tracer := otel.Tracer("pairly")
	ctx, span := tracer.Start(context.Background(), "handleRunFile",
    oteltrace.WithAttributes(
        attribute.String("room.id", roomID),
        attribute.String("file.id", msg.FileID),
        attribute.String("user.id", userID),
    ),
	)
	defer span.End()
	// only owners and editors can run code
	if role != "owner" && role != "editor" {
		event, _ := json.Marshal(map[string]string{
			"type":    "execution_error",
			"file_id": msg.FileID,
			"reason":  "permission_denied",
		})
		h.hub.Publish(roomID, event)
		return
	}

	// call executor — this returns quickly with OK or "already running"
	// the actual execution happens async inside the executor
	_, err := h.executorClient.Execute(ctx, &pb.ExecuteRequest{
		ExecutionId: msg.ExecutionID,
		RoomId:      roomID,
		FileId:      msg.FileID,
	})

	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		st, _ := status.FromError(err)
		reason := "error"
		if st.Code() == codes.ResourceExhausted {
			reason = "already_running"
		}
		event, _ := json.Marshal(map[string]string{
			"type":    "execution_error",
			"file_id": msg.FileID,
			"reason":  reason,
		})
		h.hub.Publish(roomID, event)
		return
	}

	// gRPC returned OK — publish executing event so all clients start spinner
	event, _ := json.Marshal(map[string]string{
		"type":         "executing",
		"file_id":      msg.FileID,
		"room_id":      roomID,
		"triggered_by": username,
	})
	h.hub.Publish(roomID, event)
}
