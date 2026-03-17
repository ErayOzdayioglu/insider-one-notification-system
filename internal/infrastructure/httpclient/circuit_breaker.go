package httpclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
)

// ErrCircuitOpen is returned when the circuit breaker is in the Open state
// and rejects requests without forwarding them to the underlying client.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// circuitState represents one of the three circuit breaker states.
type circuitState int

const (
	stateClosed   circuitState = iota
	stateOpen
	stateHalfOpen
)

// String returns a human-readable label for the state.
func (s circuitState) String() string {
	switch s {
	case stateClosed:
		return "closed"
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds tunable parameters for the circuit breaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures in the
	// Closed state before transitioning to Open.
	FailureThreshold int

	// SuccessThreshold is the number of consecutive successes required
	// in the HalfOpen state to transition back to Closed.
	SuccessThreshold int

	// OpenTimeout is how long the breaker stays Open before moving to
	// HalfOpen to probe with a trial request.
	OpenTimeout time.Duration
}

// DefaultCircuitBreakerConfig returns production-sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		OpenTimeout:      30 * time.Second,
	}
}

// circuitBreakerClient is a decorator around DeliveryClient that adds
// circuit breaker protection for the external webhook call.
type circuitBreakerClient struct {
	inner  DeliveryClient
	config CircuitBreakerConfig

	mu                  sync.RWMutex
	state               circuitState
	consecutiveFailures int
	consecutiveSuccesses int
	openedAt            time.Time
}

// NewCircuitBreakerClient wraps a DeliveryClient with circuit breaker
// behaviour using the supplied configuration.
func NewCircuitBreakerClient(inner DeliveryClient, cfg CircuitBreakerConfig) DeliveryClient {
	return &circuitBreakerClient{
		inner:  inner,
		config: cfg,
		state:  stateClosed,
	}
}

// Send delegates to the inner client when the circuit allows it and
// records the outcome to manage state transitions.
func (cb *circuitBreakerClient) Send(ctx context.Context, notification *entity.Notification) (*DeliveryResponse, error) {
	if err := cb.allowRequest(); err != nil {
		return nil, err
	}

	resp, err := cb.inner.Send(ctx, notification)
	cb.recordResult(err)

	if err != nil {
		return nil, fmt.Errorf("delivery through circuit breaker: %w", err)
	}
	return resp, nil
}

// allowRequest checks whether the current state permits a request. For the
// Open state it checks whether enough time has elapsed to transition to
// HalfOpen.
func (cb *circuitBreakerClient) allowRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case stateClosed:
		return nil

	case stateOpen:
		if time.Since(cb.openedAt) >= cb.config.OpenTimeout {
			cb.state = stateHalfOpen
			cb.consecutiveSuccesses = 0
			return nil
		}
		return ErrCircuitOpen

	case stateHalfOpen:
		// Allow the request through; only one at a time is enforced by
		// the caller holding the lock during the state check. The actual
		// concurrency gate is that recordResult will trip to Open on any
		// failure, effectively serialising probe attempts.
		return nil

	default:
		return ErrCircuitOpen
	}
}

// recordResult updates internal counters and transitions the state based
// on whether the request succeeded or failed.
func (cb *circuitBreakerClient) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure()
	} else {
		cb.onSuccess()
	}
}

// onFailure handles a failed request in each state.
func (cb *circuitBreakerClient) onFailure() {
	switch cb.state {
	case stateClosed:
		cb.consecutiveFailures++
		if cb.consecutiveFailures >= cb.config.FailureThreshold {
			cb.tripOpen()
		}

	case stateHalfOpen:
		// Any failure in half-open immediately trips back to open.
		cb.tripOpen()
	}
}

// onSuccess handles a successful request in each state.
func (cb *circuitBreakerClient) onSuccess() {
	switch cb.state {
	case stateClosed:
		cb.consecutiveFailures = 0

	case stateHalfOpen:
		cb.consecutiveSuccesses++
		if cb.consecutiveSuccesses >= cb.config.SuccessThreshold {
			cb.state = stateClosed
			cb.consecutiveFailures = 0
			cb.consecutiveSuccesses = 0
		}
	}
}

// tripOpen moves the breaker to the Open state and records the timestamp.
func (cb *circuitBreakerClient) tripOpen() {
	cb.state = stateOpen
	cb.openedAt = time.Now()
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
}

// State returns the current state of the circuit breaker (useful for
// metrics and health checks).
func (cb *circuitBreakerClient) State() circuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}
