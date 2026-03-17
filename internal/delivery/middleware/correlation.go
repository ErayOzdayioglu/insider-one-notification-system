package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// HeaderCorrelationID is the HTTP header name for correlation IDs.
	HeaderCorrelationID = "X-Correlation-ID"
)

// contextKey is an unexported type to prevent context key collisions.
type contextKey string

const correlationIDKey contextKey = "correlation_id"

// CorrelationID returns a Gin middleware that ensures every request carries a
// correlation ID. If the incoming request contains an X-Correlation-ID header,
// that value is reused; otherwise a new UUID is generated. The ID is stored in
// the request context and echoed back on the response.
func CorrelationID() gin.HandlerFunc {
	return func(c *gin.Context) {
		cid := c.GetHeader(HeaderCorrelationID)
		if cid == "" {
			cid = uuid.New().String()
		}

		// Store in both Gin context (for handlers) and stdlib context (for services).
		c.Set(string(correlationIDKey), cid)
		ctx := context.WithValue(c.Request.Context(), correlationIDKey, cid)
		c.Request = c.Request.WithContext(ctx)

		c.Header(HeaderCorrelationID, cid)

		c.Next()
	}
}

// CorrelationIDFromContext extracts the correlation ID from a context. Returns
// an empty string if no correlation ID is present.
func CorrelationIDFromContext(ctx context.Context) string {
	if cid, ok := ctx.Value(correlationIDKey).(string); ok {
		return cid
	}
	return ""
}
