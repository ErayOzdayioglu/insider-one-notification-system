package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const (
	// wsNotificationsChannel is the single Redis Pub/Sub channel used to
	// broadcast notification status updates to WebSocket subscribers.
	wsNotificationsChannel = "ws:notifications"
)

// PubSub abstracts Redis Pub/Sub for broadcasting notification status
// updates to WebSocket clients.
type PubSub interface {
	// Publish sends data to the given channel.
	Publish(ctx context.Context, channel string, data []byte) error

	// Subscribe returns a read-only channel that receives messages published
	// to the given channel. The returned cancel function must be called when
	// the subscriber is done to release resources.
	Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error)
}

// RedisPubSub implements PubSub using Redis Pub/Sub.
type RedisPubSub struct {
	client *redis.Client
}

// NewPubSub creates a new RedisPubSub backed by the given Redis client.
func NewPubSub(client *redis.Client) *RedisPubSub {
	return &RedisPubSub{client: client}
}

// Publish sends data to the specified Redis Pub/Sub channel.
func (p *RedisPubSub) Publish(ctx context.Context, channel string, data []byte) error {
	if err := p.client.Publish(ctx, channel, data).Err(); err != nil {
		return fmt.Errorf("publish to channel %s: %w", channel, err)
	}
	return nil
}

// Subscribe creates a Redis subscription on the specified channel and
// returns a byte-channel that delivers messages. The caller must invoke the
// returned cancel function to unsubscribe and free resources when it no
// longer needs updates.
func (p *RedisPubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error) {
	sub := p.client.Subscribe(ctx, channel)

	// Verify the subscription succeeded by waiting for confirmation.
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, nil, fmt.Errorf("subscribe to channel %s: %w", channel, err)
	}

	out := make(chan []byte, 64)

	// Forward messages from the Redis subscription to the output channel.
	go func() {
		defer close(out)
		ch := sub.Channel()
		for msg := range ch {
			select {
			case out <- []byte(msg.Payload):
			case <-ctx.Done():
				return
			}
		}
	}()

	cancel := func() {
		_ = sub.Close()
	}

	return out, cancel, nil
}

// NotificationsChannel returns the standard channel name used for
// broadcasting all notification status updates.
func NotificationsChannel() string {
	return wsNotificationsChannel
}
