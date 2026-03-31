package websocket

import (
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"parily.dev/app/internal/metrics"
	"parily.dev/app/internal/redis"
)

type Hub struct {
	mu    sync.RWMutex
	closed bool
	rooms map[string]map[*websocket.Conn]bool
	subs  map[string]*redis.Subscription
	rdb   *redis.Client
	log   *zap.Logger
}

func NewHub(rdb *redis.Client, log *zap.Logger) *Hub {
	return &Hub{
		rooms: make(map[string]map[*websocket.Conn]bool),
		subs:  make(map[string]*redis.Subscription),
		rdb:   rdb,
		log:   log,
	}
}

func channelKey(roomID, fileID string) string {
	return "room:" + roomID + ":file:" + fileID
}

func (h *Hub) Register(conn *websocket.Conn, roomID, fileID string) bool {
	key := channelKey(roomID, fileID)
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
        return false
    }

	if h.rooms[key] == nil {
		h.rooms[key] = make(map[*websocket.Conn]bool)
	}
	h.rooms[key][conn] = true
	metrics.ActiveWebsocketConnections.Inc()
	if h.subs[key] == nil {
		h.subscribeRedis(key)
	}
	return true
}

func (h *Hub) Unregister(conn *websocket.Conn, roomID, fileID string) {
	key := channelKey(roomID, fileID)
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.rooms[key], conn)
	metrics.ActiveWebsocketConnections.Dec()
	if len(h.rooms[key]) == 0 {
		if h.subs[key] != nil {
			h.subs[key].Close()
			delete(h.subs, key)
		}
		delete(h.rooms, key)
	}
}

func (h *Hub) Broadcast(sender *websocket.Conn, roomID, fileID string, msgType int, data []byte) {
	key := channelKey(roomID, fileID)
	if err := h.rdb.Publish(key, data); err != nil {
		h.log.Error("redis publish failed", zap.Error(err))
		metrics.RedisPublishErrorsTotal.Inc()
	}
}

func (h *Hub) subscribeRedis(key string) {
	sub, err := h.rdb.Subscribe(key)
	if err != nil {
		h.log.Error("redis subscribe failed", zap.String("key", key), zap.Error(err))
		return
	}
	h.subs[key] = sub

	go func() {
		for msg := range sub.Messages() {
			h.mu.RLock()
			conns := h.rooms[key]
			h.mu.RUnlock()

			for conn := range conns {
				if err := conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
					conn.Close()
				}
			}
		}
	}()
}

func (h *Hub) Shutdown() {
    h.mu.Lock()
    h.closed = true    // any Register() after this returns false immediately
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
    h.log.Info("Yjs hub shutdown complete")
}


