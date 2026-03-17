package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// HealthChecker abstracts a dependency whose health can be probed.
type HealthChecker interface {
	Ping(ctx context.Context) error
}

// HealthHandler exposes liveness and readiness endpoints.
type HealthHandler struct {
	checkers map[string]HealthChecker
}

// NewHealthHandler creates a HealthHandler with the given named health checkers.
// Typical checkers include "postgres" and "redis".
func NewHealthHandler(checkers map[string]HealthChecker) *HealthHandler {
	return &HealthHandler{checkers: checkers}
}

// Liveness godoc
// @Summary      Liveness probe
// @Description  Returns 200 if the service is alive. Used by load balancers and orchestrators.
// @Tags         health
// @Produce      json
// @Success      200 {object} HealthResponse
// @Router       /health [get]
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{Status: "ok"})
}

// Readiness godoc
// @Summary      Readiness probe
// @Description  Returns 200 if all dependencies (DB, Redis) are reachable. Returns 503 otherwise.
// @Tags         health
// @Produce      json
// @Success      200 {object} ReadyResponse
// @Failure      503 {object} ReadyResponse
// @Router       /ready [get]
func (h *HealthHandler) Readiness(c *gin.Context) {
	const timeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	services := make(map[string]string, len(h.checkers))
	allHealthy := true

	for name, checker := range h.checkers {
		if err := checker.Ping(ctx); err != nil {
			services[name] = "unhealthy: " + err.Error()
			allHealthy = false
		} else {
			services[name] = "ok"
		}
	}

	status := "ok"
	httpStatus := http.StatusOK
	if !allHealthy {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}

	c.JSON(httpStatus, ReadyResponse{
		Status:   status,
		Services: services,
	})
}
