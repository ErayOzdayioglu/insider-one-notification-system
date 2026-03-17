package websocket

import (
	"time"

	"github.com/google/uuid"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
)

// StatusUpdate is the payload broadcast to WebSocket clients whenever a
// notification transitions to a new status. It mirrors what the processing
// engine publishes to the Redis "ws:notifications" Pub/Sub channel.
type StatusUpdate struct {
	NotificationID    uuid.UUID     `json:"notification_id"`
	Status            entity.Status `json:"status"`
	ProviderMessageID *string       `json:"provider_message_id,omitempty"`
	ErrorMessage      *string       `json:"error_message,omitempty"`
	Timestamp         time.Time     `json:"timestamp"`
}
