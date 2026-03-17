package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/google/uuid"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	domainerrors "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/errors"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
)

// TemplateRepository implements repository.TemplateRepository using
// PostgreSQL via pgxpool.
type TemplateRepository struct {
	pool *pgxpool.Pool
}

// NewTemplateRepository returns a TemplateRepository backed by the given
// connection pool.
func NewTemplateRepository(pool *pgxpool.Pool) *TemplateRepository {
	return &TemplateRepository{pool: pool}
}

// Create persists a new template.
func (r *TemplateRepository) Create(ctx context.Context, t *entity.Template) error {
	variablesJSON, err := json.Marshal(t.Variables)
	if err != nil {
		return fmt.Errorf("marshaling template variables: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO templates (
			id, name, channel, subject, content,
			variables, is_active, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.ID, t.Name, t.Channel, t.Subject, t.Content,
		variablesJSON, t.IsActive, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return mapPgError(err)
	}
	return nil
}

// GetByID retrieves a template by primary key.
func (r *TemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*entity.Template, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, channel, subject, content,
			   variables, is_active, created_at, updated_at
		FROM templates
		WHERE id = $1`, id)

	t, err := scanTemplate(row)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// GetByName retrieves a template by its unique name.
func (r *TemplateRepository) GetByName(ctx context.Context, name string) (*entity.Template, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, channel, subject, content,
			   variables, is_active, created_at, updated_at
		FROM templates
		WHERE name = $1`, name)

	t, err := scanTemplate(row)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Update replaces an existing template's mutable fields.
func (r *TemplateRepository) Update(ctx context.Context, t *entity.Template) error {
	variablesJSON, err := json.Marshal(t.Variables)
	if err != nil {
		return fmt.Errorf("marshaling template variables: %w", err)
	}

	now := time.Now().UTC()

	tag, err := r.pool.Exec(ctx, `
		UPDATE templates
		SET name = $2,
			channel = $3,
			subject = $4,
			content = $5,
			variables = $6,
			is_active = $7,
			updated_at = $8
		WHERE id = $1`,
		t.ID, t.Name, t.Channel, t.Subject, t.Content,
		variablesJSON, t.IsActive, now,
	)
	if err != nil {
		return mapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return domainerrors.NewNotFoundError("template", t.ID.String())
	}

	t.UpdatedAt = now
	return nil
}

// Delete removes a template by ID (hard delete).
func (r *TemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainerrors.NewNotFoundError("template", id.String())
	}
	return nil
}

// List retrieves all templates with pagination.
func (r *TemplateRepository) List(ctx context.Context, params repository.ListParams) ([]*entity.Template, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM templates`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting templates: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, name, channel, subject, content,
			   variables, is_active, created_at, updated_at
		FROM templates
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`, params.Limit, params.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing templates: %w", err)
	}
	defer rows.Close()

	templates, err := collectTemplates(rows)
	if err != nil {
		return nil, 0, err
	}
	return templates, total, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanTemplate scans a single row into an entity.Template.
func scanTemplate(row pgx.Row) (*entity.Template, error) {
	var t entity.Template
	var variablesJSON []byte

	err := row.Scan(
		&t.ID, &t.Name, &t.Channel, &t.Subject, &t.Content,
		&variablesJSON, &t.IsActive, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("scanning template: %w", err)
	}

	if len(variablesJSON) > 0 {
		if unmarshalErr := json.Unmarshal(variablesJSON, &t.Variables); unmarshalErr != nil {
			return nil, fmt.Errorf("unmarshaling template variables: %w", unmarshalErr)
		}
	}

	return &t, nil
}

// collectTemplates scans all rows into a slice of templates.
func collectTemplates(rows pgx.Rows) ([]*entity.Template, error) {
	var result []*entity.Template
	for rows.Next() {
		var t entity.Template
		var variablesJSON []byte

		err := rows.Scan(
			&t.ID, &t.Name, &t.Channel, &t.Subject, &t.Content,
			&variablesJSON, &t.IsActive, &t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning template row: %w", err)
		}

		if len(variablesJSON) > 0 {
			if unmarshalErr := json.Unmarshal(variablesJSON, &t.Variables); unmarshalErr != nil {
				return nil, fmt.Errorf("unmarshaling template variables: %w", unmarshalErr)
			}
		}

		result = append(result, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating template rows: %w", err)
	}
	return result, nil
}
