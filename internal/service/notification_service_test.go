package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	domainerrors "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/errors"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock NotificationRepository ---

type mockNotificationRepo struct {
	createFunc      func(ctx context.Context, n *entity.Notification) error
	createBatchFunc func(ctx context.Context, ns []*entity.Notification) error
	getByIDFunc     func(ctx context.Context, id uuid.UUID) (*entity.Notification, error)
	updateStatusFunc func(ctx context.Context, id uuid.UUID, status entity.Status, providerMsgID *string, errMsg *string) error
	cancelFunc      func(ctx context.Context, id uuid.UUID) error
	listFunc        func(ctx context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error)
	getByBatchIDFunc func(ctx context.Context, batchID uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error)
}

func (m *mockNotificationRepo) Create(ctx context.Context, n *entity.Notification) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, n)
	}
	return nil
}

func (m *mockNotificationRepo) CreateBatch(ctx context.Context, ns []*entity.Notification) error {
	if m.createBatchFunc != nil {
		return m.createBatchFunc(ctx, ns)
	}
	return nil
}

func (m *mockNotificationRepo) GetByID(ctx context.Context, id uuid.UUID) (*entity.Notification, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockNotificationRepo) GetByBatchID(ctx context.Context, batchID uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error) {
	if m.getByBatchIDFunc != nil {
		return m.getByBatchIDFunc(ctx, batchID, params)
	}
	return nil, 0, nil
}

func (m *mockNotificationRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status entity.Status, providerMsgID *string, errMsg *string) error {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, id, status, providerMsgID, errMsg)
	}
	return nil
}

func (m *mockNotificationRepo) Cancel(ctx context.Context, id uuid.UUID) error {
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, id)
	}
	return nil
}

func (m *mockNotificationRepo) List(ctx context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockNotificationRepo) FetchScheduledReady(_ context.Context, _ int) ([]*entity.Notification, error) {
	return nil, nil
}

func (m *mockNotificationRepo) FetchRetryReady(_ context.Context, _ int) ([]*entity.Notification, error) {
	return nil, nil
}

func (m *mockNotificationRepo) IncrementAttempts(_ context.Context, _ uuid.UUID, _ *time.Time) error {
	return nil
}

// --- Mock Producer ---

type mockProducer struct {
	enqueueFunc func(ctx context.Context, n *entity.Notification) error
	enqueueCalls int
}

func (m *mockProducer) Enqueue(ctx context.Context, n *entity.Notification) error {
	m.enqueueCalls++
	if m.enqueueFunc != nil {
		return m.enqueueFunc(ctx, n)
	}
	return nil
}

// --- Mock PubSub ---

type mockPubSub struct {
	publishFunc func(ctx context.Context, channel string, data []byte) error
	publishCalls int
}

func (m *mockPubSub) Publish(ctx context.Context, channel string, data []byte) error {
	m.publishCalls++
	if m.publishFunc != nil {
		return m.publishFunc(ctx, channel, data)
	}
	return nil
}

func (m *mockPubSub) Subscribe(_ context.Context, _ string) (<-chan []byte, func(), error) {
	ch := make(chan []byte)
	return ch, func() { close(ch) }, nil
}

// --- Helpers ---

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func validTestNotification() *entity.Notification {
	return entity.NewNotification(entity.ChannelSMS, entity.PriorityNormal, "+15551234567", "Hello", "idem-key-1")
}

// --- Tests ---

func TestNotificationService_Create_HappyPath(t *testing.T) {
	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()
	n := validTestNotification()

	err := svc.Create(ctx, n)
	require.NoError(t, err)
	assert.Equal(t, entity.StatusQueued, n.Status)
	assert.Equal(t, 1, producer.enqueueCalls)
	assert.GreaterOrEqual(t, pubsub.publishCalls, 1) // at least one broadcast
}

func TestNotificationService_Create_ValidationError(t *testing.T) {
	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	n := entity.NewNotification("invalid", entity.PriorityNormal, "+15551234567", "Hello", "key-1")

	err := svc.Create(ctx, n)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrInvalidInput))
	assert.Equal(t, 0, producer.enqueueCalls)
}

func TestNotificationService_Create_DuplicateIdempotencyKey(t *testing.T) {
	repo := &mockNotificationRepo{
		createFunc: func(_ context.Context, _ *entity.Notification) error {
			return domainerrors.ErrDuplicateIdempotencyKey
		},
	}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	err := svc.Create(ctx, validTestNotification())
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrDuplicateIdempotencyKey))
	assert.Equal(t, 0, producer.enqueueCalls)
}

func TestNotificationService_CreateBatch_HappyPath(t *testing.T) {
	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	notifications := make([]*entity.Notification, 3)
	for i := 0; i < 3; i++ {
		notifications[i] = entity.NewNotification(
			entity.ChannelSMS, entity.PriorityNormal,
			"+15551234567", "Hello",
			uuid.New().String(),
		)
	}

	err := svc.CreateBatch(ctx, notifications)
	require.NoError(t, err)
	assert.Equal(t, 3, producer.enqueueCalls)

	// All should have a batch ID.
	for _, n := range notifications {
		assert.NotNil(t, n.BatchID)
	}
}

