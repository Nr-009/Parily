package websocket

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"parily.dev/app/internal/redis"
)

// NotifyHub manages WebSocket connections for the notification channel.
// Each user has their own Redis channel: user:{userID}:notify
// When a notification is published to that channel, it is broadcast
// to all connections that user has open (e.g. multiple tabs on dashboard).
type NotifyHub struct {
	mu    sync.RWMutex
	closed bool  
	users map[string]map[*websocket.Conn]bool
	subs  map[string]*redis.Subscription
	rdb   *redis.Client
	log   *zap.Logger
}

func NewNotifyHub(rdb *redis.Client, log *zap.Logger) *NotifyHub {
	return &NotifyHub{
		users: make(map[string]map[*websocket.Conn]bool),
		subs:  make(map[string]*redis.Subscription),
		rdb:   rdb,
		log:   log,
	}
}

func notifyChannel(userID string) string {
	return "user:" + userID + ":notify"
}

func (h *NotifyHub) Register(conn *websocket.Conn, userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
        return false
    }

	if h.users[userID] == nil {
		h.users[userID] = make(map[*websocket.Conn]bool)
	}
	h.users[userID][conn] = true

	if h.subs[userID] == nil {
		h.subscribeRedis(userID)
	}
	return true 
}

func (h *NotifyHub) Unregister(conn *websocket.Conn, userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.users[userID], conn)
	if len(h.users[userID]) == 0 {
		if h.subs[userID] != nil {
			h.subs[userID].Close()
			delete(h.subs, userID)
		}
		delete(h.users, userID)
	}
}

// Publish sends a notification to a specific user's channel.
func (h *NotifyHub) Publish(userID string, msg []byte) error {
	fmt.Println(">>> NotifyHub.Publish user:", userID, "channel:", notifyChannel(userID))
	return h.rdb.Publish(notifyChannel(userID), msg)
}

func (h *NotifyHub) subscribeRedis(userID string) {
	sub, err := h.rdb.Subscribe(notifyChannel(userID))
	if err != nil {
		h.log.Error("notify hub subscribe failed",
			zap.String("user", userID),
			zap.Error(err),
		)
		return
	}
	h.subs[userID] = sub

	go func() {
		for msg := range sub.Messages() {
			h.mu.RLock()
			conns := h.users[userID]
			h.mu.RUnlock()

			for conn := range conns {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					conn.Close()
				}
			}
		}
	}()
}

func (h *NotifyHub) Shutdown() {
    h.mu.Lock()
    h.closed = true
    var conns []*websocket.Conn
    for _, userConns := range h.users {
        for conn := range userConns {
            conns = append(conns, conn)
        }
    }
    var subs []*redis.Subscription
    for _, sub := range h.subs {
        subs = append(subs, sub)
    }
    h.users = make(map[string]map[*websocket.Conn]bool)
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
    }
    h.log.Info("NotifyHub shutdown complete")
}
