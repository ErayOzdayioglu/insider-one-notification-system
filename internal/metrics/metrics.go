package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metric collectors for the notification system.
// Use NewMetrics to create and register a singleton instance.
type Metrics struct {
	// NotificationsCreated tracks the total number of notifications accepted
	// by the API, partitioned by delivery channel and priority level.
	NotificationsCreated *prometheus.CounterVec

	// NotificationsProcessed tracks terminal delivery outcomes (delivered or
	// failed) per channel.
	NotificationsProcessed *prometheus.CounterVec

	// NotificationLatency measures the end-to-end time from notification
	// creation to successful delivery, bucketed per channel.
	NotificationLatency *prometheus.HistogramVec

	// DeliveryDuration measures the wall-clock time of outbound HTTP calls
	// to the delivery provider, bucketed per channel.
	DeliveryDuration *prometheus.HistogramVec

	// QueueDepth reports the current number of notifications waiting in each
	// channel/priority queue.
	QueueDepth *prometheus.GaugeVec

	// ActiveWorkers reports how many worker goroutines are currently
	// processing notifications per channel.
	ActiveWorkers *prometheus.GaugeVec

	// RateLimitHits counts how many times the per-channel rate limiter
	// rejected or delayed a send attempt.
	RateLimitHits *prometheus.CounterVec

	// CircuitBreakerState exposes the current state of the circuit breaker
	// for each channel: 0 = closed, 1 = open, 2 = half-open.
	CircuitBreakerState *prometheus.GaugeVec

	// HTTPRequestDuration measures the latency of inbound HTTP API requests,
	// partitioned by method, path, and response status code.
	HTTPRequestDuration *prometheus.HistogramVec

	// WebSocketConnections tracks the number of active WebSocket connections.
	WebSocketConnections prometheus.Gauge
}

// NewMetrics creates a new Metrics instance and registers every collector
// with prometheus.DefaultRegisterer. It panics if registration fails,
// which is the expected behaviour during application startup.
func NewMetrics() *Metrics {
	m := &Metrics{
		NotificationsCreated: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "notification",
				Subsystem: "api",
				Name:      "notifications_created_total",
				Help:      "Total number of notifications created, by channel and priority.",
			},
			[]string{"channel", "priority"},
		),

		NotificationsProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "notification",
				Subsystem: "delivery",
				Name:      "notifications_processed_total",
				Help:      "Total number of notifications that reached a terminal state, by channel and status (delivered/failed).",
			},
			[]string{"channel", "status"},
		),

		NotificationLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "notification",
				Subsystem: "delivery",
				Name:      "notification_latency_seconds",
				Help:      "End-to-end latency from creation to delivery, in seconds.",
				Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
			},
			[]string{"channel"},
		),

		DeliveryDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "notification",
				Subsystem: "delivery",
				Name:      "http_call_duration_seconds",
				Help:      "Duration of outbound HTTP calls to the delivery provider, in seconds.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"channel"},
		),

		QueueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "notification",
				Subsystem: "queue",
				Name:      "depth",
				Help:      "Current number of notifications waiting in each queue.",
			},
			[]string{"channel", "priority"},
		),

		ActiveWorkers: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "notification",
				Subsystem: "worker",
				Name:      "active",
				Help:      "Number of worker goroutines currently processing notifications.",
			},
			[]string{"channel"},
		),

		RateLimitHits: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "notification",
				Subsystem: "ratelimit",
				Name:      "hits_total",
				Help:      "Total number of rate-limit rejections per channel.",
			},
			[]string{"channel"},
		),

		CircuitBreakerState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "notification",
				Subsystem: "circuitbreaker",
				Name:      "state",
				Help:      "Current circuit breaker state per channel: 0=closed, 1=open, 2=half-open.",
			},
			[]string{"channel"},
		),

		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "notification",
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "Duration of inbound HTTP requests, in seconds.",
				Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "path", "status"},
		),

		WebSocketConnections: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "notification",
				Subsystem: "websocket",
				Name:      "connections_active",
				Help:      "Number of currently active WebSocket connections.",
			},
		),
	}

	prometheus.MustRegister(
		m.NotificationsCreated,
		m.NotificationsProcessed,
		m.NotificationLatency,
		m.DeliveryDuration,
		m.QueueDepth,
		m.ActiveWorkers,
		m.RateLimitHits,
		m.CircuitBreakerState,
		m.HTTPRequestDuration,
		m.WebSocketConnections,
	)

	return m
}
