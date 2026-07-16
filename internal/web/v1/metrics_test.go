package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/duynhlab/cart-service/internal/core/domain"
	logicv1 "github.com/duynhlab/cart-service/internal/logic/v1"
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

// addToCart drives the handler with body against a repo whose AddItem returns
// addErr, and returns the handler. user_id is always set (authorized).
func addToCart(t *testing.T, body []byte, addErr error) {
	t.Helper()
	mockRepo := new(MockCartRepository)
	mockRepo.On("AddItem", mock.Anything, "1", mock.Anything).Return(addErr).Maybe()
	handler := NewCartHandler(logicv1.NewCartService(mockRepo))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/cart", bytes.NewBuffer(body))
	c.Set("user_id", "1")
	handler.AddToCart(c)
}

// TestAddToCart_ItemsAddedMetric proves the add-attempt boundary records exactly
// once per outcome: accepted → added; an invalid-quantity binding rejection →
// rejected_invalid_qty; and neither a non-quantity validation failure, malformed
// body, nor a persistence failure is counted as a business add outcome.
func TestAddToCart_ItemsAddedMetric(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := testReader()
	const name = "cart.items_added.total"

	valid := domain.AddToCartRequest{ProductID: "p1", ProductName: "P", ProductPrice: 10, Quantity: 1}
	validBody, _ := json.Marshal(valid)
	badQty := valid
	badQty.Quantity = 0
	badQtyBody, _ := json.Marshal(badQty)
	noProduct := valid
	noProduct.ProductID = ""
	noProductBody, _ := json.Marshal(noProduct)

	cases := []struct {
		name         string
		body         []byte
		addErr       error
		wantAdded    int64
		wantRejected int64
	}{
		{"accepted", validBody, nil, 1, 0},
		{"invalid quantity", badQtyBody, nil, 0, 1},
		{"non-quantity validation failure", noProductBody, nil, 0, 0},
		{"malformed body", []byte("not json"), nil, 0, 0},
		{"persistence failure", validBody, errors.New("db down"), 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := collectCounter(t, reader, name, "result")
			addToCart(t, tc.body, tc.addErr)
			after := collectCounter(t, reader, name, "result")

			if d := after[logicv1.ItemsAddedResultAdded] - before[logicv1.ItemsAddedResultAdded]; d != tc.wantAdded {
				t.Errorf("items_added{result=added} delta = %d, want %d", d, tc.wantAdded)
			}
			if d := after[logicv1.ItemsAddedResultRejected] - before[logicv1.ItemsAddedResultRejected]; d != tc.wantRejected {
				t.Errorf("items_added{result=rejected_invalid_qty} delta = %d, want %d", d, tc.wantRejected)
			}
		})
	}
}

// TestClearCart_ClearedMetric proves each clear entry point attributes a
// successful clear to the right source, and that a failed clear records nothing.
func TestClearCart_ClearedMetric(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := testReader()
	const name = "cart.cleared.total"

	t.Run("user REST clear -> source=user_rest", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("Clear", mock.Anything, "1").Return(nil)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		before := collectCounter(t, reader, name, "source")
		_, c := newTestContext("DELETE", "/cart", nil)
		c.Set("user_id", "1")
		handler.ClearCart(c)
		after := collectCounter(t, reader, name, "source")

		if d := after[logicv1.ClearSourceUserREST] - before[logicv1.ClearSourceUserREST]; d != 1 {
			t.Errorf("cleared{source=user_rest} delta = %d, want 1", d)
		}
		if d := after[logicv1.ClearSourceInternalSaga] - before[logicv1.ClearSourceInternalSaga]; d != 0 {
			t.Errorf("internal_saga delta = %d on user clear, want 0", d)
		}
	})

	t.Run("internal saga clear -> source=internal_saga", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("Clear", mock.Anything, "1").Return(nil)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		before := collectCounter(t, reader, name, "source")
		_, c := newTestContext("DELETE", "/cart/v1/internal/cart/1", nil)
		c.Params = gin.Params{{Key: "userId", Value: "1"}}
		handler.ClearCartByUserID(c)
		after := collectCounter(t, reader, name, "source")

		if d := after[logicv1.ClearSourceInternalSaga] - before[logicv1.ClearSourceInternalSaga]; d != 1 {
			t.Errorf("cleared{source=internal_saga} delta = %d, want 1", d)
		}
		if d := after[logicv1.ClearSourceUserREST] - before[logicv1.ClearSourceUserREST]; d != 0 {
			t.Errorf("user_rest delta = %d on saga clear, want 0", d)
		}
	})

	t.Run("failed clear records nothing", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("Clear", mock.Anything, "1").Return(errors.New("db down"))
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		before := collectCounter(t, reader, name, "source")
		_, c := newTestContext("DELETE", "/cart", nil)
		c.Set("user_id", "1")
		handler.ClearCart(c)
		after := collectCounter(t, reader, name, "source")

		if d := after[logicv1.ClearSourceUserREST] - before[logicv1.ClearSourceUserREST]; d != 0 {
			t.Errorf("cleared delta = %d on failed clear, want 0 (a clear that failed did not happen)", d)
		}
	})
}
