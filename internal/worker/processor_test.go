package worker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/queue"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/infrastructure/httpclient"
)

// ---------------------------------------------------------------------------
// Mock: NotificationRepository
// ---------------------------------------------------------------------------

type mockNotificationRepo struct {
	updateStatusCalls      []updateStatusCall
	incrementAttemptsCalls []incrementAttemptsCall
	updateStatusFunc       func(ctx context.Context, id uuid.UUID, status entity.Status, providerMsgID *string, errMsg *string) error
	incrementAttemptsFunc  func(ctx context.Context, id uuid.UUID, nextRetryAt *time.Time) error
}

type updateStatusCall struct {
	ID            uuid.UUID
	Status        entity.Status
	ProviderMsgID *string
	ErrMsg        *string
}

type incrementAttemptsCall struct {
	ID          uuid.UUID
	NextRetryAt *time.Time
}

func (m *mockNotificationRepo) Create(_ context.Context, _ *entity.Notification) error {
	return nil
}

func (m *mockNotificationRepo) CreateBatch(_ context.Context, _ []*entity.Notification) error {
	return nil
}

func (m *mockNotificationRepo) GetByID(_ context.Context, _ uuid.UUID) (*entity.Notification, error) {
	return nil, nil
}

func (m *mockNotificationRepo) GetByBatchID(_ context.Context, _ uuid.UUID, _ repository.ListParams) ([]*entity.Notification, int64, error) {
	return nil, 0, nil
}

func (m *mockNotificationRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status entity.Status, providerMsgID *string, errMsg *string) error {
	m.updateStatusCalls = append(m.updateStatusCalls, updateStatusCall{ID: id, Status: status, ProviderMsgID: providerMsgID, ErrMsg: errMsg})
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, id, status, providerMsgID, errMsg)
	}
	return nil
}

func (m *mockNotificationRepo) Cancel(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockNotificationRepo) List(_ context.Context, _ repository.ListFilter) ([]*entity.Notification, int64, error) {
	return nil, 0, nil
}

func (m *mockNotificationRepo) FetchScheduledReady(_ context.Context, _ int) ([]*entity.Notification, error) {
	return nil, nil
}

func (m *mockNotificationRepo) FetchRetryReady(_ context.Context, _ int) ([]*entity.Notification, error) {
	return nil, nil
}

