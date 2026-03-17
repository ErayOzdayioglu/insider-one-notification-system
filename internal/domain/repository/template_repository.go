package repository

import (
	"context"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/google/uuid"
)

// TemplateRepository defines the persistence contract for message templates.
type TemplateRepository interface {
	// Create persists a new template.
	Create(ctx context.Context, template *entity.Template) error

	// GetByID retrieves a template by its primary key.
	GetByID(ctx context.Context, id uuid.UUID) (*entity.Template, error)

	// GetByName retrieves a template by its unique name.
	GetByName(ctx context.Context, name string) (*entity.Template, error)

	// Update replaces an existing template's mutable fields.
	Update(ctx context.Context, template *entity.Template) error

	// Delete removes a template by ID.
	Delete(ctx context.Context, id uuid.UUID) error

	// List retrieves all templates with pagination. Returns the slice and
	// total count.
	List(ctx context.Context, params ListParams) ([]*entity.Template, int64, error)
}
