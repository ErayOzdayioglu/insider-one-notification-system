package httpclient

import (
	"context"
	"errors"
	"sync"
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

// threadSafeMock is a concurrency-safe mock for DeliveryClient used in
// concurrent tests. The regular mockDeliveryClient has an unprotected calls
// counter which is fine for serial tests but races under goroutines.
type threadSafeMock struct {
	sendFunc func(ctx context.Context, n *entity.Notification) (*DeliveryResponse, error)
	mu       sync.Mutex
	calls    int
}

func (m *threadSafeMock) Send(ctx context.Context, n *entity.Notification) (*DeliveryResponse, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return m.sendFunc(ctx, n)
}

func TestCircuitBreaker_ConcurrentAccessSafety(t *testing.T) {
	inner := &threadSafeMock{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
			return &DeliveryResponse{MessageID: "ok", Status: "accepted", Timestamp: time.Now()}, nil
		},
	}
	cfg := testConfig()
	cb := NewCircuitBreakerClient(inner, cfg)
	ctx := context.Background()

	const goroutines = 50
	const callsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errCh := make(chan error, goroutines*callsPerGoroutine)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				_, err := cb.Send(ctx, dummyNotification())
				if err != nil {
					errCh <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// All calls should succeed since the inner client always succeeds.
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	assert.Empty(t, errs, "expected no errors from concurrent sends with a success client")

	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateClosed, cbImpl.State(), "circuit should remain closed after concurrent successes")
}

func TestCircuitBreaker_ConcurrentAccessWithFailures(t *testing.T) {
	// Use a client that alternates between success and failure to stress
	// the concurrent state transitions.
	callMu := sync.Mutex{}
	callCount := 0
	inner := &threadSafeMock{
		sendFunc: func(_ context.Context, _ *entity.Notification) (*DeliveryResponse, error) {
			callMu.Lock()
			callCount++
			n := callCount
			callMu.Unlock()
			if n%3 == 0 {
				return nil, errors.New("transient failure")
			}
			return &DeliveryResponse{MessageID: "ok", Status: "accepted", Timestamp: time.Now()}, nil
		},
	}

	cfg := CircuitBreakerConfig{
		FailureThreshold: 100, // high threshold so we don't trip
		SuccessThreshold: 2,
		OpenTimeout:      50 * time.Millisecond,
	}
	cb := NewCircuitBreakerClient(inner, cfg)
	ctx := context.Background()

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				// We don't care about individual errors here, just that
				// the circuit breaker doesn't panic under concurrent access.
				_, _ = cb.Send(ctx, dummyNotification())
			}
		}()
	}

	wg.Wait()

	// The main assertion is that we reach this point without a data race or panic.
	cbImpl := cb.(*circuitBreakerClient)
	assert.Equal(t, stateClosed, cbImpl.State(), "circuit should remain closed with high threshold")
}

func TestCircuitBreaker_DefaultConfig(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	assert.Equal(t, 5, cfg.FailureThreshold)
	assert.Equal(t, 3, cfg.SuccessThreshold)
	assert.Equal(t, 30*time.Second, cfg.OpenTimeout)
}

func TestCircuitState_String(t *testing.T) {
	assert.Equal(t, "closed", stateClosed.String())
	assert.Equal(t, "open", stateOpen.String())
	assert.Equal(t, "half-open", stateHalfOpen.String())
	assert.Equal(t, "unknown", circuitState(99).String())
}
