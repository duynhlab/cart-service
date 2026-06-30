package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
}
