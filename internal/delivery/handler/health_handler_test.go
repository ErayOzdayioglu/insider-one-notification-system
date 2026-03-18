package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock HealthChecker
// ---------------------------------------------------------------------------

type mockHealthChecker struct {
	pingFunc func(ctx context.Context) error
}

func (m *mockHealthChecker) Ping(ctx context.Context) error {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupHealthRouter(checkers map[string]HealthChecker) *gin.Engine {
	h := NewHealthHandler(checkers)
	r := gin.New()
	r.GET("/health", h.Liveness)
	r.GET("/ready", h.Readiness)
	return r
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHealthHandler_Liveness(t *testing.T) {
	router := setupHealthRouter(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
}

func TestHealthHandler_Readiness(t *testing.T) {
	tests := []struct {
		name           string
		checkers       map[string]HealthChecker
		wantStatusCode int
		wantStatus     string
	}{
		{
			name: "200 when all checks pass",
			checkers: map[string]HealthChecker{
				"postgres": &mockHealthChecker{
					pingFunc: func(_ context.Context) error { return nil },
				},
				"redis": &mockHealthChecker{
					pingFunc: func(_ context.Context) error { return nil },
				},
			},
			wantStatusCode: http.StatusOK,
			wantStatus:     "ok",
		},
		{
			name: "503 when a check fails",
			checkers: map[string]HealthChecker{
				"postgres": &mockHealthChecker{
					pingFunc: func(_ context.Context) error { return nil },
				},
				"redis": &mockHealthChecker{
					pingFunc: func(_ context.Context) error {
						return errors.New("connection refused")
					},
				},
			},
			wantStatusCode: http.StatusServiceUnavailable,
			wantStatus:     "degraded",
		},
		{
			name:           "200 with no checkers",
			checkers:       map[string]HealthChecker{},
			wantStatusCode: http.StatusOK,
			wantStatus:     "ok",
		},
		{
			name: "503 when multiple checkers and one fails",
			checkers: map[string]HealthChecker{
				"postgres": &mockHealthChecker{
					pingFunc: func(_ context.Context) error { return nil },
				},
				"redis": &mockHealthChecker{
					pingFunc: func(_ context.Context) error {
						return errors.New("connection refused")
					},
				},
				"kafka": &mockHealthChecker{
					pingFunc: func(_ context.Context) error { return nil },
				},
			},
			wantStatusCode: http.StatusServiceUnavailable,
			wantStatus:     "degraded",
		},
		{
			name: "200 when all three checkers pass",
			checkers: map[string]HealthChecker{
				"postgres": &mockHealthChecker{
					pingFunc: func(_ context.Context) error { return nil },
				},
				"redis": &mockHealthChecker{
					pingFunc: func(_ context.Context) error { return nil },
				},
				"kafka": &mockHealthChecker{
					pingFunc: func(_ context.Context) error { return nil },
				},
			},
			wantStatusCode: http.StatusOK,
			wantStatus:     "ok",
		},
		{
			name: "503 when all checkers fail",
			checkers: map[string]HealthChecker{
				"postgres": &mockHealthChecker{
					pingFunc: func(_ context.Context) error {
						return errors.New("connection timeout")
					},
				},
				"redis": &mockHealthChecker{
					pingFunc: func(_ context.Context) error {
						return errors.New("connection refused")
					},
				},
			},
			wantStatusCode: http.StatusServiceUnavailable,
			wantStatus:     "degraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupHealthRouter(tt.checkers)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/ready", nil)

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatusCode, w.Code)

			var resp ReadyResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tt.wantStatus, resp.Status)

			if tt.wantStatusCode == http.StatusServiceUnavailable {
				// Verify unhealthy services are reported.
				for name, status := range resp.Services {
					if status != "ok" {
						assert.Contains(t, status, "unhealthy", "service %s should report unhealthy", name)
					}
				}
			}
			if tt.wantStatusCode == http.StatusOK && len(tt.checkers) > 0 {
				for _, status := range resp.Services {
					assert.Equal(t, "ok", status)
				}
			}
		})
	}
}
