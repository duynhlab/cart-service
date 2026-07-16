package v1

import (
	"context"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The OTel global MeterProvider is first-wins: the package-init instruments bind
// to the first delegate installed, so a test binary installs exactly one
// provider (guarded by sync.Once) and every assertion reads the same cumulative
// ManualReader via deltas around the action under test.
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

// collectCounter reads name into an attribute→value map keyed by one label.
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

// TestRecorders pins the bounded label domain of every exported recorder: each
// label value lands under its expected key with a +1 delta. Which call site
// selects which label is proven by the web-handler and gRPC-server tests.
func TestRecorders(t *testing.T) {
	reader := testReader()
	ctx := context.Background()

	ba := collectCounter(t, reader, "cart.items_added.total", "result")
	added := []string{ItemsAddedResultAdded, ItemsAddedResultRejected}
	for _, r := range added {
		RecordItemAdded(ctx, r)
	}
	aa := collectCounter(t, reader, "cart.items_added.total", "result")
	for _, r := range added {
		if d := aa[r] - ba[r]; d != 1 {
			t.Errorf("items_added{result=%s} delta = %d, want 1", r, d)
		}
	}

	before := collectCounter(t, reader, "cart.cleared.total", "source")
	RecordCartCleared(ctx, ClearSourceUserREST)
	RecordCartCleared(ctx, ClearSourceInternalSaga)
	after := collectCounter(t, reader, "cart.cleared.total", "source")
	for _, src := range []string{ClearSourceUserREST, ClearSourceInternalSaga} {
		if d := after[src] - before[src]; d != 1 {
			t.Errorf("cleared{source=%s} delta = %d, want 1", src, d)
		}
	}

	bs := collectCounter(t, reader, "cart.snapshot_requests.total", "result")
	results := []string{SnapshotResultOK, SnapshotResultEmpty, SnapshotResultInvalidArg, SnapshotResultError}
	for _, r := range results {
		RecordSnapshotRequest(ctx, r)
	}
	as := collectCounter(t, reader, "cart.snapshot_requests.total", "result")
	for _, r := range results {
		if d := as[r] - bs[r]; d != 1 {
			t.Errorf("snapshot_requests{result=%s} delta = %d, want 1", r, d)
		}
	}
}
