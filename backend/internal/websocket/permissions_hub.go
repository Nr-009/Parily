package websocket

import (
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"parily.dev/app/internal/redis"
)

// PermissionsHub manages WebSocket connections for permission events.
// Each room has a set of connections — one per connected user.
// When a permission event fires on Redis, it broadcasts to all connections in that room.
// Completely separate from Hub — different connections, different channels, different purpose.
type PermissionsHub struct {
	mu    sync.RWMutex
	rooms map[string]map[*websocket.Conn]bool // roomID → set of connections
	subs  map[string]*redis.Subscription      // roomID → Redis subscription
	rdb   *redis.Client
	log   *zap.Logger
}

func NewPermissionsHub(rdb *redis.Client, log *zap.Logger) *PermissionsHub {
	return &PermissionsHub{
		rooms: make(map[string]map[*websocket.Conn]bool),
		subs:  make(map[string]*redis.Subscription),
		rdb:   rdb,
		log:   log,
	}
}

func (h *PermissionsHub) Register(conn *websocket.Conn, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[roomID] == nil {
		h.rooms[roomID] = make(map[*websocket.Conn]bool)
	}
	h.rooms[roomID][conn] = true

	// First connection in this room — subscribe to Redis permissions channel
	if h.subs[roomID] == nil {
		h.subscribeRedis(roomID)
	}
}

func (h *PermissionsHub) Unregister(conn *websocket.Conn, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.rooms[roomID], conn)

	if len(h.rooms[roomID]) == 0 {
		if h.subs[roomID] != nil {
			h.subs[roomID].Close()
			delete(h.subs, roomID)
		}
		delete(h.rooms, roomID)
	}
}

func (h *PermissionsHub) subscribeRedis(roomID string) {
	sub, err := h.rdb.Subscribe("room:" + roomID + ":permissions")
	if err != nil {
		h.log.Error("permissions subscribe failed",
			zap.String("room", roomID),
			zap.Error(err),
		)
		return
	}
	h.subs[roomID] = sub

	go func() {
		for msg := range sub.Messages() {
			h.mu.RLock()
			conns := h.rooms[roomID]
			h.mu.RUnlock()

			for conn := range conns {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					conn.Close()
				}
			}
		}
	}()
}
