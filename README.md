# cart-service

The per-user shopping cart: which products are selected, in what quantity, and
the price snapshot taken when they were added.

## Responsibilities

- **Owns:** cart items for a user — product, quantity, and the denormalised
  add-time price — plus the cart totals derived from them.
- **Does not own:** the product catalog or current prices (`product-service`),
  stock and availability (`inventory-service`), or the order the cart becomes
  (`order-service`, `checkout-service`).

## Tech

| Area | Technology |
|------|------------|
| Runtime | Go 1.26 |
| Transports | HTTP (private customer routes, one internal route) · gRPC (east-west read) |
| Data | PostgreSQL — one table, `cart_items` |
| Platform libraries | `authmw`, `dbx`, `grpcx`, `httpx`, `logger/zapx`, `migratex`, `obsx`, `proto` |

## API

- **Canonical contract:** [`homelab/docs/api/cart.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/cart.md)
- **Shared conventions:** [`homelab/docs/api/api.md`](https://github.com/duynhlab/homelab/blob/main/docs/api/api.md)
- **Surfaces:** JWT-protected HTTP for the customer's own cart, one tokenless
  internal route used by the order workflow to clear a cart, and
  `cart.v1.CartService` east-west so checkout can read a cart snapshot. HTTP
  `:8080` also carries `/health` and `/ready`.

Routes, payloads and error codes live in the contract, so there is one place to
change when they change.

## Run locally

Prefer the homelab **local-stack** — the customer routes need a signed token and
a catalog to add from.

Standalone you need PostgreSQL reachable through the `DB_*` variables:

```bash
go run cmd/main.go migrate   # apply schema migrations
go run cmd/main.go seed      # demo cart rows — development only, refuses production
go run cmd/main.go           # serve HTTP :8080 + gRPC :9090
```

## Verify

The commands CI runs, so a green local run means a green pipeline:

```bash
go build ./...
go test -race ./...
go test -tags=integration ./internal/core/repository/...   # needs Docker (testcontainers)
golangci-lint run
```

## Docs

- [Canonical contract](https://github.com/duynhlab/homelab/blob/main/docs/api/cart.md)
- [local-stack guide](https://github.com/duynhlab/homelab/blob/main/local-stack/README.md)

## License

MIT
