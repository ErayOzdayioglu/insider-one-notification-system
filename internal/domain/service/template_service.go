package service

import (
	"context"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	"github.com/google/uuid"
)

// TemplateService defines the application-level operations for managing
// message templates and rendering them with variable substitution.
type TemplateService interface {
	// Create validates and persists a new template.
	Create(ctx context.Context, template *entity.Template) error

	// GetByID retrieves a template by its primary key.
	GetByID(ctx context.Context, id uuid.UUID) (*entity.Template, error)

	// Update replaces an existing template's mutable fields.
	Update(ctx context.Context, template *entity.Template) error

	// Delete removes a template by ID.
	Delete(ctx context.Context, id uuid.UUID) error

	// List retrieves all templates with pagination.
	List(ctx context.Context, params repository.ListParams) ([]*entity.Template, int64, error)

	// Render loads the template identified by templateID, substitutes the
	// provided variables into the subject and content, and returns the
	// rendered strings. Returns ErrNotFound if the template does not exist
	// and a ValidationError if required variables are missing.
	Render(ctx context.Context, templateID uuid.UUID, vars map[string]string) (subject string, content string, err error)
}
