package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/queue"
)

// poolMockConsumer implements queue.Consumer for pool tests with controllable
// Dequeue behavior.
type poolMockConsumer struct {
	dequeueFunc func(ctx context.Context, channel entity.Channel, priority entity.Priority, count int64) ([]*queue.QueueMessage, error)
	ackFunc     func(ctx context.Context, channel entity.Channel, priority entity.Priority, msgID string) error
}

func (m *poolMockConsumer) Dequeue(ctx context.Context, channel entity.Channel, priority entity.Priority, count int64) ([]*queue.QueueMessage, error) {
	if m.dequeueFunc != nil {
		return m.dequeueFunc(ctx, channel, priority, count)
	}
	// Default: return empty to simulate an idle queue.
	return nil, nil
}

func (m *poolMockConsumer) Ack(ctx context.Context, channel entity.Channel, priority entity.Priority, msgID string) error {
	if m.ackFunc != nil {
		return m.ackFunc(ctx, channel, priority, msgID)
	}
	return nil
}

func TestNewWorkerPool_Creates(t *testing.T) {
	consumer := &poolMockConsumer{}
	cfg := config.WorkerConfig{
		ConcurrencyPerChannel: 2,
		PollInterval:          50 * time.Millisecond,
		RetryMaxAttempts:      5,
		RetryBaseDelay:        1 * time.Second,
		RetryMaxDelay:         5 * time.Minute,
		SchedulerInterval:     5 * time.Second,
	}

	// Create a minimal processor (all nil dependencies are fine since we
	// won't actually process messages in this test).
	processor := NewProcessor(
		&mockNotificationRepo{},
		consumer,
		&mockDeliveryClient{},
		&mockRateLimiter{},
		&mockPubSub{},
		cfg,
	)

	pool := NewWorkerPool(consumer, processor, cfg)
	require.NotNil(t, pool)
	assert.Equal(t, cfg.ConcurrencyPerChannel, pool.cfg.ConcurrencyPerChannel)
	assert.NotNil(t, pool.consumer)
	assert.NotNil(t, pool.processor)
}

func TestWorkerPool_StartAndStop_Graceful(t *testing.T) {
	consumer := &poolMockConsumer{
		dequeueFunc: func(ctx context.Context, _ entity.Channel, _ entity.Priority, _ int64) ([]*queue.QueueMessage, error) {
			// Block until context is cancelled, simulating a blocking dequeue.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	cfg := config.WorkerConfig{
		ConcurrencyPerChannel: 1,
		PollInterval:          50 * time.Millisecond,
		RetryMaxAttempts:      5,
		RetryBaseDelay:        1 * time.Second,
		RetryMaxDelay:         5 * time.Minute,
		SchedulerInterval:     5 * time.Second,
	}

	processor := NewProcessor(
		&mockNotificationRepo{},
		consumer,
		&mockDeliveryClient{},
		&mockRateLimiter{},
		&mockPubSub{},
		cfg,
	)

	pool := NewWorkerPool(consumer, processor, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	pool.Start(ctx)

	// Give workers a moment to start.
	time.Sleep(20 * time.Millisecond)

	// Cancel the context to trigger shutdown.
	cancel()

	// Stop should return promptly (within a reasonable timeout).
	done := make(chan struct{})
	go func() {
		pool.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success: pool stopped gracefully.
	case <-time.After(5 * time.Second):
		t.Fatal("worker pool did not stop within 5 seconds")
	}
}

func TestWorkerPool_StartAndStop_EmptyQueues(t *testing.T) {
	// Consumer that always returns empty results (no messages).
	consumer := &poolMockConsumer{}

	cfg := config.WorkerConfig{
		ConcurrencyPerChannel: 1,
		PollInterval:          10 * time.Millisecond, // short poll for fast test
		RetryMaxAttempts:      5,
		RetryBaseDelay:        1 * time.Second,
		RetryMaxDelay:         5 * time.Minute,
		SchedulerInterval:     5 * time.Second,
	}

	processor := NewProcessor(
		&mockNotificationRepo{},
		consumer,
		&mockDeliveryClient{},
		&mockRateLimiter{},
		&mockPubSub{},
		cfg,
	)

	pool := NewWorkerPool(consumer, processor, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	pool.Start(ctx)

	// Wait for context timeout, then stop.
	<-ctx.Done()

	done := make(chan struct{})
	go func() {
		pool.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success.
	case <-time.After(5 * time.Second):
		t.Fatal("worker pool did not stop within 5 seconds after context timeout")
	}
}
