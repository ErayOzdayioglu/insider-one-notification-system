package httpclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDeliveryClient is a test double for DeliveryClient.
type mockDeliveryClient struct {
	sendFunc func(ctx context.Context, n *entity.Notification) (*DeliveryResponse, error)
	calls    int
}

func (m *mockDeliveryClient) Send(ctx context.Context, n *entity.Notification) (*DeliveryResponse, error) {
	m.calls++
	return m.sendFunc(ctx, n)
}

func successClient() *mockDeliveryClient {
	return &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
			return &DeliveryResponse{
				MessageID: "msg-1",
				Status:    "accepted",
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
}

func failureClient() *mockDeliveryClient {
	return &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
			return nil, errors.New("provider unavailable")
		},
	}
}

func testConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      50 * time.Millisecond,
	}
}

func dummyNotification() *entity.Notification {
	return entity.NewNotification(entity.ChannelSMS, entity.PriorityNormal, "+15551234567", "test", "key-1")
}

func TestCircuitBreaker_StartsInClosedState(t *testing.T) {
	inner := successClient()
	cb := NewCircuitBreakerClient(inner, testConfig())
	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateClosed, cbImpl.State())
}

func TestCircuitBreaker_StaysClosedOnSuccess(t *testing.T) {
	inner := successClient()
	cb := NewCircuitBreakerClient(inner, testConfig())
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		resp, err := cb.Send(ctx, dummyNotification())
		require.NoError(t, err)
		assert.NotNil(t, resp)
	}

	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateClosed, cbImpl.State())
	assert.Equal(t, 5, inner.calls)
}

func TestCircuitBreaker_OpensAfterConsecutiveFailures(t *testing.T) {
	inner := failureClient()
	cfg := testConfig()
	cb := NewCircuitBreakerClient(inner, cfg)
	ctx := context.Background()

	// Reach the failure threshold.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_, err := cb.Send(ctx, dummyNotification())
		require.Error(t, err)
	}

	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateOpen, cbImpl.State())
}

func TestCircuitBreaker_RejectsRequestsWhenOpen(t *testing.T) {
	inner := failureClient()
	cfg := testConfig()
	cb := NewCircuitBreakerClient(inner, cfg)
	ctx := context.Background()

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_, _ = cb.Send(ctx, dummyNotification())
	}
	callsAfterTrip := inner.calls

	// Subsequent requests should be rejected immediately.
	_, err := cb.Send(ctx, dummyNotification())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCircuitOpen))
	assert.Equal(t, callsAfterTrip, inner.calls, "inner client should not be called when circuit is open")
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
	inner := failureClient()
	cfg := testConfig()
	cb := NewCircuitBreakerClient(inner, cfg)
	ctx := context.Background()

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_, _ = cb.Send(ctx, dummyNotification())
	}

	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateOpen, cbImpl.State())

	// Wait for open timeout.
	time.Sleep(cfg.OpenTimeout + 10*time.Millisecond)

	// Next call should be allowed (half-open probe).
	// Use a success client for the probe.
	inner.sendFunc = func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
		return &DeliveryResponse{MessageID: "msg-2", Status: "accepted", Timestamp: time.Now()}, nil
	}

	_, err := cb.Send(ctx, dummyNotification())
	require.NoError(t, err)

	// After the call, state should be half-open (since we need SuccessThreshold successes).
	assert.Equal(t, stateHalfOpen, cbImpl.State())
}

func TestCircuitBreaker_ClosesAfterSuccessThresholdInHalfOpen(t *testing.T) {
	inner := failureClient()
	cfg := testConfig()
	cb := NewCircuitBreakerClient(inner, cfg)
	ctx := context.Background()

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_, _ = cb.Send(ctx, dummyNotification())
	}

	// Wait for timeout to transition to half-open.
	time.Sleep(cfg.OpenTimeout + 10*time.Millisecond)

	// Switch inner to success.
	inner.sendFunc = func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
		return &DeliveryResponse{MessageID: "msg-ok", Status: "accepted", Timestamp: time.Now()}, nil
	}

	// Send enough successes to close the circuit.
	for i := 0; i < cfg.SuccessThreshold; i++ {
		_, err := cb.Send(ctx, dummyNotification())
		require.NoError(t, err)
	}

	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateClosed, cbImpl.State())
}

func TestCircuitBreaker_ReturnsToOpenOnFailureInHalfOpen(t *testing.T) {
	inner := failureClient()
	cfg := testConfig()
	cb := NewCircuitBreakerClient(inner, cfg)
	ctx := context.Background()

	// Trip the breaker.
	for i := 0; i < cfg.FailureThreshold; i++ {
		_, _ = cb.Send(ctx, dummyNotification())
	}

	// Wait for timeout.
	time.Sleep(cfg.OpenTimeout + 10*time.Millisecond)

	// First probe succeeds (to enter half-open).
	inner.sendFunc = func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
		return &DeliveryResponse{MessageID: "probe", Status: "accepted", Timestamp: time.Now()}, nil
	}
	_, err := cb.Send(ctx, dummyNotification())
	require.NoError(t, err)

	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateHalfOpen, cbImpl.State())

	// Next probe fails.
	inner.sendFunc = func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
		return nil, errors.New("still broken")
	}
	_, err = cb.Send(ctx, dummyNotification())
	require.Error(t, err)

	assert.Equal(t, stateOpen, cbImpl.State())
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	callCount := 0
	inner := &mockDeliveryClient{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
			callCount++
			// Fail twice, then succeed, then fail twice again - should not trip.
			if callCount <= 2 || (callCount >= 4 && callCount <= 5) {
				return nil, errors.New("fail")
			}
			return &DeliveryResponse{MessageID: "ok", Status: "accepted", Timestamp: time.Now()}, nil
		},
	}

	cfg := testConfig() // FailureThreshold = 3
	cb := NewCircuitBreakerClient(inner, cfg)
	ctx := context.Background()

	// 2 failures.
	_, _ = cb.Send(ctx, dummyNotification())
	_, _ = cb.Send(ctx, dummyNotification())

	// 1 success resets counter.
	_, _ = cb.Send(ctx, dummyNotification())

	// 2 more failures - total consecutive is 2 (below threshold of 3).
	_, _ = cb.Send(ctx, dummyNotification())
	_, _ = cb.Send(ctx, dummyNotification())

	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateClosed, cbImpl.State(), "circuit should still be closed because success reset the counter")
}
