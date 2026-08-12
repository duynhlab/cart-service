# AGENTS.md

Agent-focused guide for `cart-service`. Keep changes minimal, verified against
the code, and consistent with existing patterns.

## Authority and scope

This repository implements the service. It does **not** define the contract.

- **Canonical contract:** [`homelab/docs/api/cart.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/cart.md)
- **Shared API rules:** [`homelab/docs/api/api.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/api.md)

Implement against those files. When this repository and the contract disagree,
**stop and classify the mismatch** using
[Resolving a mismatch](https://github.com/duynhlab/homelab/blob/main/docs/api/README.md#resolving-a-mismatch)
before changing either side. One class — an implementation that violates the
intended contract — **blocks the release tag**.

**Known open mismatch:** the contract is internally inconsistent about the legacy
order→cart pricing hop. Its Known gaps section says the hop was removed; its
Technical debt row, its diagram and its callers table still list it. The removal
is the current truth. Treated as a canonical-doc gap to fix in homelab, not a
reason to keep documenting a caller that no longer exists.

No route, RPC, payload or error inventory belongs in this file. Manifests,
gateway routing, NetworkPolicy, database topology and platform observability
belong to [duynhlab/homelab](https://github.com/duynhlab/homelab).

## Contribution workflow

- Never commit or push to `main`. Branch first, then open a PR.
- Branch names use conventional prefixes: `feat/`, `fix/`, `docs/`, `chore/`,
  `refactor/`, `test/`.
- Commit subjects: imperative mood, capitalised, ≤ 50 characters, no trailing
  period. Add a body wrapped at 72 characters when the change is non-trivial.
- Do not add attribution trailers (`Signed-off-by`, `Co-authored-by`,
  `Generated-by`, etc.), GitHub issue references, or `@`-mentions in commit
  messages. Put issue links in the PR description.
- One logical change per PR. PRs are squash-merged and CI must be green.

## Build, test, lint

These are the commands CI runs, so a green local run means a green pipeline.

```bash
go build ./...
go vet ./...
go test -race ./...
go test -tags=integration ./internal/core/repository/...   # needs Docker (testcontainers)
golangci-lint run
```

Sonar new-code coverage must be ≥80%; `**/cmd/**`, `**/db/migrations/**` and
`**/core/repository/**` are excluded, everything else counts.

Local development against an unreleased `pkg`: `pkg` is one module per package,
so its root has no `go.mod` and a single `replace github.com/duynhlab/pkg` can no
longer resolve. Use one commented `replace` line per module — the trailer in
`go.mod` shows the shape, and
[`docs/api/pkg.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/pkg.md)
explains why.

## Architecture boundaries

**3-layer, dependencies flow one way only: transport → logic → core.**

- **Transport** — `internal/web/v1/` (HTTP) and `internal/grpc/v1/` (gRPC).
  Validate, map, delegate. The web layer must not mint its own span: otelgin
  already opened the server span and owns its lifecycle, so annotate it, never
  end it.
- **Logic** — `internal/logic/v1/` holds the rules. No SQL, no gin types.
- **Core** — `internal/core/` owns the domain model, the repository interface and
  the Postgres implementation.

Both transports always run. Observability is wired once through
`github.com/duynhlab/pkg/obsx`; the pool comes from `github.com/duynhlab/pkg/dbx`;
the gRPC server is built by `github.com/duynhlab/pkg/grpcx`; responses use the
shared `github.com/duynhlab/pkg/httpx` envelope; JWTs are verified by
`github.com/duynhlab/pkg/authmw`.

## Invariants

Rules an implementer can violate at the keyboard.

- **Money crosses from float dollars to integer minor units exactly once, at the
  gRPC boundary**, rounding half-away-from-zero there and nowhere else. Storage
  and the browser JSON stay float; the wire contract stays integer. A second
  conversion anywhere is a rounding bug waiting for a price ending in `.005`.
- **Quantity is clamped before the wire conversion**, so an int that would
  overflow the 32-bit field cannot. The clamp is why the narrowing conversion is
  safe.
- **An empty cart is a business condition, never an error.** It returns an empty
  list, and the caller decides what emptiness means — checkout answers a conflict
  on an empty snapshot. Turning it into a not-found moves that decision into the
  wrong service.
- **Anti-IDOR, door one: the user id comes from the verified JWT subject, never
  the request body.** Every private handler reads it from the context and returns
  401 when it is absent.
- **Anti-IDOR, door two: every item-scoped statement is scoped by user id**, and
  zero rows affected is a not-found rather than a success. This is what stops a
  valid token from mutating another user's row by guessing an item id.
- **Add-to-cart is an atomic upsert, and re-adding increments.** The conflict
  branch adds to the existing quantity rather than replacing it, and the statement
  is wrapped so the pooler routes it to the primary — a split read/write would
  hit a read-only transaction error on a replica.
- **Clear has two doors sharing one body.** They differ only in where the user id
  comes from and which source they attribute the clear to. The tokenless internal
  door exists so no bearer token has to travel through workflow input and history;
  it is mounted off the gateway and fenced by NetworkPolicy. The two metric
  sources are an operational signal: a vanished internal share means the saga
  clear is broken.
- **`quantity > 0` is enforced in three independent places** — request binding, the
  logic guard, and a `CHECK` constraint. All three stay.
- **The quantity-rejection metric is recorded at the binding layer**, because a
  binding failure never reaches logic. The sniffer counts only that field's
  failures, so other validation errors are not counted, and persistence failures
  are deliberately uncounted so the counter reads as a pure add-versus-reject
  rate.
- **Pooler-safe database settings live in `pkg/dbx`.** One DSN serves the app and
  migrations so both connect identically. The seed path re-asserts simple protocol
  itself because it runs multi-statement files.
- **Graceful-shutdown ordering is load-bearing:** readiness 503 → drain delay →
  HTTP shutdown → gRPC `GracefulStop` → pool close → OTel shutdown last, so
  pending spans, metrics and logs flush.
- **Probe suppression is one contract across logs and traces**, through the same
  skip list; a **failing** probe is still recorded. 4xx logs at warn, 5xx at error
  — an expected business rejection must not read as an infrastructure error.
- **The logged `trace_id` must be the active span's, or absent.** A synthesised id
  looks joinable while joining to nothing. The generated fallback belongs on the
  response header only.

## Repository map

- `cmd/main.go` — wiring, subcommand dispatch, HTTP + gRPC bootstrap, graceful shutdown
- `config/config.go` — env config and validation
- `internal/web/v1/` — HTTP handlers, including the shared clear body and the quantity-error sniffer
- `internal/grpc/v1/` — the `CartService` read surface and the money/quantity conversions
- `internal/logic/v1/` — business rules, sentinel errors, metrics
- `internal/core/` — pool wiring, domain model, repository interface and Postgres implementation
- `db/migrations/` — forward-only golang-migrate SQL, embedded
- `db/seed/` — development-only demo seed, embedded
- `middleware/` — tracing and logging only

## Gotchas

- Kyverno admission rejects a workload image tagged `:latest` or unpinned. The
  published image is `ghcr.io/duynhlab/cart-service/cart-service:<tag>` — the
  repository path repeats, and the tag carries no `v` prefix. There is no separate
  migration image; the init container reuses the app image with `args: ["migrate"]`.
- Metrics leave over OTLP. There is no `/metrics` endpoint and nothing scrapes
  this service.
- The JWKS default is the `/auth/v1/public/auth/jwks` path. The shorter
  `/auth/v1/public/jwks` is a deprecated alias; do not copy it into config or docs.
- Some code comments still name **PgCat**; the pooler in front of this database is
  **PgDog**. The pooler-safe settings are the same either way, but do not treat
  those comments as current topology — the contract owns that.
- `CHANGELOG.md` is stale — its last entry predates the current route shape and
  still describes a scrape endpoint and the wrong pooler. It is not part of this
  repository's documented contract; treat it as history, not reference.

## API change synchronization

An API change is not done when the code compiles.

- The contract in homelab and this repository move **together** — same change,
  and either the same PR pair or an immediate follow-up.
- Behaviour that is designed but not deployed is marked **`Planned`** in the
  contract; it is never described as current.
- A material mismatch between the contract and this implementation **blocks the
  release tag** until it is reconciled or explicitly accepted.
