package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	domainerrors "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/errors"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	domainservice "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/service"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/template"
	"github.com/google/uuid"
)

// templateService implements domainservice.TemplateService.
type templateService struct {
	repo   repository.TemplateRepository
	engine template.TemplateEngine
}

// Compile-time interface satisfaction check.
var _ domainservice.TemplateService = (*templateService)(nil)

// NewTemplateService constructs a TemplateService with all required
// dependencies injected.
func NewTemplateService(
	repo repository.TemplateRepository,
	engine template.TemplateEngine,
) domainservice.TemplateService {
	return &templateService{
		repo:   repo,
		engine: engine,
	}
}

// Create validates and persists a new template.
func (s *templateService) Create(ctx context.Context, tmpl *entity.Template) error {
	if err := validateTemplate(tmpl); err != nil {
		return err
	}

	if err := s.repo.Create(ctx, tmpl); err != nil {
		return fmt.Errorf("persisting template: %w", err)
	}

	return nil
}

// GetByID retrieves a template by its primary key.
func (s *templateService) GetByID(ctx context.Context, id uuid.UUID) (*entity.Template, error) {
	tmpl, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting template %s: %w", id, err)
	}
	return tmpl, nil
}

// Update replaces an existing template's mutable fields.
func (s *templateService) Update(ctx context.Context, tmpl *entity.Template) error {
	if err := validateTemplate(tmpl); err != nil {
		return err
	}

	if err := s.repo.Update(ctx, tmpl); err != nil {
		return fmt.Errorf("updating template %s: %w", tmpl.ID, err)
	}

	return nil
}

// Delete removes a template by ID.
func (s *templateService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting template %s: %w", id, err)
	}
	return nil
}

// List retrieves all templates with pagination.
func (s *templateService) List(ctx context.Context, params repository.ListParams) ([]*entity.Template, int64, error) {
	templates, total, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("listing templates: %w", err)
	}
	return templates, total, nil
}

// Render loads the template identified by templateID, validates that all
// required variables are provided, and renders both the subject and content
// using the TemplateEngine. Returns ErrNotFound if the template does not exist
// and a ValidationError if required variables are missing.
func (s *templateService) Render(ctx context.Context, templateID uuid.UUID, vars map[string]string) (string, string, error) {
	tmpl, err := s.repo.GetByID(ctx, templateID)
	if err != nil {
		return "", "", fmt.Errorf("loading template for render: %w", err)
	}

	if err := s.engine.ValidateVariables(tmpl.Variables, vars); err != nil {
		return "", "", domainerrors.NewValidationError("variables", err.Error())
	}

	renderedContent, err := s.engine.Render(tmpl.Content, vars)
	if err != nil {
		return "", "", fmt.Errorf("rendering template content: %w", err)
	}

	var renderedSubject string
	if tmpl.Subject != nil && *tmpl.Subject != "" {
		renderedSubject, err = s.engine.Render(*tmpl.Subject, vars)
		if err != nil {
			return "", "", fmt.Errorf("rendering template subject: %w", err)
		}
	}

	return renderedSubject, renderedContent, nil
}

// validateTemplate runs the entity's own validation and converts any errors
// into a domain ValidationError.
func validateTemplate(t *entity.Template) error {
	errs := t.Validate()
	if len(errs) == 0 {
		return nil
	}

	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return domainerrors.NewValidationError("template", strings.Join(msgs, "; "))
}
