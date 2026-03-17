# Notification System

Event-driven notification system that processes and delivers messages through **SMS**, **Email**, and **Push** channels. Built with Go, designed for high throughput, burst traffic, intelligent retries, and real-time status tracking.

## Table of Contents

- [Architecture](#architecture)
- [Tech Stack](#tech-stack)
- [Getting Started](#getting-started)
- [API Reference](#api-reference)
- [WebSocket](#websocket)
- [Design Decisions](#design-decisions)
- [Testing](#testing)
- [Observability](#observability)
- [Project Structure](#project-structure)

## Architecture

```
                    ┌──────────────┐
                    │   Clients    │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │   Gin API    │──── /swagger, /metrics, /health
                    │  (REST + WS) │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
      ┌───────▼──┐  ┌──────▼──┐  ┌─────▼──────┐
      │Notif Svc │  │Tmpl Svc │  │  WebSocket  │
      └───────┬──┘  └─────────┘  │    Hub      │
              │                   └──────▲──────┘
              │                          │
     ┌────────▼────────┐          Redis Pub/Sub
     │   PostgreSQL     │                │
     │  (persistence)   │         ┌──────┴──────┐
     └─────────────────┘         │Redis Streams │
                                  │ (9 queues)   │
                                  └──────┬──────┘
                                         │
                                  ┌──────▼──────┐
                                  │ Worker Pool  │
                                  │ (per channel)│
                                  └──────┬──────┘
                                         │
                              ┌──────────┼──────────┐
                              │          │          │
                        Rate Limiter  Circuit   Webhook
                        (100/s/ch)    Breaker   Delivery
```

### Request Flow

1. Client sends notification via REST API
2. Notification is persisted in PostgreSQL with status `pending`
3. If not scheduled, it's enqueued to the appropriate Redis Stream (`notifications:{channel}:{priority}`) and status becomes `queued`
4. Worker pool dequeues messages, applies rate limiting, delivers via webhook
5. On success: status becomes `delivered`. On failure: exponential backoff retry is scheduled
6. Every status change is broadcast via Redis Pub/Sub to WebSocket clients

### Key Components

| Component | Responsibility |
|-----------|---------------|
| **9 Priority Queues** | 3 channels (sms, email, push) x 3 priorities (high, normal, low) via Redis Streams |
| **Rate Limiter** | Token bucket per channel (100 msg/sec), implemented as Redis Lua script for atomicity |
| **Circuit Breaker** | Protects against cascading failures to webhook provider (closed/open/half-open states) |
| **Scheduler** | Polls for scheduled notifications and retry-ready failures using `FOR UPDATE SKIP LOCKED` |
| **Worker Pool** | Configurable concurrency per channel, graceful shutdown with in-flight draining |

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.22 |
| Router | Gin |
| Database | PostgreSQL 16 |
| Queue / Cache | Redis 7 (Streams, Pub/Sub, Lua scripting) |
| Migrations | golang-migrate |
| Metrics | Prometheus |
| Tracing | OpenTelemetry + Jaeger |
| WebSocket | gorilla/websocket |
| Docs | Swagger (swaggo) |
| CI/CD | GitHub Actions |

## Getting Started

### Prerequisites

- Docker and Docker Compose
- Go 1.22+ (for local development)

### Quick Start

1. **Clone the repository**
   ```bash
   git clone https://github.com/ErayOzdayioglu/insider-one-notification-system.git
   cd insider-one-notification-system
   ```

2. **Configure webhook endpoint**

   Go to [webhook.site](https://webhook.site) and copy your unique UUID, then create a `.env` file:
   ```bash
   cp .env.example .env
   # Edit .env and set WEBHOOK_UUID=your-uuid-here
   ```

3. **Start all services**
   ```bash
   docker-compose up --build
   ```

   This starts: API (`:8080`), PostgreSQL (`:5432`), Redis (`:6379`), Jaeger UI (`:16686`), Prometheus (`:9090`)

4. **Verify**
   ```bash
   curl http://localhost:8080/health
   # {"status":"ok"}
   ```

### Local Development

```bash
# Run dependencies only
docker-compose up postgres redis jaeger prometheus -d

# Run API locally
export POSTGRES_HOST=localhost REDIS_ADDR=localhost:6379 WEBHOOK_UUID=your-uuid
go run cmd/api/main.go

# Run tests
make test

# Lint
make lint
```

### Makefile Targets

| Command | Description |
|---------|-------------|
| `make build` | Build binary |
| `make test` | Run tests with race detector |
| `make lint` | Run golangci-lint |
| `make docker-up` | Start all services |
| `make docker-down` | Stop and clean up |
| `make migrate-up` | Run migrations |
| `make migrate-down` | Rollback migrations |
| `make swagger` | Regenerate Swagger docs |

## API Reference

Base URL: `http://localhost:8080/api/v1`

Interactive Swagger UI: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)

### Notifications

#### Create Notification

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "welcome-user-123",
    "channel": "email",
    "priority": "high",
    "recipient": "user@example.com",
    "subject": "Welcome!",
    "content": "Hello, welcome to our platform!"
  }'
```

Response `201`:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "queued",
  "created_at": "2026-03-17T12:00:00Z"
}
```

#### Create Batch (up to 1000)

```bash
curl -X POST http://localhost:8080/api/v1/notifications/batch \
  -H "Content-Type: application/json" \
  -d '{
    "notifications": [
      {
        "idempotency_key": "promo-user-1",
        "channel": "sms",
        "priority": "normal",
        "recipient": "+905551234567",
        "content": "Flash sale! 50% off today."
      },
      {
        "idempotency_key": "promo-user-2",
        "channel": "push",
        "priority": "low",
        "recipient": "device-token-abc",
        "content": "Check out our new arrivals!"
      }
    ]
  }'
```

Response `201`:
```json
{
  "batch_id": "660e8400-e29b-41d4-a716-446655440000",
  "notifications": [
    { "id": "...", "status": "queued" },
    { "id": "...", "status": "queued" }
  ],
  "total": 2
}
```

#### Scheduled Notification

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "reminder-user-456",
    "channel": "email",
    "priority": "normal",
    "recipient": "user@example.com",
    "subject": "Reminder",
    "content": "Your appointment is tomorrow.",
    "scheduled_at": "2026-03-18T09:00:00Z"
  }'
```

Status will be `pending` until the scheduled time, then automatically queued by the scheduler.

#### Using Templates

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "idempotency_key": "welcome-user-789",
    "channel": "email",
    "priority": "high",
    "recipient": "user@example.com",
    "content": "Hello {{.Name}}, welcome to {{.Company}}!",
    "template_vars": {
      "Name": "John",
      "Company": "Insider"
    }
  }'
```

#### Get Notification

```bash
curl http://localhost:8080/api/v1/notifications/{id}
```

#### List Notifications with Filters

```bash
curl "http://localhost:8080/api/v1/notifications?channel=email&status=delivered&limit=10&offset=0&sort_by=created_at&sort_order=desc"
```

#### Get Batch

```bash
curl "http://localhost:8080/api/v1/notifications/batch/{batchId}?limit=20&offset=0"
```

#### Cancel Notification

Only `pending` or `queued` notifications can be cancelled.

```bash
curl -X PATCH http://localhost:8080/api/v1/notifications/{id}/cancel
```

### Templates

#### Create Template

```bash
curl -X POST http://localhost:8080/api/v1/templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "welcome_email",
    "channel": "email",
    "subject": "Welcome, {{.Name}}!",
    "content": "Hello {{.Name}}, welcome to {{.Company}}! Your account is ready."
  }'
```

Variables are automatically extracted from `{{.Variable}}` placeholders in the content and subject.

#### Render Template (Preview)

```bash
curl -X POST http://localhost:8080/api/v1/templates/{id}/render \
  -H "Content-Type: application/json" \
  -d '{
    "variables": {
      "Name": "John",
      "Company": "Insider"
    }
  }'
```

Response `200`:
```json
{
  "subject": "Welcome, John!",
  "content": "Hello John, welcome to Insider! Your account is ready."
}
```

#### Other Template Endpoints

```bash
GET    /api/v1/templates          # List templates
GET    /api/v1/templates/{id}     # Get by ID
PUT    /api/v1/templates/{id}     # Update
DELETE /api/v1/templates/{id}     # Delete
```

### Health & Monitoring

```bash
GET /health          # Liveness probe (always 200)
GET /ready           # Readiness probe (checks DB + Redis)
GET /metrics         # Prometheus metrics
GET /swagger/*       # Swagger UI
```

## WebSocket

Connect to receive real-time notification status updates:

```javascript
const ws = new WebSocket("ws://localhost:8080/api/v1/ws");

ws.onmessage = (event) => {
  const update = JSON.parse(event.data);
  console.log(update);
  // {
  //   "notification_id": "550e8400-...",
  //   "status": "delivered",
  //   "provider_message_id": "msg-123",
  //   "timestamp": "2026-03-17T12:00:05Z"
  // }
};
```

Status transitions broadcast: `pending` -> `queued` -> `processing` -> `delivered`/`failed`/`cancelled`

## Design Decisions

### Idempotency

Every notification requires a unique `idempotency_key`. Duplicate keys return a `409 Conflict`, preventing double-sends even under retry storms or network issues.

### Priority Queues via Redis Streams

9 separate streams (`notifications:{channel}:{priority}`) ensure high-priority messages aren't blocked behind bulk low-priority sends. Consumer groups enable horizontal scaling of workers.

### Rate Limiting with Lua

Token bucket implemented as a Redis Lua script guarantees atomic check-and-decrement across multiple API instances. 100 messages/second per channel, configurable via environment variables.

### Circuit Breaker

Wraps the webhook delivery client with three states:
- **Closed**: normal operation, counts consecutive failures
- **Open**: rejects all requests immediately after 5 consecutive failures, waits 30s
- **Half-Open**: allows one probe request, closes on 3 consecutive successes

### Retry with Exponential Backoff

Failed deliveries are retried up to 5 times with exponential backoff (`baseDelay * 2^attempts`) plus random jitter (+-20%) to prevent thundering herd. The scheduler uses `FOR UPDATE SKIP LOCKED` for safe horizontal scaling.

### Async Processing

The API returns immediately after persisting the notification. All delivery happens asynchronously via worker pools, keeping API latency low even under burst traffic.

## Testing

```bash
# Run all tests
make test

# Run specific test
go test ./internal/service/... -run TestNotificationService -v

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

Test coverage includes:
- **Entity validation** (channels, recipients, priorities, retry logic)
- **Domain errors** (error wrapping, type matching)
- **Template engine** (rendering, variable validation)
- **Circuit breaker** (state transitions, thresholds)
- **Service layer** (create, batch, cancel with mocked dependencies)
- **HTTP handlers** (request/response mapping, error codes)
- **Worker processor** (delivery flow, retry scheduling)

## Observability

### Prometheus Metrics (`:9090`)

| Metric | Type | Description |
|--------|------|-------------|
| `notifications_created_total` | Counter | Created notifications by channel and priority |
| `notifications_processed_total` | Counter | Processed notifications by channel and status |
| `notification_latency_seconds` | Histogram | End-to-end latency (creation to delivery) |
| `delivery_duration_seconds` | Histogram | Webhook HTTP call duration |
| `queue_depth` | Gauge | Current queue depth by channel and priority |
| `active_workers` | Gauge | Active workers by channel |
| `rate_limit_hits_total` | Counter | Rate limiter rejections by channel |
| `circuit_breaker_state` | Gauge | Circuit breaker state (0=closed, 1=open, 2=half-open) |
| `http_request_duration_seconds` | Histogram | API request duration by method, path, status |
| `websocket_connections` | Gauge | Active WebSocket connections |

### Distributed Tracing - Jaeger UI (`:16686`)

Every request gets a trace with spans for: HTTP handling, database queries, queue operations, and webhook delivery. Correlation IDs are propagated via the `X-Correlation-ID` header and attached to trace spans.

## Project Structure

```
.
├── cmd/api/main.go                  # Composition root, graceful shutdown
├── internal/
│   ├── config/                      # Environment-based configuration
│   ├── domain/
│   │   ├── entity/                  # Notification, Template entities
│   │   ├── errors/                  # Domain error types
│   │   ├── queue/                   # Queue interfaces (Producer, Consumer)
│   │   ├── repository/             # Repository interfaces
│   │   └── service/                # Service interfaces
│   ├── infrastructure/
│   │   ├── postgres/               # Repository implementations (pgxpool)
│   │   ├── redis/                  # Queue, rate limiter, pub/sub
│   │   └── httpclient/            # Webhook client, circuit breaker
│   ├── service/                    # Service implementations
│   ├── template/                   # Template engine (text/template)
│   ├── worker/                     # Worker pool, processor
│   ├── scheduler/                  # Scheduled sends, retry polling
│   ├── delivery/
│   │   ├── handler/               # REST handlers, router, DTOs
│   │   ├── middleware/            # Correlation ID, logging, recovery
│   │   └── websocket/            # Hub, client, real-time updates
│   ├── metrics/                   # Prometheus collectors, middleware
│   └── tracing/                   # OpenTelemetry setup, middleware
├── migrations/                     # Versioned SQL migrations
├── docs/                          # Generated Swagger/OpenAPI spec
├── .github/workflows/ci.yml      # CI/CD pipeline
├── docker-compose.yml             # Full stack (API, PG, Redis, Jaeger, Prometheus)
├── Dockerfile                     # Multi-stage build
└── Makefile                       # Build, test, lint, deploy targets
```
