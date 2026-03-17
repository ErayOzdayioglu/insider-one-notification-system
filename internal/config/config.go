package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Webhook  WebhookConfig
	Worker   WorkerConfig
	RateLimit RateLimitConfig
	Tracing  TracingConfig
}

type ServerConfig struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
	MaxConns int32
	MinConns int32
}

func (c PostgresConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.User, c.Password, c.Host, c.Port, c.DBName, c.SSLMode)
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type WebhookConfig struct {
	BaseURL         string
	UUID            string
	Timeout         time.Duration
	MaxIdleConns    int
	IdleConnTimeout time.Duration
}

type WorkerConfig struct {
	ConcurrencyPerChannel int
	PollInterval          time.Duration
	RetryMaxAttempts      int
	RetryBaseDelay        time.Duration
	RetryMaxDelay         time.Duration
	SchedulerInterval     time.Duration
}

type RateLimitConfig struct {
	MaxPerSecondPerChannel int
	BurstSize              int
}

type TracingConfig struct {
	Enabled     bool
	Endpoint    string
	ServiceName string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            envOrDefault("SERVER_PORT", "8080"),
			ReadTimeout:     envDurationOrDefault("SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:    envDurationOrDefault("SERVER_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: envDurationOrDefault("SERVER_SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		Postgres: PostgresConfig{
			Host:     envOrDefault("POSTGRES_HOST", "localhost"),
			Port:     envOrDefault("POSTGRES_PORT", "5432"),
			User:     envOrDefault("POSTGRES_USER", "notification"),
			Password: envOrDefault("POSTGRES_PASSWORD", "notification"),
			DBName:   envOrDefault("POSTGRES_DB", "notification_system"),
			SSLMode:  envOrDefault("POSTGRES_SSLMODE", "disable"),
			MaxConns: int32(envIntOrDefault("POSTGRES_MAX_CONNS", 20)),
			MinConns: int32(envIntOrDefault("POSTGRES_MIN_CONNS", 5)),
		},
		Redis: RedisConfig{
			Addr:     envOrDefault("REDIS_ADDR", "localhost:6379"),
			Password: envOrDefault("REDIS_PASSWORD", ""),
			DB:       envIntOrDefault("REDIS_DB", 0),
		},
		Webhook: WebhookConfig{
			BaseURL:         envOrDefault("WEBHOOK_BASE_URL", "https://webhook.site"),
			UUID:            envOrDefault("WEBHOOK_UUID", ""),
			Timeout:         envDurationOrDefault("WEBHOOK_TIMEOUT", 10*time.Second),
			MaxIdleConns:    envIntOrDefault("WEBHOOK_MAX_IDLE_CONNS", 100),
			IdleConnTimeout: envDurationOrDefault("WEBHOOK_IDLE_CONN_TIMEOUT", 90*time.Second),
		},
		Worker: WorkerConfig{
			ConcurrencyPerChannel: envIntOrDefault("WORKER_CONCURRENCY_PER_CHANNEL", 10),
			PollInterval:          envDurationOrDefault("WORKER_POLL_INTERVAL", 100*time.Millisecond),
			RetryMaxAttempts:      envIntOrDefault("WORKER_RETRY_MAX_ATTEMPTS", 5),
			RetryBaseDelay:        envDurationOrDefault("WORKER_RETRY_BASE_DELAY", 1*time.Second),
			RetryMaxDelay:         envDurationOrDefault("WORKER_RETRY_MAX_DELAY", 5*time.Minute),
			SchedulerInterval:     envDurationOrDefault("WORKER_SCHEDULER_INTERVAL", 5*time.Second),
		},
		RateLimit: RateLimitConfig{
			MaxPerSecondPerChannel: envIntOrDefault("RATE_LIMIT_PER_SECOND", 100),
			BurstSize:              envIntOrDefault("RATE_LIMIT_BURST", 100),
		},
		Tracing: TracingConfig{
			Enabled:     envBoolOrDefault("TRACING_ENABLED", true),
			Endpoint:    envOrDefault("TRACING_ENDPOINT", "localhost:4317"),
			ServiceName: envOrDefault("TRACING_SERVICE_NAME", "notification-system"),
		},
	}

	return cfg, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envIntOrDefault(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

func envDurationOrDefault(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}

func envBoolOrDefault(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}
