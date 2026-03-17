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
// Mock NotificationService
// ---------------------------------------------------------------------------

type mockNotificationService struct {
	createFunc      func(ctx context.Context, n *entity.Notification) error
	createBatchFunc func(ctx context.Context, ns []*entity.Notification) error
	getByIDFunc     func(ctx context.Context, id uuid.UUID) (*entity.Notification, error)
	getByBatchFunc  func(ctx context.Context, batchID uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error)
	cancelFunc      func(ctx context.Context, id uuid.UUID) error
	listFunc        func(ctx context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error)
}

func (m *mockNotificationService) Create(ctx context.Context, n *entity.Notification) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, n)
	}
	return nil
}

func (m *mockNotificationService) CreateBatch(ctx context.Context, ns []*entity.Notification) error {
	if m.createBatchFunc != nil {
		return m.createBatchFunc(ctx, ns)
	}
	return nil
}

func (m *mockNotificationService) GetByID(ctx context.Context, id uuid.UUID) (*entity.Notification, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockNotificationService) GetByBatchID(ctx context.Context, batchID uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error) {
	if m.getByBatchFunc != nil {
		return m.getByBatchFunc(ctx, batchID, params)
	}
	return nil, 0, nil
}

func (m *mockNotificationService) Cancel(ctx context.Context, id uuid.UUID) error {
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, id)
	}
	return nil
}

func (m *mockNotificationService) List(ctx context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, filter)
	}
	return nil, 0, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func init() {
	gin.SetMode(gin.TestMode)
}

func setupNotificationRouter(svc *mockNotificationService) *gin.Engine {
	h := NewNotificationHandler(svc)
	r := gin.New()
	g := r.Group("/api/v1/notifications")
	{
		g.POST("", h.Create)
		g.POST("/batch", h.CreateBatch)
		g.GET("", h.List)
		g.GET("/:id", h.GetByID)
		g.PATCH("/:id/cancel", h.Cancel)
	}
	return r
}

func newSubject(s string) *string { return &s }

func makeValidCreateRequest() CreateNotificationRequest {
	return CreateNotificationRequest{
		IdempotencyKey: "test-key-123",
		Channel:        "sms",
		Priority:       "normal",
		Recipient:      "+1234567890",
		Content:        "Hello world",
	}
}

