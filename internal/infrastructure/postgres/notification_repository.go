package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/google/uuid"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	domainerrors "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/errors"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
)

const uniqueViolationCode = "23505"

// allowedSortColumns is a whitelist of columns that may be used for ORDER BY
// to prevent SQL injection through the SortBy field.
var allowedSortColumns = map[string]bool{
	"created_at":   true,
	"updated_at":   true,
	"scheduled_at": true,
	"priority":     true,
	"status":       true,
	"channel":      true,
}

// NotificationRepository implements repository.NotificationRepository using
// PostgreSQL via pgxpool.
type NotificationRepository struct {
	pool *pgxpool.Pool
}

// NewNotificationRepository returns a NotificationRepository backed by the
// given connection pool.
func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	return &NotificationRepository{pool: pool}
}

// Create persists a single notification.
func (r *NotificationRepository) Create(ctx context.Context, n *entity.Notification) error {
	templateVarsJSON, err := marshalTemplateVars(n.TemplateVars)
	if err != nil {
		return fmt.Errorf("marshaling template vars: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO notifications (
			id, batch_id, idempotency_key, channel, priority,
			recipient, subject, content, template_id, template_vars,
			status, scheduled_at,
			attempts, max_attempts, last_attempt_at, next_retry_at,
			provider_message_id, error_message,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12,
			$13, $14, $15, $16,
			$17, $18,
			$19, $20
		)`,
		n.ID, n.BatchID, n.IdempotencyKey, n.Channel, n.Priority,
		n.Recipient, n.Subject, n.Content, n.TemplateID, templateVarsJSON,
		n.Status, n.ScheduledAt,
		n.Attempts, n.MaxAttempts, n.LastAttemptAt, n.NextRetryAt,
		n.ProviderMessageID, n.ErrorMessage,
		n.CreatedAt, n.UpdatedAt,
	)
	if err != nil {
		return mapPgError(err)
	}
	return nil
}

// CreateBatch persists up to 1000 notifications in a single INSERT within a
// transaction.
func (r *NotificationRepository) CreateBatch(ctx context.Context, notifications []*entity.Notification) error {
	if len(notifications) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Build a single INSERT with multiple value rows.
	const colCount = 20
	var b strings.Builder
	b.WriteString(`
		INSERT INTO notifications (
			id, batch_id, idempotency_key, channel, priority,
			recipient, subject, content, template_id, template_vars,
			status, scheduled_at,
			attempts, max_attempts, last_attempt_at, next_retry_at,
			provider_message_id, error_message,
			created_at, updated_at
		) VALUES `)

	args := make([]any, 0, len(notifications)*colCount)
	for i, n := range notifications {
		if i > 0 {
			b.WriteString(", ")
		}
		offset := i * colCount
		b.WriteString(fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10,
			offset+11, offset+12, offset+13, offset+14, offset+15,
			offset+16, offset+17, offset+18, offset+19, offset+20,
		))

		templateVarsJSON, marshalErr := marshalTemplateVars(n.TemplateVars)
		if marshalErr != nil {
			return fmt.Errorf("marshaling template vars for notification %s: %w", n.ID, marshalErr)
		}

		args = append(args,
			n.ID, n.BatchID, n.IdempotencyKey, n.Channel, n.Priority,
			n.Recipient, n.Subject, n.Content, n.TemplateID, templateVarsJSON,
			n.Status, n.ScheduledAt,
			n.Attempts, n.MaxAttempts, n.LastAttemptAt, n.NextRetryAt,
			n.ProviderMessageID, n.ErrorMessage,
			n.CreatedAt, n.UpdatedAt,
		)
	}

	if _, err := tx.Exec(ctx, b.String(), args...); err != nil {
		return mapPgError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing batch insert: %w", err)
	}
	return nil
}

// GetByID retrieves a notification by primary key.
func (r *NotificationRepository) GetByID(ctx context.Context, id uuid.UUID) (*entity.Notification, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT
			id, batch_id, idempotency_key, channel, priority,
			recipient, subject, content, template_id, template_vars,
			status, scheduled_at,
			attempts, max_attempts, last_attempt_at, next_retry_at,
			provider_message_id, error_message,
			created_at, updated_at
		FROM notifications
		WHERE id = $1`, id)

	n, err := scanNotification(row)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// GetByBatchID retrieves all notifications for a batch with pagination.
func (r *NotificationRepository) GetByBatchID(ctx context.Context, batchID uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE batch_id = $1`, batchID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting batch notifications: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			id, batch_id, idempotency_key, channel, priority,
			recipient, subject, content, template_id, template_vars,
			status, scheduled_at,
			attempts, max_attempts, last_attempt_at, next_retry_at,
			provider_message_id, error_message,
			created_at, updated_at
		FROM notifications
		WHERE batch_id = $1
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3`, batchID, params.Limit, params.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("querying batch notifications: %w", err)
	}
	defer rows.Close()

	notifications, err := collectNotifications(rows)
	if err != nil {
		return nil, 0, err
	}
	return notifications, total, nil
}

