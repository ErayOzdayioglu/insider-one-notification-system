package queue

import (
	"context"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
)

// QueueMessage wraps a Notification with the stream-level message ID
// assigned by the underlying queue implementation (e.g. Redis Stream ID).
type QueueMessage struct {
	StreamMsgID  string
	Notification *entity.Notification
}

// Producer publishes notifications into the processing queue.
type Producer interface {
	// Enqueue adds a notification to the appropriate channel/priority queue.
	Enqueue(ctx context.Context, notification *entity.Notification) error
}

// Consumer reads and acknowledges notifications from the processing queue.
type Consumer interface {
	// Dequeue retrieves up to count messages from the queue for the given
	// channel and priority combination. Implementations should block briefly
	// if the queue is empty rather than busy-spinning.
	Dequeue(ctx context.Context, channel entity.Channel, priority entity.Priority, count int64) ([]*QueueMessage, error)

	// Ack acknowledges successful processing of the message identified by
	// msgID, removing it from the pending entries list.
	Ack(ctx context.Context, channel entity.Channel, priority entity.Priority, msgID string) error
}
