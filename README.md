# cart-service

Shopping cart microservice for managing user carts and items.

## Features

- Add/remove items
- Update quantities
- Cart totals calculation
- Cart count for badges

## API Endpoints

All routes follow Variant A naming and require JWT (audience = `private`). The JWT is validated by calling the auth service over gRPC (see below). See [homelab naming convention](https://github.com/duynhlab/homelab/blob/main/docs/api/api-naming-convention.md).

| Method | Path |
|--------|------|
| `GET` | `/cart/v1/private/cart` |
| `POST` | `/cart/v1/private/cart` |
| `DELETE` | `/cart/v1/private/cart` |
| `GET` | `/cart/v1/private/cart/count` |
| `PATCH` | `/cart/v1/private/cart/items/:itemId` |
| `DELETE` | `/cart/v1/private/cart/items/:itemId` |

Infrastructure endpoints (not subject to JWT, excluded from RED metrics): `GET /health`, `GET /ready`, `GET /metrics`.

## Authentication (gRPC client)

cart-service is a gRPC **client**, not a server. Every `/cart/v1/private/*`
route is wrapped by the shared `github.com/duynhlab/pkg/authmw` middleware,
which validates the bearer token by calling `auth.v1.AuthService/GetMe` over
gRPC (target from `AUTH_GRPC_ADDR`, dialed via `pkg/grpcx`). The middleware is
**fail-closed**: missing token → 401, auth rejects → 401, auth unreachable →
503. On success it sets `user_id`/`username`/`email` in the Gin context. gRPC
is the platform's east-west transport; no JWT parsing happens in this service.

## Tech Stack

- Go 1.26 + Gin framework
- PostgreSQL via `pgx/v5` (`pgxpool`, simple-protocol mode for transaction poolers)
- gRPC client to auth-service (`pkg/grpcx`, `pkg/authmw`)
- OpenTelemetry tracing + OTel→Prometheus metrics (`pkg/obsx`)
- Pyroscope continuous profiling

## Development

### Prerequisites

- Go 1.26+
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2+

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

### Pre-push Checklist

```bash
go build ./... && go test ./... && golangci-lint run --timeout=10m
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
  the Prometheus default registry. This bridges gRPC RED metrics
  (`rpc_client_*`, emitted by the `pkg/grpcx` otel stats handlers) onto the
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
| `AUTH_GRPC_ADDR` | `dns:///auth.auth.svc.cluster.local:9090` | auth gRPC target for token validation |
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
