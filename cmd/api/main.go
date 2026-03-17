// @title           Insider One Notification System API
// @version         1.0
// @description     Event-driven notification system with SMS, Email, and Push channels
// @host            localhost:8080
// @BasePath        /api/v1

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/redis/go-redis/v9"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/delivery/handler"
	ws "github.com/erayozdayioglu/insider-one-notification-system/internal/delivery/websocket"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/infrastructure/httpclient"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/infrastructure/postgres"
	infraredis "github.com/erayozdayioglu/insider-one-notification-system/internal/infrastructure/redis"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/metrics"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/scheduler"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/service"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/template"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/tracing"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/worker"

	_ "github.com/erayozdayioglu/insider-one-notification-system/docs"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// -----------------------------------------------------------------------
	// Load configuration
	// -----------------------------------------------------------------------
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// -----------------------------------------------------------------------
	// Initialize tracing
	// -----------------------------------------------------------------------
	shutdownTracer, err := tracing.InitTracer(ctx, cfg.Tracing)
	if err != nil {
		logger.Error("failed to initialize tracer", "error", err)
		os.Exit(1)
	}
	defer shutdownTracer()
	logger.Info("tracing initialized", "enabled", cfg.Tracing.Enabled)

	// -----------------------------------------------------------------------
	// Initialize Prometheus metrics
	// -----------------------------------------------------------------------
	appMetrics := metrics.NewMetrics()
	logger.Info("prometheus metrics registered")

	// -----------------------------------------------------------------------
	// Connect to PostgreSQL
	// -----------------------------------------------------------------------
	pgPool, err := postgres.NewPool(ctx, cfg.Postgres.DSN(), cfg.Postgres.MaxConns, cfg.Postgres.MinConns)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pgPool.Close()
	logger.Info("connected to postgres", "host", cfg.Postgres.Host, "db", cfg.Postgres.DBName)

	// -----------------------------------------------------------------------
	// Run database migrations
	// -----------------------------------------------------------------------
	if err := runMigrations(cfg.Postgres.DSN()); err != nil {
		logger.Error("failed to run database migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("database migrations completed")

	// -----------------------------------------------------------------------
	// Connect to Redis
	// -----------------------------------------------------------------------
	redisClient, err := infraredis.NewClient(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()
	logger.Info("connected to redis", "addr", cfg.Redis.Addr)

	// -----------------------------------------------------------------------
	// Create repositories
	// -----------------------------------------------------------------------
	notificationRepo := postgres.NewNotificationRepository(pgPool)
	templateRepo := postgres.NewTemplateRepository(pgPool)

	// -----------------------------------------------------------------------
	// Create Redis queue producer/consumer, ensure streams
	// -----------------------------------------------------------------------
	producer := infraredis.NewStreamProducer(redisClient)

	hostname, _ := os.Hostname()
	consumerName := fmt.Sprintf("consumer-%s-%d", hostname, os.Getpid())
	consumer := infraredis.NewStreamConsumer(redisClient, consumerName)

	if err := infraredis.EnsureStreams(ctx, redisClient); err != nil {
		logger.Error("failed to ensure redis streams", "error", err)
		os.Exit(1)
	}
	logger.Info("redis streams initialized")

	// -----------------------------------------------------------------------
	// Create rate limiter
	// -----------------------------------------------------------------------
	rateLimiter := infraredis.NewTokenBucketLimiter(
		redisClient,
		cfg.RateLimit.MaxPerSecondPerChannel,
		cfg.RateLimit.BurstSize,
	)

	// -----------------------------------------------------------------------
	// Create Redis PubSub
	// -----------------------------------------------------------------------
	pubsub := infraredis.NewPubSub(redisClient)

	// -----------------------------------------------------------------------
	// Create HTTP delivery client with circuit breaker
	// -----------------------------------------------------------------------
	rawDeliveryClient := httpclient.NewDeliveryClient(cfg.Webhook)
	deliveryClient := httpclient.NewCircuitBreakerClient(rawDeliveryClient, httpclient.DefaultCircuitBreakerConfig())

	// -----------------------------------------------------------------------
	// Create template engine
	// -----------------------------------------------------------------------
	templateEngine := template.NewEngine()

	// -----------------------------------------------------------------------
	// Create services
	// -----------------------------------------------------------------------
	notificationService := service.NewNotificationService(notificationRepo, producer, pubsub, logger)
	templateService := service.NewTemplateService(templateRepo, templateEngine)

	// -----------------------------------------------------------------------
	// Create worker processor, worker pool, start pool
	// -----------------------------------------------------------------------
	proc := worker.NewProcessor(
		notificationRepo,
		consumer,
		deliveryClient,
		rateLimiter,
		pubsub,
		cfg.Worker,
	)

	pool := worker.NewWorkerPool(consumer, proc, cfg.Worker)
	pool.Start(ctx)

	// -----------------------------------------------------------------------
	// Create scheduler, start scheduler
	// -----------------------------------------------------------------------
	sched := scheduler.NewScheduler(notificationRepo, producer, cfg.Worker)
	sched.Start(ctx)

	// -----------------------------------------------------------------------
	// Create WebSocket hub, start hub
	// -----------------------------------------------------------------------
	wsHub := ws.NewHub(pubsub)
	go wsHub.Start(ctx)
	logger.Info("websocket hub started")

	// -----------------------------------------------------------------------
	// Create handlers
	// -----------------------------------------------------------------------
	notificationHandler := handler.NewNotificationHandler(notificationService)
	templateHandler := handler.NewTemplateHandler(templateService)
	healthHandler := handler.NewHealthHandler(map[string]handler.HealthChecker{
		"postgres": pgPool,
		"redis":    &redisHealthChecker{client: redisClient},
	})

	// -----------------------------------------------------------------------
	// Create router with all dependencies
	// -----------------------------------------------------------------------
	wsHandler := &ginWebSocketHandler{hub: wsHub}

	router := handler.NewRouter(handler.RouterDeps{
		Logger:              logger,
		NotificationHandler: notificationHandler,
		TemplateHandler:     templateHandler,
		HealthHandler:       healthHandler,
		WebSocketHandler:    wsHandler,
	})

	// Add metrics and tracing middleware.
	router.Use(metrics.HTTPMetricsMiddleware(appMetrics))
	router.Use(tracing.HTTPTracingMiddleware())

	// -----------------------------------------------------------------------
	// Start HTTP server
	// -----------------------------------------------------------------------
	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		logger.Info("starting HTTP server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	// -----------------------------------------------------------------------
	// Graceful shutdown
	// -----------------------------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("received shutdown signal", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	// Stop accepting new requests.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}
	logger.Info("HTTP server stopped")

	// Cancel background context to signal workers, scheduler, and hub.
	cancel()

	// Wait for workers to drain.
	pool.Stop()
	logger.Info("worker pool stopped")

	// Wait for scheduler to finish.
	sched.Stop()
	logger.Info("scheduler stopped")

	// Stop the WebSocket hub.
	wsHub.Stop()
	logger.Info("websocket hub stopped")

	logger.Info("graceful shutdown complete")
}

// runMigrations applies all pending database migrations from the migrations
// directory. It treats migrate.ErrNoChange as a non-error (database is
// already up to date).
func runMigrations(dsn string) error {
	m, err := migrate.New("file://migrations", dsn)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			slog.Error("migration source close error", "error", srcErr)
		}
		if dbErr != nil {
			slog.Error("migration database close error", "error", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}
	return nil
}

// redisHealthChecker wraps a Redis client to satisfy the handler.HealthChecker
// interface, which requires a Ping(ctx) error method.
type redisHealthChecker struct {
	client *redis.Client
}

func (r *redisHealthChecker) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// ginWebSocketHandler adapts the websocket.HandleWebSocket function to the
// handler.WebSocketHandler interface expected by the router.
type ginWebSocketHandler struct {
	hub *ws.Hub
}

func (h *ginWebSocketHandler) HandleConnection(c *gin.Context) {
	ws.HandleWebSocket(h.hub)(c.Writer, c.Request)
}