func TestNotificationService_CreateBatch_ExceedsLimit(t *testing.T) {
	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	notifications := make([]*entity.Notification, 1001)
	for i := range notifications {
		notifications[i] = validTestNotification()
	}

	err := svc.CreateBatch(ctx, notifications)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrInvalidInput))
}

func TestNotificationService_CreateBatch_EmptyBatch(t *testing.T) {
	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	err := svc.CreateBatch(ctx, []*entity.Notification{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrInvalidInput))
}

func TestNotificationService_GetByID_Found(t *testing.T) {
	expected := validTestNotification()
	repo := &mockNotificationRepo{
		getByIDFunc: func(_ context.Context, id uuid.UUID) (*entity.Notification, error) {
			if id == expected.ID {
				return expected, nil
			}
			return nil, domainerrors.NewNotFoundError("notification", id.String())
		},
	}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	result, err := svc.GetByID(ctx, expected.ID)
	require.NoError(t, err)
	assert.Equal(t, expected.ID, result.ID)
}

func TestNotificationService_GetByID_NotFound(t *testing.T) {
	repo := &mockNotificationRepo{
		getByIDFunc: func(_ context.Context, id uuid.UUID) (*entity.Notification, error) {
			return nil, domainerrors.NewNotFoundError("notification", id.String())
		},
	}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	_, err := svc.GetByID(ctx, uuid.New())
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrNotFound))
}

func TestNotificationService_Cancel_Success(t *testing.T) {
	repo := &mockNotificationRepo{
		cancelFunc: func(_ context.Context, _ uuid.UUID) error {
			return nil
		},
	}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	err := svc.Cancel(ctx, uuid.New())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, pubsub.publishCalls, 1) // broadcasts cancelled status
}

func TestNotificationService_Cancel_NotFound(t *testing.T) {
	repo := &mockNotificationRepo{
		cancelFunc: func(_ context.Context, id uuid.UUID) error {
			return domainerrors.NewNotFoundError("notification", id.String())
		},
	}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	err := svc.Cancel(ctx, uuid.New())
	require.Error(t, err)
	assert.True(t, errors.Is(err, domainerrors.ErrNotFound))
}

// --- Mock TemplateService ---

type mockTemplateSvc struct {
	renderFunc func(ctx context.Context, templateID uuid.UUID, vars map[string]string) (string, string, error)
}

func (m *mockTemplateSvc) Create(_ context.Context, _ *entity.Template) error { return nil }
func (m *mockTemplateSvc) GetByID(_ context.Context, _ uuid.UUID) (*entity.Template, error) {
	return nil, nil
}
func (m *mockTemplateSvc) Update(_ context.Context, _ *entity.Template) error { return nil }
func (m *mockTemplateSvc) Delete(_ context.Context, _ uuid.UUID) error       { return nil }
func (m *mockTemplateSvc) List(_ context.Context, _ repository.ListParams) ([]*entity.Template, int64, error) {
	return nil, 0, nil
}
func (m *mockTemplateSvc) Render(ctx context.Context, templateID uuid.UUID, vars map[string]string) (string, string, error) {
	if m.renderFunc != nil {
		return m.renderFunc(ctx, templateID, vars)
	}
	return "", "", nil
}

// --- Additional Tests ---

func TestNotificationService_Create_ScheduledNotification(t *testing.T) {
	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	n := validTestNotification()
	future := time.Now().UTC().Add(24 * time.Hour)
	n.ScheduledAt = &future

	err := svc.Create(ctx, n)
	require.NoError(t, err)

	// Scheduled notification should stay pending, NOT be enqueued.
	assert.Equal(t, entity.StatusPending, n.Status)
	assert.Equal(t, 0, producer.enqueueCalls, "scheduled notification must not be enqueued")
	// broadcastStatus should have been called once for the pending status.
	assert.Equal(t, 1, pubsub.publishCalls)
}

func TestNotificationService_Create_WithTemplateID(t *testing.T) {
	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}
	templateID := uuid.New()

	tmplSvc := &mockTemplateSvc{
		renderFunc: func(_ context.Context, id uuid.UUID, vars map[string]string) (string, string, error) {
			assert.Equal(t, templateID, id)
			return "Welcome " + vars["Name"], "Hello " + vars["Name"] + ", welcome aboard!", nil
		},
	}

	svc := NewNotificationService(repo, tmplSvc, producer, pubsub, testLogger())
	ctx := context.Background()

	n := entity.NewNotification(entity.ChannelEmail, entity.PriorityNormal, "user@example.com", "", "idem-tmpl-1")
	n.TemplateID = &templateID
	n.TemplateVars = map[string]string{"Name": "Alice"}

	err := svc.Create(ctx, n)
	require.NoError(t, err)

	// Content and subject should have been rendered from the template.
	assert.Equal(t, "Hello Alice, welcome aboard!", n.Content)
	assert.NotNil(t, n.Subject)
	assert.Equal(t, "Welcome Alice", *n.Subject)
	assert.Equal(t, entity.StatusQueued, n.Status)
	assert.Equal(t, 1, producer.enqueueCalls)
}