func makeNotification(id uuid.UUID) *entity.Notification {
	now := time.Now().UTC()
	subj := "Test Subject"
	return &entity.Notification{
		ID:             id,
		IdempotencyKey: "test-key-123",
		Channel:        entity.ChannelSMS,
		Priority:       entity.PriorityNormal,
		Recipient:      "+1234567890",
		Subject:        &subj,
		Content:        "Hello world",
		Status:         entity.StatusPending,
		MaxAttempts:    5,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// ---------------------------------------------------------------------------
// Create handler tests
// ---------------------------------------------------------------------------

func TestNotificationHandler_Create(t *testing.T) {
	tests := []struct {
		name           string
		body           any
		setupMock      func(m *mockNotificationService)
		wantStatusCode int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name: "201 on valid request",
			body: makeValidCreateRequest(),
			setupMock: func(m *mockNotificationService) {
				m.createFunc = func(_ context.Context, n *entity.Notification) error {
					return nil
				}
			},
			wantStatusCode: http.StatusCreated,
			checkBody: func(t *testing.T, body []byte) {
				var resp CreateNotificationResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.NotEmpty(t, resp.ID)
				assert.Equal(t, "pending", resp.Status)
				assert.False(t, resp.CreatedAt.IsZero())
			},
		},
		{
			name:           "400 on invalid JSON",
			body:           "not json",
			setupMock:      func(m *mockNotificationService) {},
			wantStatusCode: http.StatusBadRequest,
			checkBody: func(t *testing.T, body []byte) {
				var resp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.NotEmpty(t, resp.Error)
			},
		},
		{
			name:           "400 on missing required fields",
			body:           map[string]string{"content": "hello"},
			setupMock:      func(m *mockNotificationService) {},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "409 on duplicate idempotency key",
			body: makeValidCreateRequest(),
			setupMock: func(m *mockNotificationService) {
				m.createFunc = func(_ context.Context, _ *entity.Notification) error {
					return domainerrors.ErrDuplicateIdempotencyKey
				}
			},
			wantStatusCode: http.StatusConflict,
			checkBody: func(t *testing.T, body []byte) {
				var resp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Contains(t, resp.Error, "duplicate idempotency key")
			},
		},
		{
			name: "422 on validation error",
			body: makeValidCreateRequest(),
			setupMock: func(m *mockNotificationService) {
				m.createFunc = func(_ context.Context, _ *entity.Notification) error {
					return domainerrors.NewValidationError("recipient", "invalid phone number")
				}
			},
			wantStatusCode: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, body []byte) {
				var resp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, "validation failed", resp.Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockNotificationService{}
			tt.setupMock(mock)
			router := setupNotificationRouter(mock)

			bodyBytes, err := json.Marshal(tt.body)
			require.NoError(t, err)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(bodyBytes))
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

func TestNotificationHandler_GetByID(t *testing.T) {
	notifID := uuid.New()

	tests := []struct {
		name           string
		urlID          string
		setupMock      func(m *mockNotificationService)
		wantStatusCode int
	}{
		{
			name:  "200 found",
			urlID: notifID.String(),
			setupMock: func(m *mockNotificationService) {
				m.getByIDFunc = func(_ context.Context, id uuid.UUID) (*entity.Notification, error) {
					return makeNotification(id), nil
				}
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name:  "404 not found",
			urlID: notifID.String(),
			setupMock: func(m *mockNotificationService) {
				m.getByIDFunc = func(_ context.Context, id uuid.UUID) (*entity.Notification, error) {
					return nil, domainerrors.NewNotFoundError("notification", id.String())
				}
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name:           "400 invalid UUID",
			urlID:          "not-a-uuid",
			setupMock:      func(m *mockNotificationService) {},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockNotificationService{}
			tt.setupMock(mock)
			router := setupNotificationRouter(mock)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/"+tt.urlID, nil)

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Cancel handler tests
// ---------------------------------------------------------------------------

func TestNotificationHandler_Cancel(t *testing.T) {
	notifID := uuid.New()

	tests := []struct {
		name           string
		urlID          string
		setupMock      func(m *mockNotificationService)
		wantStatusCode int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name:  "200 success",
			urlID: notifID.String(),
			setupMock: func(m *mockNotificationService) {
				m.cancelFunc = func(_ context.Context, _ uuid.UUID) error {
					return nil
				}
			},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp CancelNotificationResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, notifID.String(), resp.ID)
				assert.Equal(t, "cancelled", resp.Status)
			},
		},
		{
			name:  "404 not found",
			urlID: notifID.String(),
			setupMock: func(m *mockNotificationService) {
				m.cancelFunc = func(_ context.Context, id uuid.UUID) error {
					return domainerrors.NewNotFoundError("notification", id.String())
				}
			},
			wantStatusCode: http.StatusNotFound,
		},
		{
			name:           "400 invalid UUID",
			urlID:          "bad-id",
			setupMock:      func(m *mockNotificationService) {},
			wantStatusCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockNotificationService{}
			tt.setupMock(mock)
			router := setupNotificationRouter(mock)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/"+tt.urlID+"/cancel", nil)

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// List handler tests
// ---------------------------------------------------------------------------

func TestNotificationHandler_List(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		setupMock      func(m *mockNotificationService)
		wantStatusCode int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name:  "200 with pagination",
			query: "?offset=0&limit=10",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
					assert.Equal(t, 0, filter.Offset)
					assert.Equal(t, 10, filter.Limit)
					n1 := makeNotification(uuid.New())
					n2 := makeNotification(uuid.New())
					return []*entity.Notification{n1, n2}, 2, nil
				}
			},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp PaginatedNotificationsResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Len(t, resp.Notifications, 2)
				assert.Equal(t, int64(2), resp.Total)
				assert.Equal(t, 0, resp.Offset)
				assert.Equal(t, 10, resp.Limit)
			},
		},
		{
			name:  "200 with channel filter",
			query: "?channel=sms",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
					assert.NotNil(t, filter.Channel)
					assert.Equal(t, entity.ChannelSMS, *filter.Channel)
					return []*entity.Notification{}, 0, nil
				}
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name:  "200 empty list",
			query: "",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, _ repository.ListFilter) ([]*entity.Notification, int64, error) {
					return []*entity.Notification{}, 0, nil
				}
			},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp PaginatedNotificationsResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Empty(t, resp.Notifications)
				assert.Equal(t, int64(0), resp.Total)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockNotificationService{}
			tt.setupMock(mock)
			router := setupNotificationRouter(mock)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications"+tt.query, nil)

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CreateBatch handler tests
// ---------------------------------------------------------------------------

func TestNotificationHandler_CreateBatch(t *testing.T) {
	tests := []struct {
		name           string
		body           any
		setupMock      func(m *mockNotificationService)
		wantStatusCode int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name: "201 success",
			body: BatchCreateRequest{
				Notifications: []CreateNotificationRequest{
					makeValidCreateRequest(),
					{
						IdempotencyKey: "test-key-456",
						Channel:        "email",
						Priority:       "high",
						Recipient:      "user@example.com",
						Subject:        newSubject("Hello"),
						Content:        "Welcome!",
					},
				},
			},
			setupMock: func(m *mockNotificationService) {
				m.createBatchFunc = func(_ context.Context, ns []*entity.Notification) error {
					batchID := uuid.New()
					for _, n := range ns {
						n.BatchID = &batchID
					}
					return nil
				}
			},
			wantStatusCode: http.StatusCreated,
			checkBody: func(t *testing.T, body []byte) {
				var resp BatchCreateResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.NotEmpty(t, resp.BatchID)
				assert.Equal(t, 2, resp.Total)
				assert.Len(t, resp.Notifications, 2)
				for _, n := range resp.Notifications {
					assert.NotEmpty(t, n.ID)
					assert.Equal(t, "pending", n.Status)
				}
			},
		},
		{
			name:           "400 on invalid JSON",
			body:           "not json",
			setupMock:      func(m *mockNotificationService) {},
			wantStatusCode: http.StatusBadRequest,
		},
		{
			name: "409 on duplicate idempotency key in batch",
			body: BatchCreateRequest{
				Notifications: []CreateNotificationRequest{makeValidCreateRequest()},
			},
			setupMock: func(m *mockNotificationService) {
				m.createBatchFunc = func(_ context.Context, _ []*entity.Notification) error {
					return domainerrors.ErrDuplicateIdempotencyKey
				}
			},
			wantStatusCode: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockNotificationService{}
			tt.setupMock(mock)
			router := setupNotificationRouter(mock)

			bodyBytes, err := json.Marshal(tt.body)
			require.NoError(t, err)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/batch", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
		})
	}
}
