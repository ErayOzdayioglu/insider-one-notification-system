package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	domainerrors "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/errors"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/queue"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	domainservice "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/service"
	redisPubSub "github.com/erayozdayioglu/insider-one-notification-system/internal/infrastructure/redis"
	"github.com/google/uuid"
)

const maxBatchSize = 1000

// statusUpdate is the payload published via PubSub when a notification's
// status changes. WebSocket subscribers use this to push real-time updates.
type statusUpdate struct {
	NotificationID string        `json:"notification_id"`
	Status         entity.Status `json:"status"`
}

// notificationService implements domainservice.NotificationService.
type notificationService struct {
	repo     repository.NotificationRepository
	producer queue.Producer
	pubsub   redisPubSub.PubSub
	logger   *slog.Logger
}

// Compile-time interface satisfaction check.
var _ domainservice.NotificationService = (*notificationService)(nil)

// NewNotificationService constructs a NotificationService with all required
// dependencies injected.
func NewNotificationService(
	repo repository.NotificationRepository,
	producer queue.Producer,
	pubsub redisPubSub.PubSub,
	logger *slog.Logger,
) domainservice.NotificationService {
	return &notificationService{
		repo:     repo,
		producer: producer,
		pubsub:   pubsub,
		logger:   logger,
	}
}

// Create validates and persists a single notification. If the notification is
// not scheduled for the future, it is immediately enqueued for processing.
// A status change event is broadcast via PubSub after each state transition.
func (s *notificationService) Create(ctx context.Context, notification *entity.Notification) error {
	if err := validateNotification(notification); err != nil {
		return err
	}

	notification.Status = entity.StatusPending

	if err := s.repo.Create(ctx, notification); err != nil {
		return fmt.Errorf("persisting notification: %w", err)
	}

	s.broadcastStatus(ctx, notification.ID, notification.Status)

	// Scheduled notifications stay in pending until the scheduler picks them up.
	if notification.ScheduledAt != nil {
		return nil
	}

	if err := s.producer.Enqueue(ctx, notification); err != nil {
		return fmt.Errorf("enqueuing notification %s: %w", notification.ID, err)
	}

	notification.Status = entity.StatusQueued
	if err := s.repo.UpdateStatus(ctx, notification.ID, entity.StatusQueued, nil, nil); err != nil {
		return fmt.Errorf("updating status to queued for notification %s: %w", notification.ID, err)
	}

	s.broadcastStatus(ctx, notification.ID, entity.StatusQueued)

	return nil
}

// CreateBatch validates and persists up to 1000 notifications in a single
// transaction, then enqueues any that are not scheduled for the future.
// A shared batch ID is generated and assigned to every notification.
func (s *notificationService) CreateBatch(ctx context.Context, notifications []*entity.Notification) error {
	if len(notifications) == 0 {
		return domainerrors.NewValidationError("notifications", "batch must contain at least one notification")
	}
	if len(notifications) > maxBatchSize {
		return domainerrors.NewValidationError("notifications", fmt.Sprintf("batch size %d exceeds maximum of %d", len(notifications), maxBatchSize))
	}

	batchID := uuid.New()

	for i, n := range notifications {
		if err := validateNotification(n); err != nil {
			return fmt.Errorf("notification at index %d: %w", i, err)
		}
		n.BatchID = &batchID
		n.Status = entity.StatusPending
	}

	if err := s.repo.CreateBatch(ctx, notifications); err != nil {
		return fmt.Errorf("persisting notification batch: %w", err)
	}

	for _, n := range notifications {
		s.broadcastStatus(ctx, n.ID, n.Status)
	}

	// Enqueue non-scheduled notifications.
	for _, n := range notifications {
		if n.ScheduledAt != nil {
			continue
		}

		if err := s.producer.Enqueue(ctx, n); err != nil {
			s.logger.ErrorContext(ctx, "failed to enqueue batch notification",
				slog.String("notification_id", n.ID.String()),
				slog.String("error", err.Error()),
			)
			continue
		}

		n.Status = entity.StatusQueued
		if err := s.repo.UpdateStatus(ctx, n.ID, entity.StatusQueued, nil, nil); err != nil {
			s.logger.ErrorContext(ctx, "failed to update status to queued",
				slog.String("notification_id", n.ID.String()),
				slog.String("error", err.Error()),
			)
			continue
		}

		s.broadcastStatus(ctx, n.ID, entity.StatusQueued)
	}

	return nil
}

// GetByID retrieves a notification by its primary key.
func (s *notificationService) GetByID(ctx context.Context, id uuid.UUID) (*entity.Notification, error) {
	n, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting notification %s: %w", id, err)
	}
	return n, nil
}

// GetByBatchID retrieves all notifications belonging to a batch with pagination.
func (s *notificationService) GetByBatchID(ctx context.Context, batchID uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error) {
	notifications, total, err := s.repo.GetByBatchID(ctx, batchID, params)
	if err != nil {
		return nil, 0, fmt.Errorf("getting notifications for batch %s: %w", batchID, err)
	}
	return notifications, total, nil
}

// Cancel marks a pending or queued notification as cancelled.
func (s *notificationService) Cancel(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.Cancel(ctx, id); err != nil {
		return fmt.Errorf("cancelling notification %s: %w", id, err)
	}

	s.broadcastStatus(ctx, id, entity.StatusCancelled)

	return nil
}

// List retrieves notifications matching the given filters.
func (s *notificationService) List(ctx context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
	notifications, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("listing notifications: %w", err)
	}
	return notifications, total, nil
}

// validateNotification runs the entity's own validation and converts any
// errors into a domain ValidationError.
func validateNotification(n *entity.Notification) error {
	errs := n.Validate()
	if len(errs) == 0 {
		return nil
	}

	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return domainerrors.NewValidationError("notification", strings.Join(msgs, "; "))
}

// broadcastStatus publishes a status update via Redis PubSub. Errors are
// logged but not propagated — status broadcasting is best-effort so that
// a PubSub outage does not block core notification processing.
func (s *notificationService) broadcastStatus(ctx context.Context, id uuid.UUID, status entity.Status) {
	payload, err := json.Marshal(statusUpdate{
		NotificationID: id.String(),
		Status:         status,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to marshal status update",
			slog.String("notification_id", id.String()),
			slog.String("error", err.Error()),
		)
		return
	}

	if err := s.pubsub.Publish(ctx, redisPubSub.NotificationsChannel(), payload); err != nil {
		s.logger.ErrorContext(ctx, "failed to publish status update",
			slog.String("notification_id", id.String()),
			slog.String("error", err.Error()),
		)
	}
}
