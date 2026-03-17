package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// HTTPMetricsMiddleware returns a Gin middleware that records request duration
// and count per method, path, and response status code. The path used is the
// matched route template (e.g. "/notifications/:id") rather than the literal
// URI, which prevents high-cardinality label explosions.
func HTTPMetricsMiddleware(m *Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Process the request.
		c.Next()

		duration := time.Since(start).Seconds()

		// Use the registered route pattern to keep cardinality bounded.
		// Falls back to the raw path only when no route matched.
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}

		status := strconv.Itoa(c.Writer.Status())

		m.HTTPRequestDuration.WithLabelValues(c.Request.Method, path, status).Observe(duration)
	}
}
