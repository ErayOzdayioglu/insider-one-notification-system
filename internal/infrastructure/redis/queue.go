package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/queue"
)

const (
	// consumerGroupName is the Redis Streams consumer group used by all workers.
	consumerGroupName = "notification-workers"

	// streamDataField is the hash field name within each stream message that
	// holds the serialised notification JSON.
	streamDataField = "data"

	// blockTimeout controls how long XREADGROUP blocks before returning an
	// empty result, preventing tight busy-spin loops.
	blockTimeout = 2 * time.Second
)

// streamKey builds the canonical stream name for a channel/priority pair.
// Convention: "notifications:{channel}:{priority}" (9 streams total).
func streamKey(channel entity.Channel, priority entity.Priority) string {
	return fmt.Sprintf("notifications:%s:%s", channel, priority)
}

// --- Producer ---------------------------------------------------------------

// StreamProducer publishes notifications into the appropriate Redis Stream
// using XADD.
type StreamProducer struct {
	client *redis.Client
}

// NewStreamProducer creates a new StreamProducer backed by the given Redis client.
func NewStreamProducer(client *redis.Client) *StreamProducer {
	return &StreamProducer{client: client}
}

// Enqueue serialises the notification to JSON and appends it to the Redis
// Stream corresponding to the notification's channel and priority.
func (p *StreamProducer) Enqueue(ctx context.Context, notification *entity.Notification) error {
	data, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal notification %s: %w", notification.ID, err)
	}

	key := streamKey(notification.Channel, notification.Priority)
	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: key,
		ID:     "*", // auto-generate ID
		Values: map[string]interface{}{
			streamDataField: string(data),
		},
	}).Err(); err != nil {
		return fmt.Errorf("xadd to stream %s: %w", key, err)
	}

	return nil
}

// --- Consumer ---------------------------------------------------------------

// StreamConsumer reads and acknowledges notifications from Redis Streams
// using XREADGROUP with consumer groups.
type StreamConsumer struct {
	client       *redis.Client
	consumerName string
}

// NewStreamConsumer creates a new StreamConsumer. consumerName should be
// unique per worker instance (e.g. hostname or pod name) so that Redis can
// track pending entries per consumer.
func NewStreamConsumer(client *redis.Client, consumerName string) *StreamConsumer {
	return &StreamConsumer{
		client:       client,
		consumerName: consumerName,
	}
}

// Dequeue retrieves up to count messages from the stream corresponding to
// the given channel and priority. It blocks briefly (blockTimeout) if the
// stream is empty to avoid busy-spinning. Returns an empty slice (not an
// error) when no messages are available.
func (c *StreamConsumer) Dequeue(ctx context.Context, channel entity.Channel, priority entity.Priority, count int64) ([]*queue.QueueMessage, error) {
	key := streamKey(channel, priority)

	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    consumerGroupName,
		Consumer: c.consumerName,
		Streams:  []string{key, ">"},
		Count:    count,
		Block:    blockTimeout,
	}).Result()
	if err != nil {
		// redis.Nil means the block timed out with no new messages.
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("xreadgroup on stream %s: %w", key, err)
	}

	var messages []*queue.QueueMessage
	for _, stream := range streams {
		for _, msg := range stream.Messages {
			raw, ok := msg.Values[streamDataField]
			if !ok {
				continue
			}

			var notification entity.Notification
			if err := json.Unmarshal([]byte(raw.(string)), &notification); err != nil {
				return nil, fmt.Errorf("unmarshal notification from stream %s msg %s: %w", key, msg.ID, err)
			}

			messages = append(messages, &queue.QueueMessage{
				StreamMsgID:  msg.ID,
				Notification: &notification,
			})
		}
	}

	return messages, nil
}

// Ack acknowledges the successful processing of a message, removing it from
// the consumer group's pending entries list.
func (c *StreamConsumer) Ack(ctx context.Context, channel entity.Channel, priority entity.Priority, msgID string) error {
	key := streamKey(channel, priority)
	if err := c.client.XAck(ctx, key, consumerGroupName, msgID).Err(); err != nil {
		return fmt.Errorf("xack on stream %s msg %s: %w", key, msgID, err)
	}
	return nil
}

// --- Stream bootstrap -------------------------------------------------------

// EnsureStreams creates all 9 notification streams and their consumer groups
// if they do not already exist. This should be called once during
// application startup.
func EnsureStreams(ctx context.Context, client *redis.Client) error {
	for _, ch := range entity.AllChannels() {
		for _, pr := range entity.AllPriorities() {
			key := streamKey(ch, pr)
			err := client.XGroupCreateMkStream(ctx, key, consumerGroupName, "0").Err()
			if err != nil {
				// "BUSYGROUP" means the group already exists -- not an error.
				if isConsumerGroupExistsErr(err) {
					continue
				}
				return fmt.Errorf("create consumer group for stream %s: %w", key, err)
			}
		}
	}
	return nil
}

// isConsumerGroupExistsErr returns true if the error indicates the consumer
// group already exists (Redis BUSYGROUP error).
func isConsumerGroupExistsErr(err error) bool {
	return err != nil && err.Error() == "BUSYGROUP Consumer Group name already exists"
}
