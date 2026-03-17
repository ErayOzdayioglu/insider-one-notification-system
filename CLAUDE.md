# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Event-driven notification system for Insider One that processes and delivers messages through SMS, Email, and Push channels. Built with Go 1.2x. Must handle high throughput (millions daily), burst traffic, intelligent retries, and real-time status tracking.

## Build & Run Commands

```bash
# Full stack
docker-compose up

# Run all tests
go test ./...

# Run single test
go test ./path/to/package -run TestName

# Lint
golangci-lint run

# Database migrations
# (tool TBD — likely golang-migrate or goose)

# Generate Swagger docs
swag init -g cmd/api/main.go
```

## Architecture

**API Layer** — RESTful notification management API:
- CRUD for notifications (single + batch up to 1000)
- Query by ID/batch ID, cancel pending, list with filters & pagination
- Health check and metrics endpoints

**Processing Engine** — Async queue workers:
- Rate limiting: max 100 msg/sec per channel (SMS, Email, Push)
- Priority queues: high, normal, low
- Content validation and idempotency (prevent duplicate sends)

**Delivery & Retry** — External provider integration via webhook.site:
- POST to `https://webhook.site/{uuid}` with `{to, channel, content}`
- Expects 202 with `{messageId, status, timestamp}`
- Retry logic with backoff for failed deliveries

**Observability** — Structured logging with correlation IDs, real-time metrics (queue depth, success/failure rates, latency)

## Engineering Standards

Implement all features — including bonus features — at a senior/staff-level quality bar. This is a showcase of distributed systems and high-traffic engineering expertise.

- **Clean Code**: meaningful names, small focused functions, no dead code, no magic numbers. Code should read like well-written prose.
- **SOLID Principles**: single responsibility per struct/package, depend on interfaces not concretions, keep interfaces small and cohesive. Use dependency injection throughout — no hidden global state.
- **Distributed Systems Rigor**: design every component assuming horizontal scaling. Use proper concurrency patterns (context propagation, graceful shutdown, worker pools). Handle partial failures, timeouts, and back-pressure explicitly.
- **High-Traffic Patterns**: connection pooling, efficient serialization, bounded queues, circuit breakers around external calls. Benchmark hot paths.
- **Testing**: unit tests for business logic, integration tests for infrastructure boundaries (DB, queue, HTTP), end-to-end tests for critical flows. Use table-driven tests. Target high coverage on core domain.
- **Error Handling**: wrap errors with context (`fmt.Errorf("...: %w", err)`), never swallow errors silently, use structured error types where appropriate.

## Key Design Constraints

- Idempotency keys must prevent duplicate notification sends
- Rate limiter is per-channel, not global
- Batch endpoint accepts up to 1000 notifications per request
- All notification processing is asynchronous — API returns immediately, workers process from queue
- Provide Swagger/OpenAPI documentation
- Database schema changes must use versioned migrations

## Bonus Features (Implement All)

Scheduled notifications, message templates with variable substitution, WebSocket real-time status updates, distributed tracing, GitHub Actions CI/CD. These are not optional — implement all of them.
