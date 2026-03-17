package tracing

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/erayozdayioglu/insider-one-notification-system/internal/tracing"

// HTTPTracingMiddleware returns a Gin middleware that starts a new span for
// every inbound request. It:
//
//  1. Extracts W3C trace-context headers from the incoming request so that
//     distributed traces propagate across service boundaries.
//  2. Creates a server span with method and route attributes.
//  3. Records the HTTP status code and sets the span status when the
//     handler chain completes.
//  4. Attaches a correlation ID (from the X-Correlation-ID header, if
//     present) as a span attribute for cross-referencing with structured
//     logs.
func HTTPTracingMiddleware() gin.HandlerFunc {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(c *gin.Context) {
		// Extract any incoming trace context from request headers.
		ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// Derive a human-readable span name from the route template.
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		spanName := fmt.Sprintf("%s %s", c.Request.Method, route)

		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.HTTPRoute(route),
				semconv.URLPath(c.Request.URL.Path),
			),
		)
		defer span.End()

		// Attach correlation ID when available.
		if correlationID := c.GetHeader("X-Correlation-ID"); correlationID != "" {
			span.SetAttributes(attribute.String("correlation.id", correlationID))
		}

		// Replace the request context so downstream handlers see the span.
		c.Request = c.Request.WithContext(ctx)

		// Process the request.
		c.Next()

		// Record the response status.
		statusCode := c.Writer.Status()
		span.SetAttributes(semconv.HTTPResponseStatusCode(statusCode))

		if statusCode >= 500 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}
	}
}
