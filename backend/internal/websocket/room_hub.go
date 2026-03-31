package websocket

import (
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"parily.dev/app/internal/metrics"
	"parily.dev/app/internal/redis"
)

// RoomHub manages WebSocket connections for the room channel.
// Handles all non-Yjs messaging: permission events and presence heartbeats.
// Each room has a set of connections — one per connected user.
// When any message arrives on Redis, it broadcasts to all connections in that room.
type RoomHub struct {
	mu    sync.RWMutex
	closed bool
	rooms map[string]map[*websocket.Conn]bool
	subs  map[string]*redis.Subscription
	rdb   *redis.Client
	log   *zap.Logger
}

func NewRoomHub(rdb *redis.Client, log *zap.Logger) *RoomHub {
	return &RoomHub{
		rooms: make(map[string]map[*websocket.Conn]bool),
		subs:  make(map[string]*redis.Subscription),
		rdb:   rdb,
		log:   log,
	}
}

func (h *RoomHub) Register(conn *websocket.Conn, roomID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
        return false
    }
	if h.rooms[roomID] == nil {
		h.rooms[roomID] = make(map[*websocket.Conn]bool)
		metrics.ActiveRoomsTotal.Inc()
	}
	h.rooms[roomID][conn] = true
	metrics.ActiveWebsocketConnections.Inc()
	metrics.RoomJoinsTotal.Inc()
	if h.subs[roomID] == nil {
		h.subscribeRedis(roomID)
	}
	return true
}

func (h *RoomHub) Unregister(conn *websocket.Conn, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.rooms[roomID], conn)
	metrics.ActiveWebsocketConnections.Dec()
	if len(h.rooms[roomID]) == 0 {
		if h.subs[roomID] != nil {
			h.subs[roomID].Close()
			delete(h.subs, roomID)
			metrics.ActiveRoomsTotal.Dec()
		}
		delete(h.rooms, roomID)
	}
}

// Publish sends a message to the room channel so Redis fans it out to all pods.
func (h *RoomHub) Publish(roomID string, msg []byte) error {
	if err := h.rdb.Publish("room:"+roomID+":room", msg); err != nil {
    	metrics.RedisPublishErrorsTotal.Inc()
    	return err
	}
return nil
}

func (h *RoomHub) subscribeRedis(roomID string) {
	sub, err := h.rdb.Subscribe("room:" + roomID + ":room")
	if err != nil {
		h.log.Error("room hub subscribe failed",
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

func (h *RoomHub) Shutdown() {
    h.mu.Lock()
    h.closed = true
    var conns []*websocket.Conn
    for _, room := range h.rooms {
        for conn := range room {
            conns = append(conns, conn)
        }
    }
    var subs []*redis.Subscription
    for _, sub := range h.subs {
        subs = append(subs, sub)
    }
    roomCount := len(h.rooms)
    h.rooms = make(map[string]map[*websocket.Conn]bool)
    h.subs = make(map[string]*redis.Subscription)
    h.mu.Unlock()

    for _, sub := range subs {
        sub.Close()
    }
    for _, conn := range conns {
        conn.WriteMessage(
            websocket.CloseMessage,
            websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
        )
        conn.Close()
        metrics.ActiveWebsocketConnections.Dec()
    }
    for i := 0; i < roomCount; i++ {
        metrics.ActiveRoomsTotal.Dec()
    }
    h.log.Info("RoomHub shutdown complete")
}
