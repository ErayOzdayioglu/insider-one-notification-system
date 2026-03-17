package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/erayozdayioglu/insider-one-notification-system/docs"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/delivery/middleware"
)

// WebSocketHandler is the interface expected from the WebSocket delivery layer.
// It must provide a Gin handler function that upgrades HTTP connections.
type WebSocketHandler interface {
	HandleConnection(c *gin.Context)
}

// RouterDeps aggregates all dependencies needed to build the HTTP router.
type RouterDeps struct {
	Logger              *slog.Logger
	NotificationHandler *NotificationHandler
	TemplateHandler     *TemplateHandler
	HealthHandler       *HealthHandler
	WebSocketHandler    WebSocketHandler
}

// NewRouter creates and configures a Gin engine with all middleware and routes.
func NewRouter(deps RouterDeps) *gin.Engine {
	router := gin.New()

	// ---------------------------------------------------------------------------
	// Global middleware (order matters: correlation first, then logging, recovery)
	// ---------------------------------------------------------------------------
	router.Use(middleware.CorrelationID())
	router.Use(middleware.Logging(deps.Logger))
	router.Use(middleware.Recovery(deps.Logger))
	router.Use(corsMiddleware())

	// ---------------------------------------------------------------------------
	// Health and readiness (outside /api/v1 to keep probes simple)
	// ---------------------------------------------------------------------------
	router.GET("/health", deps.HealthHandler.Liveness)
	router.GET("/ready", deps.HealthHandler.Readiness)

	// ---------------------------------------------------------------------------
	// Swagger UI — serve embedded swagger.json to bypass template rendering issues
	// ---------------------------------------------------------------------------
	router.GET("/docs/swagger.json", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", docs.SwaggerJSON)
	})
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler,
		ginSwagger.URL("/docs/swagger.json"),
	))

	// ---------------------------------------------------------------------------
	// Metrics placeholder
	// ---------------------------------------------------------------------------
	router.GET("/metrics", func(c *gin.Context) {
		c.String(http.StatusOK, "# metrics endpoint placeholder\n")
	})

	// ---------------------------------------------------------------------------
	// API v1 routes
	// ---------------------------------------------------------------------------
	v1 := router.Group("/api/v1")
	{
		// Notifications
		notif := v1.Group("/notifications")
		{
			notif.POST("", deps.NotificationHandler.Create)
			notif.POST("/batch", deps.NotificationHandler.CreateBatch)
			notif.GET("", deps.NotificationHandler.List)
			notif.GET("/:id", deps.NotificationHandler.GetByID)
			notif.PATCH("/:id/cancel", deps.NotificationHandler.Cancel)
			notif.GET("/batch/:batchId", deps.NotificationHandler.GetByBatchID)
		}

		// Templates
		tmpl := v1.Group("/templates")
		{
			tmpl.POST("", deps.TemplateHandler.Create)
			tmpl.GET("", deps.TemplateHandler.List)
			tmpl.GET("/:id", deps.TemplateHandler.GetByID)
			tmpl.PUT("/:id", deps.TemplateHandler.Update)
			tmpl.DELETE("/:id", deps.TemplateHandler.Delete)
			tmpl.POST("/:id/render", deps.TemplateHandler.Render)
		}

		// WebSocket (optional, guarded by nil check)
		if deps.WebSocketHandler != nil {
			v1.GET("/ws", deps.WebSocketHandler.HandleConnection)
		}
	}

	return router
}

// corsMiddleware returns a permissive CORS configuration suitable for
// development and API consumption.
func corsMiddleware() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Correlation-ID"},
		ExposeHeaders:    []string{"X-Correlation-ID"},
		AllowCredentials: false,
		MaxAge:           86400, // 24 hours
	})
}