func (m *mockNotificationRepo) IncrementAttempts(ctx context.Context, id uuid.UUID, nextRetryAt *time.Time) error {
	m.incrementAttemptsCalls = append(m.incrementAttemptsCalls, incrementAttemptsCall{ID: id, NextRetryAt: nextRetryAt})
	if m.incrementAttemptsFunc != nil {
		return m.incrementAttemptsFunc(ctx, id, nextRetryAt)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock: Consumer
// ---------------------------------------------------------------------------

type mockConsumer struct {
	ackCalls []ackCall
	ackFunc  func(ctx context.Context, channel entity.Channel, priority entity.Priority, msgID string) error
}

type ackCall struct {
	Channel  entity.Channel
	Priority entity.Priority
	MsgID    string
}

func (m *mockConsumer) Dequeue(_ context.Context, _ entity.Channel, _ entity.Priority, _ int64) ([]*queue.QueueMessage, error) {
	return nil, nil
}

func (m *mockConsumer) Ack(ctx context.Context, channel entity.Channel, priority entity.Priority, msgID string) error {
	m.ackCalls = append(m.ackCalls, ackCall{Channel: channel, Priority: priority, MsgID: msgID})
	if m.ackFunc != nil {
		return m.ackFunc(ctx, channel, priority, msgID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock: DeliveryClient
// ---------------------------------------------------------------------------

type mockDeliveryClient struct {
	sendFunc func(ctx context.Context, n *entity.Notification) (*httpclient.DeliveryResponse, error)
}

func (m *mockDeliveryClient) Send(ctx context.Context, n *entity.Notification) (*httpclient.DeliveryResponse, error) {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, n)
	}
	return &httpclient.DeliveryResponse{}, nil
}

// ---------------------------------------------------------------------------
// Mock: RateLimiter
// ---------------------------------------------------------------------------

type mockRateLimiter struct {
	waitFunc func(ctx context.Context, channel string) error
}

func (m *mockRateLimiter) Allow(_ context.Context, _ string) (bool, time.Duration, error) {
	return true, 0, nil
}

func (m *mockRateLimiter) Wait(ctx context.Context, channel string) error {
	if m.waitFunc != nil {
		return m.waitFunc(ctx, channel)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock: PubSub
// ---------------------------------------------------------------------------

type mockPubSub struct {
	publishCalls []publishCall
	publishFunc  func(ctx context.Context, channel string, data []byte) error
}

type publishCall struct {
	Channel string
	Data    []byte
}

func (m *mockPubSub) Publish(ctx context.Context, channel string, data []byte) error {
	m.publishCalls = append(m.publishCalls, publishCall{Channel: channel, Data: data})
	if m.publishFunc != nil {
		return m.publishFunc(ctx, channel, data)
	}
	return nil
}

func (m *mockPubSub) Subscribe(_ context.Context, _ string) (<-chan []byte, func(), error) {
	ch := make(chan []byte)
	close(ch)
	return ch, func() {}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func defaultWorkerConfig() config.WorkerConfig {
	return config.WorkerConfig{
		ConcurrencyPerChannel: 1,
		PollInterval:          100 * time.Millisecond,
		RetryMaxAttempts:      5,
		RetryBaseDelay:        1 * time.Second,
		RetryMaxDelay:         5 * time.Minute,
		SchedulerInterval:     5 * time.Second,
	}
}

func makeTestNotification() *entity.Notification {
	now := time.Now().UTC()
	return &entity.Notification{
		ID:             uuid.New(),
		IdempotencyKey: "idem-key-1",
		Channel:        entity.ChannelSMS,
		Priority:       entity.PriorityNormal,
		Recipient:      "+1234567890",
		Content:        "Test message",
		Status:         entity.StatusQueued,
		Attempts:       0,
		MaxAttempts:    5,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func makeQueueMessage(n *entity.Notification) *queue.QueueMessage {
	return &queue.QueueMessage{
		StreamMsgID:  "stream-msg-123",
		Notification: n,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestProcessor_Process_SuccessfulDelivery(t *testing.T) {
	notif := makeTestNotification()
	msg := makeQueueMessage(notif)

	repo := &mockNotificationRepo{}
	consumer := &mockConsumer{}
	providerMsgID := "provider-msg-abc"
	delivery := &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*httpclient.DeliveryResponse, error) {
			return &httpclient.DeliveryResponse{
				MessageID: providerMsgID,
				Status:    "accepted",
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
	limiter := &mockRateLimiter{}
	pubsub := &mockPubSub{}

	p := NewProcessor(repo, consumer, delivery, limiter, pubsub, defaultWorkerConfig())

	err := p.Process(context.Background(), msg)
	require.NoError(t, err)

	// Verify status transitions: processing -> delivered.
	require.Len(t, repo.updateStatusCalls, 2)
	assert.Equal(t, entity.StatusProcessing, repo.updateStatusCalls[0].Status)
	assert.Equal(t, entity.StatusDelivered, repo.updateStatusCalls[1].Status)
	assert.NotNil(t, repo.updateStatusCalls[1].ProviderMsgID)
	assert.Equal(t, providerMsgID, *repo.updateStatusCalls[1].ProviderMsgID)

	// Verify ACK was called.
	require.Len(t, consumer.ackCalls, 1)
	assert.Equal(t, notif.Channel, consumer.ackCalls[0].Channel)
	assert.Equal(t, notif.Priority, consumer.ackCalls[0].Priority)
	assert.Equal(t, msg.StreamMsgID, consumer.ackCalls[0].MsgID)

	// Verify no IncrementAttempts was called.
	assert.Empty(t, repo.incrementAttemptsCalls)

	// Verify PubSub was called (processing + delivered).
	assert.Len(t, pubsub.publishCalls, 2)
}

func TestProcessor_Process_FailedDeliveryWithRetry(t *testing.T) {
	notif := makeTestNotification()
	notif.Attempts = 1 // Already attempted once, next will be attempt 2 out of 5.
	msg := makeQueueMessage(notif)

	repo := &mockNotificationRepo{}
	consumer := &mockConsumer{}
	delivery := &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*httpclient.DeliveryResponse, error) {
			return nil, errors.New("connection timeout")
		},
	}
	limiter := &mockRateLimiter{}
	pubsub := &mockPubSub{}

	p := NewProcessor(repo, consumer, delivery, limiter, pubsub, defaultWorkerConfig())

	err := p.Process(context.Background(), msg)
	require.NoError(t, err)

	// Verify status transitions: processing -> failed.
	require.Len(t, repo.updateStatusCalls, 2)
	assert.Equal(t, entity.StatusProcessing, repo.updateStatusCalls[0].Status)
	assert.Equal(t, entity.StatusFailed, repo.updateStatusCalls[1].Status)
	assert.NotNil(t, repo.updateStatusCalls[1].ErrMsg)
	assert.Contains(t, *repo.updateStatusCalls[1].ErrMsg, "connection timeout")

	// Verify IncrementAttempts was called with a non-nil nextRetryAt (retriable).
	require.Len(t, repo.incrementAttemptsCalls, 1)
	assert.NotNil(t, repo.incrementAttemptsCalls[0].NextRetryAt, "nextRetryAt should be set for retriable failure")
	assert.True(t, repo.incrementAttemptsCalls[0].NextRetryAt.After(time.Now().UTC().Add(-time.Second)),
		"nextRetryAt should be in the future")

	// Verify ACK was called (message is acked; scheduler re-enqueues retries).
	require.Len(t, consumer.ackCalls, 1)

	// Verify PubSub was called (processing + failed).
	assert.Len(t, pubsub.publishCalls, 2)
}

func TestProcessor_Process_AllAttemptsExhausted(t *testing.T) {
	notif := makeTestNotification()
	notif.Attempts = 4  // This is the 5th attempt (0-indexed: 4), max is 5.
	notif.MaxAttempts = 5
	msg := makeQueueMessage(notif)

	repo := &mockNotificationRepo{}
	consumer := &mockConsumer{}
	delivery := &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*httpclient.DeliveryResponse, error) {
			return nil, errors.New("permanent failure")
		},
	}
	limiter := &mockRateLimiter{}
	pubsub := &mockPubSub{}

	p := NewProcessor(repo, consumer, delivery, limiter, pubsub, defaultWorkerConfig())

	err := p.Process(context.Background(), msg)
	require.NoError(t, err)

	// Verify status transitions: processing -> failed (terminal).
	require.Len(t, repo.updateStatusCalls, 2)
	assert.Equal(t, entity.StatusProcessing, repo.updateStatusCalls[0].Status)
	assert.Equal(t, entity.StatusFailed, repo.updateStatusCalls[1].Status)

	// Verify IncrementAttempts was called with nil nextRetryAt (terminal).
	require.Len(t, repo.incrementAttemptsCalls, 1)
	assert.Nil(t, repo.incrementAttemptsCalls[0].NextRetryAt, "nextRetryAt should be nil for terminal failure")

	// Verify ACK was called.
	require.Len(t, consumer.ackCalls, 1)
}

func TestProcessor_Process_RateLimiterError(t *testing.T) {
	notif := makeTestNotification()
	msg := makeQueueMessage(notif)

	repo := &mockNotificationRepo{}
	consumer := &mockConsumer{}
	delivery := &mockDeliveryClient{}
	limiter := &mockRateLimiter{
		waitFunc: func(_ context.Context, _ string) error {
			return context.DeadlineExceeded
		},
	}
	pubsub := &mockPubSub{}

	p := NewProcessor(repo, consumer, delivery, limiter, pubsub, defaultWorkerConfig())

	err := p.Process(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limiter wait")

	// Status was set to processing before the rate limiter error.
	require.Len(t, repo.updateStatusCalls, 1)
	assert.Equal(t, entity.StatusProcessing, repo.updateStatusCalls[0].Status)

	// No ACK should have been called.
	assert.Empty(t, consumer.ackCalls)
}

func TestProcessor_Process_CircuitBreakerOpen(t *testing.T) {
	notif := makeTestNotification()
	msg := makeQueueMessage(notif)

	repo := &mockNotificationRepo{}
	consumer := &mockConsumer{}
	delivery := &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*httpclient.DeliveryResponse, error) {
			return nil, fmt.Errorf("delivery through circuit breaker: %w", httpclient.ErrCircuitOpen)
		},
	}
	limiter := &mockRateLimiter{}
	pubsub := &mockPubSub{}

	p := NewProcessor(repo, consumer, delivery, limiter, pubsub, defaultWorkerConfig())

	err := p.Process(context.Background(), msg)
	require.NoError(t, err) // Process handles delivery errors internally

	// Should have: processing status, then failed status.
	require.Len(t, repo.updateStatusCalls, 2)
	assert.Equal(t, entity.StatusProcessing, repo.updateStatusCalls[0].Status)
	assert.Equal(t, entity.StatusFailed, repo.updateStatusCalls[1].Status)
	assert.NotNil(t, repo.updateStatusCalls[1].ErrMsg)
	assert.Contains(t, *repo.updateStatusCalls[1].ErrMsg, "circuit breaker")

	// Should have incremented attempts with a retry time (attempts 0 < maxAttempts 5).
	require.Len(t, repo.incrementAttemptsCalls, 1)
	assert.NotNil(t, repo.incrementAttemptsCalls[0].NextRetryAt)

	// ACK should still be called.
	require.Len(t, consumer.ackCalls, 1)
}

func TestProcessor_Process_UpdateStatusFailsAfterDelivery(t *testing.T) {
	notif := makeTestNotification()
	msg := makeQueueMessage(notif)

	callCount := 0
	repo := &mockNotificationRepo{
		updateStatusFunc: func(_ context.Context, _ uuid.UUID, status entity.Status, _ *string, _ *string) error {
			callCount++
			// First call (processing) succeeds, second call (delivered) fails.
			if callCount == 2 && status == entity.StatusDelivered {
				return errors.New("database connection lost")
			}
			return nil
		},
	}
	consumer := &mockConsumer{}
	delivery := &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*httpclient.DeliveryResponse, error) {
			return &httpclient.DeliveryResponse{
				MessageID: "provider-msg-xyz",
				Status:    "accepted",
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
	limiter := &mockRateLimiter{}
	pubsub := &mockPubSub{}

	p := NewProcessor(repo, consumer, delivery, limiter, pubsub, defaultWorkerConfig())

	err := p.Process(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "updating status to delivered")

	// UpdateStatus was called twice (processing + delivered attempt).
	assert.Equal(t, 2, callCount)

	// ACK should NOT have been called since we errored before reaching it.
	assert.Empty(t, consumer.ackCalls)
}

func TestProcessor_Process_AckFails(t *testing.T) {
	notif := makeTestNotification()
	msg := makeQueueMessage(notif)

	repo := &mockNotificationRepo{}
	consumer := &mockConsumer{
		ackFunc: func(_ context.Context, _ entity.Channel, _ entity.Priority, _ string) error {
			return errors.New("redis XACK failed")
		},
	}
	delivery := &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*httpclient.DeliveryResponse, error) {
			return &httpclient.DeliveryResponse{
				MessageID: "provider-msg-ack-fail",
				Status:    "accepted",
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
	limiter := &mockRateLimiter{}
	pubsub := &mockPubSub{}

	p := NewProcessor(repo, consumer, delivery, limiter, pubsub, defaultWorkerConfig())

	err := p.Process(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acking message")

	// Delivery was successful, status should reflect processing -> delivered.
	require.Len(t, repo.updateStatusCalls, 2)
	assert.Equal(t, entity.StatusProcessing, repo.updateStatusCalls[0].Status)
	assert.Equal(t, entity.StatusDelivered, repo.updateStatusCalls[1].Status)

	// ACK was attempted.
	require.Len(t, consumer.ackCalls, 1)
}

func TestProcessor_Process_AckFailsAfterFailedDelivery(t *testing.T) {
	notif := makeTestNotification()
	notif.Attempts = 1
	msg := makeQueueMessage(notif)

	repo := &mockNotificationRepo{}
	consumer := &mockConsumer{
		ackFunc: func(_ context.Context, _ entity.Channel, _ entity.Priority, _ string) error {
			return errors.New("redis XACK failed")
		},
	}
	delivery := &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*httpclient.DeliveryResponse, error) {
			return nil, errors.New("connection refused")
		},
	}
	limiter := &mockRateLimiter{}
	pubsub := &mockPubSub{}

	p := NewProcessor(repo, consumer, delivery, limiter, pubsub, defaultWorkerConfig())

	err := p.Process(context.Background(), msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acking failed message")

	// Status transitions: processing -> failed.
	require.Len(t, repo.updateStatusCalls, 2)
	assert.Equal(t, entity.StatusFailed, repo.updateStatusCalls[1].Status)
}
