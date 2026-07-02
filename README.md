# cart-service

Shopping cart microservice for managing user carts and items.

## Features

- Add/remove items
- Update quantities
- Cart totals calculation
- Cart count for badges

## API Endpoints

All routes follow Variant A naming and require JWT (audience = `private`). The JWT is verified locally against auth's JWKS (see below). See [homelab naming convention](https://github.com/duynhlab/homelab/blob/main/docs/api/api-naming-convention.md).

| Method | Path |
|--------|------|
| `GET` | `/cart/v1/private/cart` |
| `POST` | `/cart/v1/private/cart` |
| `DELETE` | `/cart/v1/private/cart` |
| `GET` | `/cart/v1/private/cart/count` |
| `PATCH` | `/cart/v1/private/cart/items/:itemId` |
| `DELETE` | `/cart/v1/private/cart/items/:itemId` |

Infrastructure endpoints (not subject to JWT, excluded from RED metrics): `GET /health`, `GET /ready`, `GET /metrics`.

## Authentication (local JWT verification)

Every `/cart/v1/private/*` route is wrapped by the shared
`github.com/duynhlab/pkg/authmw` middleware, which verifies RS256 JWTs locally
against auth's JWKS (fetched from `AUTH_JWKS_URL`, cached). The middleware is
**fail-closed**: missing, invalid, or expired token → 401. On success it sets
`user_id`/`username`/`email` in the Gin context. JWT is the only credential —
there is no gRPC fallback to auth-service.

## Tech Stack

- Go 1.26 + Gin framework
- PostgreSQL via `pgx/v5` (`pgxpool`, simple-protocol mode for transaction poolers)
- Local JWT verification against auth's JWKS (`pkg/authmw`)
- OpenTelemetry tracing + OTel→Prometheus metrics (`pkg/obsx`)
- Pyroscope continuous profiling

## Development

### Prerequisites

- Go 1.26+
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2+
- Docker (only for the integration tests — see [Testing](#testing))

### Local Development

```bash
# Install dependencies
go mod tidy
go mod download

# Build
go build ./...

# Test
go test ./...

# Lint (must pass before PR merge)
golangci-lint run --timeout=10m

# Run locally (requires .env or env vars)
go run cmd/main.go
```

### Testing

Unit tests use the stdlib `testing` package with hand-written mocks and table-driven
subtests (no testify/gomock). The **repository layer** is covered by **integration tests**
against a real PostgreSQL via [testcontainers](https://golang.testcontainers.org/).

```bash
# Unit tests (no Docker)
go test ./...

# With coverage (as CI runs it)
go test -race -coverprofile=coverage.out ./...

# Integration tests — repository layer, real Postgres (needs a running Docker daemon)
go test -tags=integration ./internal/core/repository/...
```

Integration tests are build-tagged `//go:build integration`, so the default `go test ./...`
skips them and the service binary never links testcontainers. CI runs both jobs and merges
their coverage into SonarCloud (gate: ≥ 80% on new code).

### Pre-push Checklist

```bash
go build ./... && \
  go test ./... && \
  go test -tags=integration ./internal/core/repository/... && \
  golangci-lint run --timeout=10m
```

## Observability

Middleware chain (applied in order in `cmd/main.go`): **tracing → logging → metrics**.

- **Tracing** — OpenTelemetry spans exported to the OTel Collector
  (`OTEL_COLLECTOR_ENDPOINT`). Handlers open a `http.request` span tagged with
  the layer.
- **Logging** — structured `slog` via `pkg/logger/clog`. Each request is logged
  with a `trace_id` taken from the active span (`obsx.TraceIDFromContext`),
  falling back to the `traceparent` / `X-Trace-ID` headers, for log↔trace
  correlation.
- **Metrics** — `obsx.SetupMetrics()` installs an OTel MeterProvider backed by
  the Prometheus default registry, so OTel-emitted metrics land on the
  **existing `/metrics` endpoint** — there is **no separate metrics port**. The
  HTTP middleware adds `request_duration_seconds`, `requests_total`,
  `requests_in_flight`, `request_size_bytes`, `response_size_bytes`, and
  `error_rate_total` to the same registry. Infrastructure paths
  (`/health`, `/ready`, `/metrics`, …) are excluded. The platform
  ServiceMonitor scrapes `/metrics`.
- **Profiling** — Pyroscope continuous profiling (`PYROSCOPE_ENDPOINT`).

## Configuration

All config is loaded from environment variables (with `.env` support for local
dev) in `config/config.go`; `SERVICE_NAME` is required.

| Variable | Default | Purpose |
|----------|---------|---------|
| `SERVICE_NAME` | _(required)_ | Service name (traces/profiles/logs) |
| `PORT` | `8080` | HTTP listen port |
| `ENV` | `development` | `development` / `staging` / `production` |
| `AUTH_JWKS_URL` | `http://auth.auth.svc.cluster.local:8080/auth/v1/public/jwks` | auth JWKS endpoint for local JWT verification |
| `JWT_ISSUER` | `https://gateway.duynh.me` | expected JWT issuer |
| `JWT_AUDIENCE` | `duynhlab-platform` | expected JWT audience |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | — / `5432` / — / — / — | PostgreSQL connection (validated only when `DB_HOST` set) |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `DB_POOL_MAX_CONNECTIONS` | `25` | pgxpool max connections |
| `TRACING_ENABLED` | `true` | Toggle OTel tracing |
| `OTEL_COLLECTOR_ENDPOINT` | `otel-collector-opentelemetry-collector.monitoring.svc.cluster.local:4318` | OTLP/HTTP trace endpoint |
| `OTEL_SAMPLE_RATE` | `0.1` | Trace sampling rate (0.0–1.0) |
| `METRICS_ENABLED` | `true` | Toggle metrics MeterProvider |
| `PROFILING_ENABLED` | `true` | Toggle Pyroscope profiling |
| `PYROSCOPE_ENDPOINT` | `http://pyroscope.monitoring.svc.cluster.local:4040` | Pyroscope endpoint |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `json` | Structured logging |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout |
| `READINESS_DRAIN_DELAY` | `5s` | Delay after failing `/ready` before stopping HTTP (max 30s) |

## License

MIT
