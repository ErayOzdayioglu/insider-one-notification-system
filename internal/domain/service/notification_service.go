package service

import (
	"context"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	"github.com/google/uuid"
)

// NotificationService defines the application-level operations for managing
// notification lifecycle. Implementations coordinate between repositories,
// queues, and validation logic.
type NotificationService interface {
	// Create validates and persists a single notification, then enqueues it
	// for asynchronous processing.
	Create(ctx context.Context, notification *entity.Notification) error

	// CreateBatch validates and persists up to 1000 notifications in a single
	// transaction, then enqueues them all.
	CreateBatch(ctx context.Context, notifications []*entity.Notification) error

	// GetByID retrieves a notification by its primary key.
	GetByID(ctx context.Context, id uuid.UUID) (*entity.Notification, error)

	// GetByBatchID retrieves all notifications belonging to a batch with
	// pagination. Returns the slice and total count.
	GetByBatchID(ctx context.Context, batchID uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error)

	// Cancel marks a pending or queued notification as cancelled.
	Cancel(ctx context.Context, id uuid.UUID) error

	// List retrieves notifications matching the given filters.
	List(ctx context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error)
}
