package v1

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	cartv1 "github.com/duynhlab/pkg/proto/cart/v1"

	"github.com/duynhlab/cart-service/internal/core/domain"
)

// One MeterProvider per test binary (first-wins), delta-based assertions.
var (
	meterOnce   sync.Once
	meterReader *sdkmetric.ManualReader
)

func testReader() *sdkmetric.ManualReader {
	meterOnce.Do(func() {
		meterReader = sdkmetric.NewManualReader()
		otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(meterReader)))
	})
	return meterReader
}

func collectCounter(t *testing.T, reader sdkmetric.Reader, name, label string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				v, _ := dp.Attributes.Value(attribute.Key(label))
				out[v.AsString()] = dp.Value
			}
		}
	}
	return out
}

// TestGetCart_SnapshotMetric drives every GetCart terminal path and asserts the
// snapshot_requests counter increments exactly once under the matching result
// label, proving the branch→label mapping the checkout read depends on.
func TestGetCart_SnapshotMetric(t *testing.T) {
	reader := testReader()
	const name = "cart.snapshot_requests.total"
	ctx := context.Background()

	cases := []struct {
		label  string
		server *Server
		req    *cartv1.GetCartRequest
		wantOK bool
	}{
		{
			label:  "invalid_arg",
			server: NewServer(&fakeCartReader{}),
			req:    &cartv1.GetCartRequest{},
		},
		{
			label:  "error",
			server: NewServer(&fakeCartReader{err: errors.New("boom")}),
			req:    &cartv1.GetCartRequest{UserId: "7"},
		},
		{
			label:  "empty",
			server: NewServer(&fakeCartReader{cart: &domain.Cart{UserID: "7"}}),
			req:    &cartv1.GetCartRequest{UserId: "7"},
			wantOK: true,
		},
		{
			label: "ok",
			server: NewServer(&fakeCartReader{cart: &domain.Cart{
				UserID: "7",
				Items:  []domain.CartItem{{ProductID: "1", Quantity: 1}},
			}}),
			req:    &cartv1.GetCartRequest{UserId: "7"},
			wantOK: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			before := collectCounter(t, reader, name, "result")
			_, err := tc.server.GetCart(ctx, tc.req)
			if tc.wantOK && err != nil {
				t.Fatalf("GetCart() error = %v, want nil", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("GetCart() error = nil, want non-nil")
			}
			after := collectCounter(t, reader, name, "result")
			if d := after[tc.label] - before[tc.label]; d != 1 {
				t.Errorf("snapshot_requests{result=%s} delta = %d, want 1", tc.label, d)
			}
		})
	}
}