// UpdateStatus transitions a notification to a new status.
func (r *NotificationRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status entity.Status, providerMsgID *string, errMsg *string) error {
	now := time.Now().UTC()

	tag, err := r.pool.Exec(ctx, `
		UPDATE notifications
		SET status = $2,
			provider_message_id = COALESCE($3, provider_message_id),
			error_message = COALESCE($4, error_message),
			updated_at = $5
		WHERE id = $1`,
		id, status, providerMsgID, errMsg, now,
	)
	if err != nil {
		return fmt.Errorf("updating notification status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerrors.NewNotFoundError("notification", id.String())
	}
	return nil
}

// Cancel marks a pending or queued notification as cancelled.
func (r *NotificationRepository) Cancel(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()

	tag, err := r.pool.Exec(ctx, `
		UPDATE notifications
		SET status = $2, updated_at = $3
		WHERE id = $1 AND status IN ('pending', 'queued')`,
		id, entity.StatusCancelled, now,
	)
	if err != nil {
		return fmt.Errorf("cancelling notification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerrors.NewNotFoundError("notification", id.String())
	}
	return nil
}

// List retrieves notifications matching the given filters with pagination.
func (r *NotificationRepository) List(ctx context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
	whereClauses, args := buildWhereClause(filter)

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Count total matching rows.
	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM notifications %s", whereSQL)
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting notifications: %w", err)
	}

	// Determine sort column (whitelist-validated).
	sortCol := "created_at"
	if filter.SortBy != "" {
		if allowedSortColumns[filter.SortBy] {
			sortCol = filter.SortBy
		}
	}
	sortDir := "DESC"
	if filter.SortOrder == repository.SortAsc {
		sortDir = "ASC"
	}

	nextParam := len(args) + 1
	dataQuery := fmt.Sprintf(`
		SELECT
			id, batch_id, idempotency_key, channel, priority,
			recipient, subject, content, template_id, template_vars,
			status, scheduled_at,
			attempts, max_attempts, last_attempt_at, next_retry_at,
			provider_message_id, error_message,
			created_at, updated_at
		FROM notifications
		%s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d`,
		whereSQL, sortCol, sortDir, nextParam, nextParam+1,
	)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing notifications: %w", err)
	}
	defer rows.Close()

	notifications, err := collectNotifications(rows)
	if err != nil {
		return nil, 0, err
	}
	return notifications, total, nil
}

