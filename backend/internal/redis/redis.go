package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"parily.dev/app/internal/config"
)

type Client struct {
	rdb *redis.Client
}

type Subscription struct {
	ps *redis.PubSub
}

// Messages returns the channel of incoming messages.
func (s *Subscription) Messages() <-chan []byte {
	raw := s.ps.Channel()
	out := make(chan []byte)
	go func() {
		for msg := range raw {
			out <- []byte(msg.Payload)
		}
		close(out)
	}()
	return out
}

func (s *Subscription) Close() {
	_ = s.ps.Close()
}

func Connect(cfg *config.Config) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       0,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}
	return &Client{rdb: rdb}, nil
}

// Publish sends a message to the given channel key as-is — no prefix added.
// The hub passes the full key e.g. "room:abc:file:xyz".
func (c *Client) Publish(channel string, msg []byte) error {
	return c.rdb.Publish(context.Background(), channel, msg).Err()
}

// Subscribe subscribes to the given channel key as-is — no prefix added.
func (c *Client) Subscribe(channel string) (*Subscription, error) {
	ps := c.rdb.Subscribe(context.Background(), channel)
	// Verify the subscription was accepted
	if _, err := ps.Receive(context.Background()); err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	return &Subscription{ps: ps}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func (c *Client) Close() error {
	return c.rdb.Close()
}


// SetNX sets key only if it doesn't exist. Returns true if lock was acquired.
// EX sets auto-expiry in seconds as a dead man's switch.
func (c *Client) SetNX(key string, value string, expirySeconds int) (bool, error) {
	result, err := c.rdb.SetArgs(context.Background(), key, value, redis.SetArgs{
		Mode: "NX",
		TTL:  time.Duration(expirySeconds) * time.Second,
	}).Result()
	if err != nil && err != redis.Nil {
		return false, fmt.Errorf("setnx: %w", err)
	}
	return result == "OK", nil
}


// Del deletes a key — used to release the execution lock.
func (c *Client) Del(key string) error {
	return c.rdb.Del(context.Background(), key).Err()
}