func TestNotificationService_CreateBatch_PartialEnqueueFailures(t *testing.T) {
	repo := &mockNotificationRepo{}
	pubsub := &mockPubSub{}

	enqueueCallIndex := 0
	producer := &mockProducer{
		enqueueFunc: func(_ context.Context, _ *entity.Notification) error {
			enqueueCallIndex++
			// Fail on the second notification.
			if enqueueCallIndex == 2 {
				return errors.New("queue temporarily unavailable")
			}
			return nil
		},
	}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	notifications := make([]*entity.Notification, 3)
	for i := 0; i < 3; i++ {
		notifications[i] = entity.NewNotification(
			entity.ChannelSMS, entity.PriorityNormal,
			"+15551234567", "Hello",
			uuid.New().String(),
		)
	}

	// CreateBatch should not return an error — enqueue failures are logged and skipped.
	err := svc.CreateBatch(ctx, notifications)
	require.NoError(t, err)

	// All three should have been attempted for enqueue.
	assert.Equal(t, 3, producer.enqueueCalls)

	// First and third should be queued, second should remain pending.
	assert.Equal(t, entity.StatusQueued, notifications[0].Status)
	assert.Equal(t, entity.StatusPending, notifications[1].Status, "failed enqueue should leave status as pending")
	assert.Equal(t, entity.StatusQueued, notifications[2].Status)
}

func TestNotificationService_List_Success(t *testing.T) {
	expected := []*entity.Notification{
		validTestNotification(),
		validTestNotification(),
	}
	var expectedTotal int64 = 42

	repo := &mockNotificationRepo{
		listFunc: func(_ context.Context, filter repository.ListFilter) ([]*entity.Notification, int64, error) {
			assert.Equal(t, 10, filter.Limit)
			assert.Equal(t, 0, filter.Offset)
			return expected, expectedTotal, nil
		},
	}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	filter := repository.ListFilter{Limit: 10, Offset: 0}
	results, total, err := svc.List(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, expectedTotal, total)
}

func TestNotificationService_GetByBatchID_Success(t *testing.T) {
	batchID := uuid.New()
	expected := []*entity.Notification{
		validTestNotification(),
		validTestNotification(),
	}
	var expectedTotal int64 = 2

	repo := &mockNotificationRepo{
		getByBatchIDFunc: func(_ context.Context, id uuid.UUID, params repository.ListParams) ([]*entity.Notification, int64, error) {
			assert.Equal(t, batchID, id)
			assert.Equal(t, 0, params.Offset)
			assert.Equal(t, 20, params.Limit)
			return expected, expectedTotal, nil
		},
	}
	producer := &mockProducer{}
	pubsub := &mockPubSub{}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	params := repository.ListParams{Offset: 0, Limit: 20}
	results, total, err := svc.GetByBatchID(ctx, batchID, params)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, expectedTotal, total)
}

func TestNotificationService_BroadcastStatus_PublishInvoked(t *testing.T) {
	var publishedData []byte
	var publishedChannel string

	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{
		publishFunc: func(_ context.Context, channel string, data []byte) error {
			publishedChannel = channel
			publishedData = data
			return nil
		},
	}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	n := validTestNotification()
	err := svc.Create(ctx, n)
	require.NoError(t, err)

	// Verify that pubsub.Publish was called at least twice (pending + queued).
	assert.GreaterOrEqual(t, pubsub.publishCalls, 2)

	// Verify the last published message contains the notification ID and queued status.
	assert.NotEmpty(t, publishedData)
	assert.NotEmpty(t, publishedChannel)
	assert.Contains(t, string(publishedData), n.ID.String())
	assert.Contains(t, string(publishedData), string(entity.StatusQueued))
}

func TestNotificationService_BroadcastStatus_PublishError_DoesNotBlockCreate(t *testing.T) {
	repo := &mockNotificationRepo{}
	producer := &mockProducer{}
	pubsub := &mockPubSub{
		publishFunc: func(_ context.Context, _ string, _ []byte) error {
			return errors.New("redis connection refused")
		},
	}

	svc := NewNotificationService(repo, nil, producer, pubsub, testLogger())
	ctx := context.Background()

	n := validTestNotification()
	err := svc.Create(ctx, n)
	// Create should still succeed even when PubSub fails.
	require.NoError(t, err)
	assert.Equal(t, entity.StatusQueued, n.Status)
}
