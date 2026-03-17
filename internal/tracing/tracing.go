package tracing

import (
	"context"
	"fmt"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// InitTracer configures the global OpenTelemetry TracerProvider. When tracing
// is disabled in the configuration, it installs a no-op provider and returns a
// nil-safe shutdown function. The returned shutdown function flushes pending
// spans and releases resources; callers must invoke it during graceful
// shutdown.
func InitTracer(ctx context.Context, cfg config.TracingConfig) (shutdown func(), err error) {
	// No-op path: tracing disabled.
	if !cfg.Enabled {
		return func() {}, nil
	}

	// Build a resource describing this service.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTEL resource: %w", err)
	}

	// Set up the OTLP/gRPC exporter targeting Jaeger's OTLP receiver.
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP gRPC exporter: %w", err)
	}

	// Assemble the TracerProvider with a batching span processor for
	// production-grade throughput.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Install as the global provider and configure W3C trace-context
	// propagation so traces flow across service boundaries.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown = func() {
		// Use a background context for shutdown so it is not cancelled early.
		_ = tp.Shutdown(context.Background())
	}

	return shutdown, nil
}
