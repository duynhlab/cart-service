package v1

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Business metrics for cart (RFC-0017 W2), answering the questions that matter
// for the shopping funnel and the RFC-0015 checkout read path:
//  1. Are adds being rejected by the quantity rule?  → items_added{result}
//  2. Who is clearing carts — the user or the saga?   → cleared{source}
//  3. Is the checkout cart snapshot healthy?          → snapshot_requests{result}
//
// Instruments ride the global OTel MeterProvider that obsx installs (RFC-0014
// OTLP pipeline → collector → VictoriaMetrics). Before that setup the global
// provider is a no-op, so package-init here is safe. Names are OTel-style; the
// collector renders them as cart_items_added_total, cart_cleared_total, and
// cart_snapshot_requests_total.
//
// Labels are bounded to enumerable domain values (RFC-0017 D-9): no ids, no
// free-form text, no quantities.
var (
	meter = otel.Meter("cart-service")

	itemsAddedCounter, _ = meter.Int64Counter("cart.items_added.total",
		metric.WithDescription("Add-to-cart attempts by outcome (quantity-rejection KPI)"))
	cartClearedCounter, _ = meter.Int64Counter("cart.cleared.total",
		metric.WithDescription("Cart clears by originating surface (user REST vs internal saga)"))
	snapshotRequestsCounter, _ = meter.Int64Counter("cart.snapshot_requests.total",
		metric.WithDescription("gRPC GetCart snapshot reads (RFC-0015 checkout) by outcome"))
)

// Add-to-cart outcomes (bounded). The persistence-failure path is deliberately
// not counted — it surfaces via the DB span and pool error signals — so this
// counter reads purely as a business add-vs-reject rate.
const (
	ItemsAddedResultAdded    = "added"
	ItemsAddedResultRejected = "rejected_invalid_qty"
)

// Cart-clear sources (bounded). A clear originates either from the browser REST
// path (JWT user) or from the order-fulfillment saga's tokenless internal route.
const (
	ClearSourceUserREST     = "user_rest"
	ClearSourceInternalSaga = "internal_saga"
)

// Snapshot-read outcomes (bounded) for the gRPC GetCart checkout read.
const (
	SnapshotResultOK         = "ok"
	SnapshotResultEmpty      = "empty"
	SnapshotResultInvalidArg = "invalid_arg"
	SnapshotResultError      = "error"
)

// RecordItemAdded counts one add-to-cart attempt by outcome. The quantity rule is
// enforced by request binding at the web layer (min=1), so the rejection is
// observed there rather than in logic; the web handler records exactly once per
// attempt — accepted, or rejected for invalid quantity.
func RecordItemAdded(ctx context.Context, result string) {
	itemsAddedCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

// RecordCartCleared counts one successful cart clear by originating surface. The
// source is a transport concern the web layer resolves, so it is passed in.
func RecordCartCleared(ctx context.Context, source string) {
	cartClearedCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("source", source)))
}

// RecordSnapshotRequest counts one gRPC GetCart snapshot read by outcome. Called
// exactly once per request on whichever terminal path the read takes.
func RecordSnapshotRequest(ctx context.Context, result string) {
	snapshotRequestsCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}
