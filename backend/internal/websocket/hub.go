package websocket

import (
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"parily.dev/app/internal/redis"
)

type Hub struct {
	mu    sync.RWMutex
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

func (h *Hub) Register(conn *websocket.Conn, roomID, fileID string) {
	key := channelKey(roomID, fileID)
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[key] == nil {
		h.rooms[key] = make(map[*websocket.Conn]bool)
	}
	h.rooms[key][conn] = true

	if h.subs[key] == nil {
		h.subscribeRedis(key)
	}
}

func (h *Hub) Unregister(conn *websocket.Conn, roomID, fileID string) {
	key := channelKey(roomID, fileID)
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.rooms[key], conn)

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
