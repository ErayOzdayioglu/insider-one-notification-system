package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	domainerrors "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/errors"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
)

// ---------------------------------------------------------------------------
// Mock TemplateService
// ---------------------------------------------------------------------------

type mockTemplateService struct {
	createFunc  func(ctx context.Context, t *entity.Template) error
	getByIDFunc func(ctx context.Context, id uuid.UUID) (*entity.Template, error)
	updateFunc  func(ctx context.Context, t *entity.Template) error
	deleteFunc  func(ctx context.Context, id uuid.UUID) error
	listFunc    func(ctx context.Context, params repository.ListParams) ([]*entity.Template, int64, error)
	renderFunc  func(ctx context.Context, id uuid.UUID, vars map[string]string) (string, string, error)
}

func (m *mockTemplateService) Create(ctx context.Context, t *entity.Template) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, t)
	}
	return nil
}

func (m *mockTemplateService) GetByID(ctx context.Context, id uuid.UUID) (*entity.Template, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockTemplateService) Update(ctx context.Context, t *entity.Template) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, t)
	}
	return nil
}

func (m *mockTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockTemplateService) List(ctx context.Context, params repository.ListParams) ([]*entity.Template, int64, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, params)
	}
	return nil, 0, nil
}

func (m *mockTemplateService) Render(ctx context.Context, id uuid.UUID, vars map[string]string) (string, string, error) {
	if m.renderFunc != nil {
		return m.renderFunc(ctx, id, vars)
	}
	return "", "", nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupTemplateRouter(svc *mockTemplateService) *gin.Engine {
	h := NewTemplateHandler(svc)
	r := gin.New()
	g := r.Group("/api/v1/templates")
	{
		g.POST("", h.Create)
		g.GET("/:id", h.GetByID)
		g.POST("/:id/render", h.Render)
	}
	return r
}

func makeTemplate(id uuid.UUID) *entity.Template {
	now := time.Now().UTC()
	subj := "Welcome, {{.Name}}!"
	return &entity.Template{
		ID:        id,
		Name:      "welcome_sms",
		Channel:   entity.ChannelSMS,
		Subject:   &subj,
		Content:   "Hello {{.Name}}, welcome to {{.Company}}!",
		Variables: []string{"Name", "Company"},
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// ---------------------------------------------------------------------------
// Create handler tests
// ---------------------------------------------------------------------------

func TestTemplateHandler_Create(t *testing.T) {
	tests := []struct {
		name           string
		body           any
		setupMock      func(m *mockTemplateService)
		wantStatusCode int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name: "201 success",
			body: CreateTemplateRequest{
				Name:    "welcome_sms",
				Channel: "sms",
				Content: "Hello {{.Name}}!",
			},
			setupMock: func(m *mockTemplateService) {
				m.createFunc = func(_ context.Context, _ *entity.Template) error {
					return nil
				}
			},
			wantStatusCode: http.StatusCreated,
			checkBody: func(t *testing.T, body []byte) {
				var resp TemplateResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.NotEmpty(t, resp.ID)
				assert.Equal(t, "welcome_sms", resp.Name)
				assert.Equal(t, "sms", resp.Channel)
				assert.True(t, resp.IsActive)
			},
		},
		{
			name: "422 validation error",
			body: CreateTemplateRequest{
				Name:    "bad_template",
				Channel: "sms",
				Content: "Hello {{.Name}}!",
			},
			setupMock: func(m *mockTemplateService) {
				m.createFunc = func(_ context.Context, _ *entity.Template) error {
					return domainerrors.NewValidationError("content", "template content is invalid")
				}
			},
			wantStatusCode: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, body []byte) {
				var resp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, "validation failed", resp.Error)
			},
		},
		{
			name:           "400 on invalid JSON",
			body:           "not json",
			setupMock:      func(m *mockTemplateService) {},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "400 on missing required fields",
			body:           map[string]string{"name": "test"},
			setupMock:      func(m *mockTemplateService) {},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTemplateService{}
			tt.setupMock(mock)
			router := setupTemplateRouter(mock)

			bodyBytes, err := json.Marshal(tt.body)
			require.NoError(t, err)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/templates", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetByID handler tests
// ---------------------------------------------------------------------------

func TestTemplateHandler_GetByID(t *testing.T) {
	tmplID := uuid.New()

	tests := []struct {
		name           string
		urlID          string
		setupMock      func(m *mockTemplateService)
		wantStatusCode int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name:  "200 found",
			urlID: tmplID.String(),
			setupMock: func(m *mockTemplateService) {
				m.getByIDFunc = func(_ context.Context, id uuid.UUID) (*entity.Template, error) {
					return makeTemplate(id), nil
				}
			},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp TemplateResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, tmplID.String(), resp.ID)
				assert.Equal(t, "welcome_sms", resp.Name)
			},
		},
		{
			name:  "404 not found",
			urlID: tmplID.String(),
			setupMock: func(m *mockTemplateService) {
				m.getByIDFunc = func(_ context.Context, id uuid.UUID) (*entity.Template, error) {
					return nil, domainerrors.NewNotFoundError("template", id.String())
				}
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name:           "400 invalid UUID",
			urlID:          "not-a-uuid",
			setupMock:      func(m *mockTemplateService) {},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTemplateService{}
			tt.setupMock(mock)
			router := setupTemplateRouter(mock)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/"+tt.urlID, nil)

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Render handler tests
// ---------------------------------------------------------------------------

func TestTemplateHandler_Render(t *testing.T) {
	tmplID := uuid.New()

	tests := []struct {
		name           string
		urlID          string
		body           any
		setupMock      func(m *mockTemplateService)
		wantStatusCode int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name:  "200 success",
			urlID: tmplID.String(),
			body: RenderTemplateRequest{
				Variables: map[string]string{"Name": "John", "Company": "Insider"},
			},
			setupMock: func(m *mockTemplateService) {
				m.renderFunc = func(_ context.Context, _ uuid.UUID, vars map[string]string) (string, string, error) {
					return "Welcome, John!", "Hello John, welcome to Insider!", nil
				}
			},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp RenderTemplateResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, "Welcome, John!", resp.Subject)
				assert.Equal(t, "Hello John, welcome to Insider!", resp.Content)
			},
		},
		{
			name:  "422 missing variables",
			urlID: tmplID.String(),
			body: RenderTemplateRequest{
				Variables: map[string]string{"Name": "John"},
			},
			setupMock: func(m *mockTemplateService) {
				m.renderFunc = func(_ context.Context, _ uuid.UUID, _ map[string]string) (string, string, error) {
					return "", "", domainerrors.NewValidationError("variables", "missing required variable: Company")
				}
			},
			wantStatusCode: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, body []byte) {
				var resp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, "validation failed", resp.Error)
			},
		},
		{
			name:  "404 template not found",
			urlID: tmplID.String(),
			body: RenderTemplateRequest{
				Variables: map[string]string{"Name": "John"},
			},
			setupMock: func(m *mockTemplateService) {
				m.renderFunc = func(_ context.Context, id uuid.UUID, _ map[string]string) (string, string, error) {
					return "", "", domainerrors.NewNotFoundError("template", id.String())
				}
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name:           "400 invalid UUID",
			urlID:          "bad-id",
			body:           RenderTemplateRequest{Variables: map[string]string{}},
			setupMock:      func(m *mockTemplateService) {},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name:           "400 invalid JSON body",
			urlID:          tmplID.String(),
			body:           "not json",
			setupMock:      func(m *mockTemplateService) {},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTemplateService{}
			tt.setupMock(mock)
			router := setupTemplateRouter(mock)

			bodyBytes, err := json.Marshal(tt.body)
			require.NoError(t, err)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/"+tt.urlID+"/render", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
		})
	}
}
