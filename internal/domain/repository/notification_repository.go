package repository

import (
	"context"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/google/uuid"
)

// SortOrder defines the direction of result ordering.
type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

// ListFilter holds the filtering, sorting, and pagination parameters for
// querying notifications.
type ListFilter struct {
	Channel   *entity.Channel
	Status    *entity.Status
	Priority  *entity.Priority
	Recipient *string

	Offset    int
	Limit     int
	SortBy    string    // column name, e.g. "created_at"
	SortOrder SortOrder // "asc" or "desc"
}

// ListParams holds simple pagination parameters for sub-queries such as
// fetching notifications within a batch.
type ListParams struct {
	Offset int
	Limit  int
}

// NotificationRepository defines the persistence contract for notifications.
// Implementations must be safe for concurrent use.
type NotificationRepository interface {
	// Create persists a single notification. Returns ErrDuplicateIdempotencyKey
	// if the idempotency key already exists.
	Create(ctx context.Context, notification *entity.Notification) error

	// CreateBatch persists up to 1000 notifications in a single transaction.
	CreateBatch(ctx context.Context, notifications []*entity.Notification) error

	// GetByID retrieves a notification by its primary key.
	GetByID(ctx context.Context, id uuid.UUID) (*entity.Notification, error)

	// GetByBatchID retrieves all notifications belonging to a batch with
	// pagination. Returns the slice and total count.
	GetByBatchID(ctx context.Context, batchID uuid.UUID, params ListParams) ([]*entity.Notification, int64, error)

	// UpdateStatus transitions a notification to a new status, optionally
	// recording the provider message ID or error message.
	UpdateStatus(ctx context.Context, id uuid.UUID, status entity.Status, providerMsgID *string, errMsg *string) error

	// Cancel marks a pending or queued notification as cancelled. Returns
	// ErrNotFound if the notification does not exist or is already in a
	// terminal state.
	Cancel(ctx context.Context, id uuid.UUID) error

	// List retrieves notifications matching the given filters with pagination.
	// Returns the matching slice and total count.
	List(ctx context.Context, filter ListFilter) ([]*entity.Notification, int64, error)

	// FetchScheduledReady atomically selects up to limit notifications whose
	// scheduled_at has arrived and status is pending. Uses SELECT ... FOR
	// UPDATE SKIP LOCKED to support concurrent workers.
	FetchScheduledReady(ctx context.Context, limit int) ([]*entity.Notification, error)

	// FetchRetryReady atomically selects up to limit failed notifications
	// whose next_retry_at has arrived and whose attempt count is below
	// max_attempts. Uses SELECT ... FOR UPDATE SKIP LOCKED.
	FetchRetryReady(ctx context.Context, limit int) ([]*entity.Notification, error)

	// IncrementAttempts bumps the attempt counter, records the current time
	// as last_attempt_at, and optionally sets the next_retry_at.
	IncrementAttempts(ctx context.Context, id uuid.UUID, nextRetryAt *time.Time) error
}
