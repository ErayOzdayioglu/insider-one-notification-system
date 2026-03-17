package service

import (
	"context"
	"errors"
	"testing"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	domainerrors "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/errors"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock TemplateRepository ---

type mockTemplateRepo struct {
	createFunc  func(ctx context.Context, t *entity.Template) error
	getByIDFunc func(ctx context.Context, id uuid.UUID) (*entity.Template, error)
	updateFunc  func(ctx context.Context, t *entity.Template) error
	deleteFunc  func(ctx context.Context, id uuid.UUID) error
	listFunc    func(ctx context.Context, params repository.ListParams) ([]*entity.Template, int64, error)
	getByNameFunc func(ctx context.Context, name string) (*entity.Template, error)
}

func (m *mockTemplateRepo) Create(ctx context.Context, t *entity.Template) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, t)
	}
	return nil
}

func (m *mockTemplateRepo) GetByID(ctx context.Context, id uuid.UUID) (*entity.Template, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockTemplateRepo) GetByName(ctx context.Context, name string) (*entity.Template, error) {
	if m.getByNameFunc != nil {
		return m.getByNameFunc(ctx, name)
	}
	return nil, nil
}

func (m *mockTemplateRepo) Update(ctx context.Context, t *entity.Template) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, t)
	}
	return nil
}

func (m *mockTemplateRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockTemplateRepo) List(ctx context.Context, params repository.ListParams) ([]*entity.Template, int64, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, params)
	}
	return nil, 0, nil
}

// --- Mock TemplateEngine ---

type mockTemplateEngine struct {
	renderFunc           func(content string, vars map[string]string) (string, error)
	validateVariablesFunc func(required []string, provided map[string]string) error
}

func (m *mockTemplateEngine) Render(content string, vars map[string]string) (string, error) {
	if m.renderFunc != nil {
		return m.renderFunc(content, vars)
	}
	return content, nil
}

func (m *mockTemplateEngine) ValidateVariables(required []string, provided map[string]string) error {
	if m.validateVariablesFunc != nil {
		return m.validateVariablesFunc(required, provided)
	}
	return nil
}

// --- Helpers ---

func validTestTemplate() *entity.Template {
	return entity.NewTemplate("welcome-sms", entity.ChannelSMS, "Hello {{.Name}}")
}

func validEmailTestTemplate() *entity.Template {
	tmpl := entity.NewTemplate("welcome-email", entity.ChannelEmail, "Hello {{.Name}}")
	subject := "Welcome {{.Name}}"
	tmpl.Subject = &subject
	return tmpl
}

// --- Tests ---

func TestTemplateService_Create_HappyPath(t *testing.T) {
	repo := &mockTemplateRepo{}
	eng := &mockTemplateEngine{}
	svc := NewTemplateService(repo, eng)

	err := svc.Create(context.Background(), validTestTemplate())
	require.NoError(t, err)
}

func TestTemplateService_Create_ValidationError(t *testing.T) {
	repo := &mockTemplateRepo{}
	eng := &mockTemplateEngine{}
	svc := NewTemplateService(repo, eng)

	tmpl := &entity.Template{
		ID:      uuid.New(),
		Name:    "", // missing name
		Channel: entity.ChannelSMS,
		Content: "Hello",
	}

	err := svc.Create(context.Background(), tmpl)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrInvalidInput))
}

func TestTemplateService_Create_RepoError(t *testing.T) {
	repo := &mockTemplateRepo{
		createFunc: func(_ context.Context, _ *entity.Template) error {
			return errors.New("db connection lost")
		},
	}
	eng := &mockTemplateEngine{}
	svc := NewTemplateService(repo, eng)

	err := svc.Create(context.Background(), validTestTemplate())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persisting template")
}

func TestTemplateService_Render_HappyPath(t *testing.T) {
	tmpl := validEmailTestTemplate()

	repo := &mockTemplateRepo{
		getByIDFunc: func(_ context.Context, id uuid.UUID) (*entity.Template, error) {
			if id == tmpl.ID {
				return tmpl, nil
			}
			return nil, domainerrors.NewNotFoundError("template", id.String())
		},
	}

	eng := &mockTemplateEngine{
		renderFunc: func(content string, vars map[string]string) (string, error) {
			if content == tmpl.Content {
				return "Hello Alice", nil
			}
			if content == *tmpl.Subject {
				return "Welcome Alice", nil
			}
			return content, nil
		},
	}

	svc := NewTemplateService(repo, eng)

	subject, content, err := svc.Render(context.Background(), tmpl.ID, map[string]string{"Name": "Alice"})
	require.NoError(t, err)
	assert.Equal(t, "Welcome Alice", subject)
	assert.Equal(t, "Hello Alice", content)
}

func TestTemplateService_Render_TemplateNotFound(t *testing.T) {
	repo := &mockTemplateRepo{
		getByIDFunc: func(_ context.Context, id uuid.UUID) (*entity.Template, error) {
			return nil, domainerrors.NewNotFoundError("template", id.String())
		},
	}
	eng := &mockTemplateEngine{}
	svc := NewTemplateService(repo, eng)

	_, _, err := svc.Render(context.Background(), uuid.New(), map[string]string{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrNotFound))
}

func TestTemplateService_Render_MissingVariables(t *testing.T) {
	tmpl := validTestTemplate()
	repo := &mockTemplateRepo{
		getByIDFunc: func(_ context.Context, _ uuid.UUID) (*entity.Template, error) {
			return tmpl, nil
		},
	}

	eng := &mockTemplateEngine{
		validateVariablesFunc: func(required []string, provided map[string]string) error {
			return errors.New("missing required template variables: Name")
		},
	}

	svc := NewTemplateService(repo, eng)

	_, _, err := svc.Render(context.Background(), tmpl.ID, map[string]string{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrInvalidInput))
}

func TestTemplateService_GetByID_Found(t *testing.T) {
	tmpl := validTestTemplate()
	repo := &mockTemplateRepo{
		getByIDFunc: func(_ context.Context, id uuid.UUID) (*entity.Template, error) {
			if id == tmpl.ID {
				return tmpl, nil
			}
			return nil, domainerrors.NewNotFoundError("template", id.String())
		},
	}
	eng := &mockTemplateEngine{}
	svc := NewTemplateService(repo, eng)

	result, err := svc.GetByID(context.Background(), tmpl.ID)
	require.NoError(t, err)
	assert.Equal(t, tmpl.ID, result.ID)
	assert.Equal(t, tmpl.Name, result.Name)
}

func TestTemplateService_GetByID_NotFound(t *testing.T) {
	repo := &mockTemplateRepo{
		getByIDFunc: func(_ context.Context, id uuid.UUID) (*entity.Template, error) {
			return nil, domainerrors.NewNotFoundError("template", id.String())
		},
	}
	eng := &mockTemplateEngine{}
	svc := NewTemplateService(repo, eng)

	_, err := svc.GetByID(context.Background(), uuid.New())
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrNotFound))
}
