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

	"fmt"
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
		g.GET("/batch/:batchId", h.GetByBatchID)
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

// ---------------------------------------------------------------------------
// List handler tests — additional filter combinations
// ---------------------------------------------------------------------------

func TestNotificationHandler_List_Filters(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		setupMock      func(m *mockNotificationService)
		wantStatusCode int
		checkFilter    func(t *testing.T, filter repository.ListFilter)
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name:  "filter by status",
			query: "?status=delivered",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
					assert.NotNil(t, filter.Status)
					assert.Equal(t, entity.StatusDelivered, *filter.Status)
					return []*entity.Notification{}, 0, nil
				}
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name:  "filter by priority",
			query: "?priority=high",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
					assert.NotNil(t, filter.Priority)
					assert.Equal(t, entity.PriorityHigh, *filter.Priority)
					return []*entity.Notification{}, 0, nil
				}
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name:  "filter by recipient",
			query: "?recipient=user@example.com",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
					assert.NotNil(t, filter.Recipient)
					assert.Equal(t, "user@example.com", *filter.Recipient)
					return []*entity.Notification{}, 0, nil
				}
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name:  "filter by channel and status combined",
			query: "?channel=email&status=pending",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
					assert.NotNil(t, filter.Channel)
					assert.Equal(t, entity.ChannelEmail, *filter.Channel)
					assert.NotNil(t, filter.Status)
					assert.Equal(t, entity.StatusPending, *filter.Status)
					return []*entity.Notification{}, 0, nil
				}
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name:  "all filters combined",
			query: "?channel=push&status=failed&priority=low&recipient=device-token-abc&offset=5&limit=50&sort_by=updated_at&sort_order=asc",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
					assert.NotNil(t, filter.Channel)
					assert.Equal(t, entity.ChannelPush, *filter.Channel)
					assert.NotNil(t, filter.Status)
					assert.Equal(t, entity.StatusFailed, *filter.Status)
					assert.NotNil(t, filter.Priority)
					assert.Equal(t, entity.PriorityLow, *filter.Priority)
					assert.NotNil(t, filter.Recipient)
					assert.Equal(t, "device-token-abc", *filter.Recipient)
					assert.Equal(t, 5, filter.Offset)
					assert.Equal(t, 50, filter.Limit)
					assert.Equal(t, "updated_at", filter.SortBy)
					assert.Equal(t, repository.SortOrder("asc"), filter.SortOrder)
					return []*entity.Notification{makeNotification(uuid.New())}, 1, nil
				}
			},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp PaginatedNotificationsResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Len(t, resp.Notifications, 1)
				assert.Equal(t, int64(1), resp.Total)
				assert.Equal(t, 5, resp.Offset)
				assert.Equal(t, 50, resp.Limit)
			},
		},
		{
			name:  "500 on service error",
			query: "",
			setupMock: func(m *mockNotificationService) {
				m.listFunc = func(_ context.Context, _ repository.ListFilter) ([]*entity.Notification, int64, error) {
					return nil, 0, fmt.Errorf("database connection failed")
				}
			},
			wantStatusCode: http.StatusInternalServerError,
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
// GetByBatchID handler tests
// ---------------------------------------------------------------------------

func TestNotificationHandler_GetByBatchID(t *testing.T) {
	batchID := uuid.New()

	tests := []struct {
		name           string
		urlBatchID     string
		query          string
		setupMock      func(m *mockNotificationService)
		wantStatusCode int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name:       "200 success with pagination",
			urlBatchID: batchID.String(),
			query:      "?offset=0&limit=10",
			setupMock: func(m *mockNotificationService) {
				m.getByBatchFunc = func(_ context.Context, id uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error) {
					assert.Equal(t, batchID, id)
					assert.Equal(t, 0, params.Offset)
					assert.Equal(t, 10, params.Limit)
					bid := batchID
					n1 := makeNotification(uuid.New())
					n1.BatchID = &bid
					n2 := makeNotification(uuid.New())
					n2.BatchID = &bid
					return []*entity.Notification{n1, n2}, 5, nil
				}
			},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var resp PaginatedNotificationsResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Len(t, resp.Notifications, 2)
				assert.Equal(t, int64(5), resp.Total)
				assert.Equal(t, 0, resp.Offset)
				assert.Equal(t, 10, resp.Limit)
				// Verify batch ID is present in response
				for _, n := range resp.Notifications {
					assert.NotNil(t, n.BatchID)
					assert.Equal(t, batchID.String(), *n.BatchID)
				}
			},
		},
		{
			name:       "200 with default pagination",
			urlBatchID: batchID.String(),
			query:      "",
			setupMock: func(m *mockNotificationService) {
				m.getByBatchFunc = func(_ context.Context, _ uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error) {
					assert.Equal(t, 0, params.Offset)
					assert.Equal(t, 20, params.Limit) // defaultLimit
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
		{
			name:           "400 invalid batch UUID",
			urlBatchID:     "not-a-uuid",
			query:          "",
			setupMock:      func(m *mockNotificationService) {},
			wantStatusCode: http.StatusBadRequest,
			checkBody: func(t *testing.T, body []byte) {
				var resp ErrorResponse
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Contains(t, resp.Error, "invalid batch id")
			},
		},
		{
			name:       "500 on service error",
			urlBatchID: batchID.String(),
			query:      "",
			setupMock: func(m *mockNotificationService) {
				m.getByBatchFunc = func(_ context.Context, _ uuid.UUID, _ repository.ListParams) ([]*entity.Notification, int64, error) {
					return nil, 0, fmt.Errorf("database error")
				}
			},
			wantStatusCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockNotificationService{}
			tt.setupMock(mock)
			router := setupNotificationRouter(mock)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/batch/"+tt.urlBatchID+tt.query, nil)

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			if tt.checkBody != nil {
				tt.checkBody(t, w.Body.Bytes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Create handler tests — scheduled_at and template_id
// ---------------------------------------------------------------------------

func TestNotificationHandler_Create_ScheduledAt(t *testing.T) {
	futureTime := time.Now().UTC().Add(24 * time.Hour)

	mock := &mockNotificationService{}
	mock.createFunc = func(_ context.Context, n *entity.Notification) error {
		assert.NotNil(t, n.ScheduledAt)
		return nil
	}
	router := setupNotificationRouter(mock)

	reqBody := makeValidCreateRequest()
	reqBody.ScheduledAt = &futureTime

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp CreateNotificationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "pending", resp.Status)
}

func TestNotificationHandler_Create_TemplateID(t *testing.T) {
	templateID := uuid.New().String()

	mock := &mockNotificationService{}
	mock.createFunc = func(_ context.Context, n *entity.Notification) error {
		assert.NotNil(t, n.TemplateID)
		assert.Equal(t, templateID, n.TemplateID.String())
		return nil
	}
	router := setupNotificationRouter(mock)

	reqBody := makeValidCreateRequest()
	reqBody.TemplateID = &templateID

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

// ---------------------------------------------------------------------------
// mapDomainError tests — error mapping through handlers
// ---------------------------------------------------------------------------

func TestNotificationHandler_ErrorMapping(t *testing.T) {
	tests := []struct {
		name           string
		serviceErr     error
		wantStatusCode int
		wantErrorMsg   string
	}{
		{
			name:           "ErrRateLimited maps to 429",
			serviceErr:     domainerrors.ErrRateLimited,
			wantStatusCode: http.StatusTooManyRequests,
			wantErrorMsg:   "rate limited",
		},
		{
			name:           "ErrInvalidInput maps to 400",
			serviceErr:     domainerrors.ErrInvalidInput,
			wantStatusCode: http.StatusBadRequest,
			wantErrorMsg:   "invalid input",
		},
		{
			name:           "wrapped ErrInvalidInput maps to 400",
			serviceErr:     fmt.Errorf("bad channel value: %w", domainerrors.ErrInvalidInput),
			wantStatusCode: http.StatusBadRequest,
			wantErrorMsg:   "bad channel value: invalid input",
		},
		{
			name:           "generic error maps to 500",
			serviceErr:     fmt.Errorf("unexpected database failure"),
			wantStatusCode: http.StatusInternalServerError,
			wantErrorMsg:   "internal server error",
		},
		{
			name:           "ErrNotFound sentinel maps to 404",
			serviceErr:     domainerrors.ErrNotFound,
			wantStatusCode: http.StatusNotFound,
			wantErrorMsg:   "not found",
		},
		{
			name:           "wrapped ErrRateLimited maps to 429",
			serviceErr:     fmt.Errorf("sms channel: %w", domainerrors.ErrRateLimited),
			wantStatusCode: http.StatusTooManyRequests,
			wantErrorMsg:   "rate limited",
		},
		{
			name:           "ErrDuplicateIdempotencyKey maps to 409",
			serviceErr:     domainerrors.ErrDuplicateIdempotencyKey,
			wantStatusCode: http.StatusConflict,
			wantErrorMsg:   "duplicate idempotency key",
		},
		{
			name:           "ValidationError maps to 422",
			serviceErr:     domainerrors.NewValidationError("channel", "unsupported channel"),
			wantStatusCode: http.StatusUnprocessableEntity,
			wantErrorMsg:   "validation failed",
		},
		{
			name:           "NotFoundError maps to 404",
			serviceErr:     domainerrors.NewNotFoundError("notification", "abc-123"),
			wantStatusCode: http.StatusNotFound,
			wantErrorMsg:   "notification with id \"abc-123\" not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notifID := uuid.New()
			mock := &mockNotificationService{}
			mock.getByIDFunc = func(_ context.Context, _ uuid.UUID) (*entity.Notification, error) {
				return nil, tt.serviceErr
			}
			router := setupNotificationRouter(mock)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/"+notifID.String(), nil)

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)

			var resp ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tt.wantErrorMsg, resp.Error)
		})
	}
}
