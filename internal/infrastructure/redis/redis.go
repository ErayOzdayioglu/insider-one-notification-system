// Package redis provides Redis-backed infrastructure implementations for
// the notification system's queue, rate limiter, and pub/sub components.
package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// NewClient creates a configured Redis client and verifies the connection
// with a PING. The caller is responsible for closing the client when done.
func NewClient(addr, password string, db int) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return client, nil
}
