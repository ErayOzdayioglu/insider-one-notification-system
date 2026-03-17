package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/queue"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/infrastructure/httpclient"
	infraredis "github.com/erayozdayioglu/insider-one-notification-system/internal/infrastructure/redis"
)

// statusUpdate is the JSON payload published via PubSub when a notification's
// status changes, allowing WebSocket subscribers to receive real-time updates.
type statusUpdate struct {
	NotificationID string `json:"notification_id"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	Timestamp      string `json:"timestamp"`
}

// Processor handles the lifecycle of a single notification: rate limiting,
// delivery via the external provider, status persistence, and retry scheduling.
type Processor struct {
	repo       repository.NotificationRepository
	consumer   queue.Consumer
	delivery   httpclient.DeliveryClient
	limiter    infraredis.RateLimiter
	pubsub     infraredis.PubSub
	workerCfg  config.WorkerConfig
}

// NewProcessor creates a Processor with all required dependencies.
func NewProcessor(
	repo repository.NotificationRepository,
	consumer queue.Consumer,
	delivery httpclient.DeliveryClient,
	limiter infraredis.RateLimiter,
	pubsub infraredis.PubSub,
	workerCfg config.WorkerConfig,
) *Processor {
	return &Processor{
		repo:      repo,
		consumer:  consumer,
		delivery:  delivery,
		limiter:   limiter,
		pubsub:    pubsub,
		workerCfg: workerCfg,
	}
}

// Process handles a single queued notification message through the full
// delivery lifecycle: status transition, rate limiting, provider call,
// and result recording.
func (p *Processor) Process(ctx context.Context, msg *queue.QueueMessage) error {
	n := msg.Notification

	// Step a: transition to processing.
	if err := p.repo.UpdateStatus(ctx, n.ID, entity.StatusProcessing, nil, nil); err != nil {
		return fmt.Errorf("updating status to processing for %s: %w", n.ID, err)
	}
	p.publishStatusUpdate(ctx, n.ID.String(), string(entity.StatusProcessing), "")

	// Step b: wait for rate limiter token for this channel.
	if err := p.limiter.Wait(ctx, string(n.Channel)); err != nil {
		return fmt.Errorf("rate limiter wait for channel %s: %w", n.Channel, err)
	}

	// Step c: attempt delivery via the external provider.
	resp, deliveryErr := p.delivery.Send(ctx, n)

	// Step d: on success — mark delivered and ACK.
	if deliveryErr == nil {
		providerMsgID := resp.MessageID
		if err := p.repo.UpdateStatus(ctx, n.ID, entity.StatusDelivered, &providerMsgID, nil); err != nil {
			return fmt.Errorf("updating status to delivered for %s: %w", n.ID, err)
		}
		p.publishStatusUpdate(ctx, n.ID.String(), string(entity.StatusDelivered), "")

		if err := p.consumer.Ack(ctx, n.Channel, n.Priority, msg.StreamMsgID); err != nil {
			return fmt.Errorf("acking message %s: %w", msg.StreamMsgID, err)
		}
		return nil
	}

	// Step e: on failure — increment attempts, schedule retry or mark terminal failure.
	log.Printf("delivery failed for notification %s: %v", n.ID, deliveryErr)

	newAttempts := n.Attempts + 1
	errMsg := deliveryErr.Error()

	if newAttempts < n.MaxAttempts {
		// Retriable: calculate next retry time and persist.
		nextRetry := calculateNextRetry(newAttempts, p.workerCfg.RetryBaseDelay, p.workerCfg.RetryMaxDelay)
		if err := p.repo.IncrementAttempts(ctx, n.ID, &nextRetry); err != nil {
			return fmt.Errorf("incrementing attempts for %s: %w", n.ID, err)
		}
		if err := p.repo.UpdateStatus(ctx, n.ID, entity.StatusFailed, nil, &errMsg); err != nil {
			return fmt.Errorf("updating status to failed for %s: %w", n.ID, err)
		}
	} else {
		// Terminal failure: no more retries.
		if err := p.repo.IncrementAttempts(ctx, n.ID, nil); err != nil {
			return fmt.Errorf("incrementing attempts for %s: %w", n.ID, err)
		}
		if err := p.repo.UpdateStatus(ctx, n.ID, entity.StatusFailed, nil, &errMsg); err != nil {
			return fmt.Errorf("updating status to failed (terminal) for %s: %w", n.ID, err)
		}
	}

	p.publishStatusUpdate(ctx, n.ID.String(), string(entity.StatusFailed), errMsg)

	// ACK the queue message regardless — the scheduler will re-enqueue retriable
	// notifications when their next_retry_at arrives.
	if err := p.consumer.Ack(ctx, n.Channel, n.Priority, msg.StreamMsgID); err != nil {
		return fmt.Errorf("acking failed message %s: %w", msg.StreamMsgID, err)
	}

	return nil
}

// publishStatusUpdate broadcasts a status change via PubSub. Errors are
// logged but not propagated because delivery status persistence is the
// source of truth; PubSub is best-effort for real-time UI updates.
func (p *Processor) publishStatusUpdate(ctx context.Context, notificationID, status, errMsg string) {
	update := statusUpdate{
		NotificationID: notificationID,
		Status:         status,
		Error:          errMsg,
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("failed to marshal status update for %s: %v", notificationID, err)
		return
	}
	if err := p.pubsub.Publish(ctx, infraredis.NotificationsChannel(), data); err != nil {
		log.Printf("failed to publish status update for %s: %v", notificationID, err)
	}
}

// calculateNextRetry computes the next retry time using exponential backoff
// with jitter. The delay is baseDelay * 2^attempts, capped at maxDelay,
// with up to 20% random jitter added to prevent thundering herd.
func calculateNextRetry(attempts int, baseDelay, maxDelay time.Duration) time.Time {
	delay := float64(baseDelay) * math.Pow(2, float64(attempts))
	if delay > float64(maxDelay) {
		delay = float64(maxDelay)
	}

	// Add jitter: +-20% of the computed delay.
	jitter := delay * 0.2 * (rand.Float64()*2 - 1) //nolint:gosec // jitter does not need crypto rand
	delay += jitter

	if delay < float64(baseDelay) {
		delay = float64(baseDelay)
	}

	return time.Now().UTC().Add(time.Duration(delay))
}
