package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

func TestShouldCollectMetrics(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/cart/v1/items", true},
		{"/cart/v1/internal/users/7", true},
		{"/health", false},
		{"/healthz", false}, // HasPrefix("/healthz", "/health") matches → skipped
		{"/ready", false},
		{"/readiness", false},
		{"/liveness", false},
		{"/metrics", false},
		{"/", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := shouldCollectMetrics(tt.path); got != tt.want {
				t.Errorf("shouldCollectMetrics(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPrometheusMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PrometheusMiddleware())
	r.GET("/api/work", func(c *gin.Context) { c.String(http.StatusOK, "done") })
	r.GET("/boom", func(c *gin.Context) { c.String(http.StatusInternalServerError, "boom") })
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	cases := []struct {
		name string
		path string
		code int
	}{
		{"collects on a normal route", "/api/work", http.StatusOK},
		{"records the 5xx error branch", "/boom", http.StatusInternalServerError},
		{"skips infrastructure path", "/health", http.StatusOK},
		// No route registered → c.FullPath() == "" → labelled "unknown".
		{"unmatched route uses 'unknown' path label", "/api/does-not-exist", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.code {
				t.Fatalf("GET %s status = %d, want %d", tc.path, w.Code, tc.code)
			}
		})
	}

	t.Run("records exemplar when the request carries a sampled span", func(t *testing.T) {
		// A valid sampled SpanContext injected ahead of the metrics middleware
		// drives the ObserveWithExemplar branch (traceID exemplar on the histogram).
		traced := gin.New()
		traced.Use(func(c *gin.Context) {
			sc := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
				SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
				TraceFlags: trace.FlagsSampled,
			})
			c.Request = c.Request.WithContext(trace.ContextWithSpanContext(c.Request.Context(), sc))
			c.Next()
		})
		traced.Use(PrometheusMiddleware())
		traced.GET("/api/traced", func(c *gin.Context) { c.String(http.StatusOK, "done") })

		w := httptest.NewRecorder()
		traced.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/traced", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		// The status assertion alone can't catch a silently-dropped exemplar:
		// verify the histogram bucket actually carries the injected traceID.
		const wantTraceID = "0102030405060708090a0b0c0d0e0f10"
		families, err := prometheus.DefaultGatherer.Gather()
		if err != nil {
			t.Fatalf("gather: %v", err)
		}
		for _, mf := range families {
			if mf.GetName() != "request_duration_seconds" {
				continue
			}
			for _, m := range mf.GetMetric() {
				for _, lp := range m.GetLabel() {
					if lp.GetName() != "path" || lp.GetValue() != "/api/traced" {
						continue
					}
					for _, b := range m.GetHistogram().GetBucket() {
						ex := b.GetExemplar()
						if ex == nil {
							continue
						}
						for _, el := range ex.GetLabel() {
							if el.GetName() == "traceID" && el.GetValue() == wantTraceID {
								return // exemplar recorded with the injected traceID
							}
						}
					}
				}
			}
		}
		t.Fatalf("no exemplar with traceID=%s found on request_duration_seconds{path=\"/api/traced\"}", wantTraceID)
	})
}