// FetchScheduledReady atomically selects and transitions pending notifications
// whose scheduled_at has arrived.
func (r *NotificationRepository) FetchScheduledReady(ctx context.Context, limit int) ([]*entity.Notification, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	now := time.Now().UTC()

	rows, err := tx.Query(ctx, `
		SELECT
			id, batch_id, idempotency_key, channel, priority,
			recipient, subject, content, template_id, template_vars,
			status, scheduled_at,
			attempts, max_attempts, last_attempt_at, next_retry_at,
			provider_message_id, error_message,
			created_at, updated_at
		FROM notifications
		WHERE status = 'pending'
			AND scheduled_at IS NOT NULL
			AND scheduled_at <= $1
		ORDER BY scheduled_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("selecting scheduled notifications: %w", err)
	}

	notifications, err := collectNotifications(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	if len(notifications) > 0 {
		ids := extractIDs(notifications)
		_, err = tx.Exec(ctx, `
			UPDATE notifications
			SET status = 'queued', updated_at = $1
			WHERE id = ANY($2)`, now, ids)
		if err != nil {
			return nil, fmt.Errorf("updating scheduled notifications to queued: %w", err)
		}

		for _, n := range notifications {
			n.Status = entity.StatusQueued
			n.UpdatedAt = now
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing fetch scheduled: %w", err)
	}
	return notifications, nil
}

// FetchRetryReady atomically selects and transitions failed notifications
// that are due for retry.
func (r *NotificationRepository) FetchRetryReady(ctx context.Context, limit int) ([]*entity.Notification, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	now := time.Now().UTC()

	rows, err := tx.Query(ctx, `
		SELECT
			id, batch_id, idempotency_key, channel, priority,
			recipient, subject, content, template_id, template_vars,
			status, scheduled_at,
			attempts, max_attempts, last_attempt_at, next_retry_at,
			provider_message_id, error_message,
			created_at, updated_at
		FROM notifications
		WHERE status = 'failed'
			AND next_retry_at IS NOT NULL
			AND next_retry_at <= $1
			AND attempts < max_attempts
		ORDER BY next_retry_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("selecting retry-ready notifications: %w", err)
	}

	notifications, err := collectNotifications(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	if len(notifications) > 0 {
		ids := extractIDs(notifications)
		_, err = tx.Exec(ctx, `
			UPDATE notifications
			SET status = 'queued', updated_at = $1
			WHERE id = ANY($2)`, now, ids)
		if err != nil {
			return nil, fmt.Errorf("updating retry notifications to queued: %w", err)
		}

		for _, n := range notifications {
			n.Status = entity.StatusQueued
			n.UpdatedAt = now
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing fetch retry: %w", err)
	}
	return notifications, nil
}

// IncrementAttempts bumps the attempt counter and optionally sets
// next_retry_at.
func (r *NotificationRepository) IncrementAttempts(ctx context.Context, id uuid.UUID, nextRetryAt *time.Time) error {
	now := time.Now().UTC()

	tag, err := r.pool.Exec(ctx, `
		UPDATE notifications
		SET attempts = attempts + 1,
			last_attempt_at = $2,
			next_retry_at = $3,
			updated_at = $2
		WHERE id = $1`,
		id, now, nextRetryAt,
	)
	if err != nil {
		return fmt.Errorf("incrementing attempts: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerrors.NewNotFoundError("notification", id.String())
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanNotification scans a single row into an entity.Notification.
func scanNotification(row pgx.Row) (*entity.Notification, error) {
	var n entity.Notification
	var templateVarsJSON []byte

	err := row.Scan(
		&n.ID, &n.BatchID, &n.IdempotencyKey, &n.Channel, &n.Priority,
		&n.Recipient, &n.Subject, &n.Content, &n.TemplateID, &templateVarsJSON,
		&n.Status, &n.ScheduledAt,
		&n.Attempts, &n.MaxAttempts, &n.LastAttemptAt, &n.NextRetryAt,
		&n.ProviderMessageID, &n.ErrorMessage,
		&n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("scanning notification: %w", err)
	}

	if len(templateVarsJSON) > 0 {
		if unmarshalErr := json.Unmarshal(templateVarsJSON, &n.TemplateVars); unmarshalErr != nil {
			return nil, fmt.Errorf("unmarshaling template vars: %w", unmarshalErr)
		}
	}

	return &n, nil
}

// collectNotifications scans all rows into a slice of notifications.
func collectNotifications(rows pgx.Rows) ([]*entity.Notification, error) {
	var result []*entity.Notification
	for rows.Next() {
		var n entity.Notification
		var templateVarsJSON []byte

		err := rows.Scan(
			&n.ID, &n.BatchID, &n.IdempotencyKey, &n.Channel, &n.Priority,
			&n.Recipient, &n.Subject, &n.Content, &n.TemplateID, &templateVarsJSON,
			&n.Status, &n.ScheduledAt,
			&n.Attempts, &n.MaxAttempts, &n.LastAttemptAt, &n.NextRetryAt,
			&n.ProviderMessageID, &n.ErrorMessage,
			&n.CreatedAt, &n.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning notification row: %w", err)
		}

		if len(templateVarsJSON) > 0 {
			if unmarshalErr := json.Unmarshal(templateVarsJSON, &n.TemplateVars); unmarshalErr != nil {
				return nil, fmt.Errorf("unmarshaling template vars: %w", unmarshalErr)
			}
		}

		result = append(result, &n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating notification rows: %w", err)
	}
	return result, nil
}

// buildWhereClause constructs dynamic WHERE conditions from ListFilter.
func buildWhereClause(filter repository.ListFilter) ([]string, []any) {
	var clauses []string
	var args []any
	paramIdx := 1

	if filter.Channel != nil {
		clauses = append(clauses, fmt.Sprintf("channel = $%d", paramIdx))
		args = append(args, *filter.Channel)
		paramIdx++
	}
	if filter.Status != nil {
		clauses = append(clauses, fmt.Sprintf("status = $%d", paramIdx))
		args = append(args, *filter.Status)
		paramIdx++
	}
	if filter.Priority != nil {
		clauses = append(clauses, fmt.Sprintf("priority = $%d", paramIdx))
		args = append(args, *filter.Priority)
		paramIdx++
	}
	if filter.Recipient != nil {
		clauses = append(clauses, fmt.Sprintf("recipient = $%d", paramIdx))
		args = append(args, *filter.Recipient)
		paramIdx++
	}

	return clauses, args
}

// extractIDs returns a slice of UUIDs from a slice of notifications.
func extractIDs(notifications []*entity.Notification) []uuid.UUID {
	ids := make([]uuid.UUID, len(notifications))
	for i, n := range notifications {
		ids[i] = n.ID
	}
	return ids
}

// marshalTemplateVars converts a template vars map to JSON bytes, returning
// nil when the map is empty.
func marshalTemplateVars(vars map[string]string) ([]byte, error) {
	if len(vars) == 0 {
		return nil, nil
	}
	return json.Marshal(vars)
}

// mapPgError translates PostgreSQL-specific errors to domain errors.
func mapPgError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == uniqueViolationCode {
			return domainerrors.ErrDuplicateIdempotencyKey
		}
	}
	return fmt.Errorf("postgres error: %w", err)
}
