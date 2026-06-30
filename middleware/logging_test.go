package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSplitTraceParent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"w3c traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			[]string{"00", "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7", "01"}},
		{"empty", "", nil},
		{"single", "abc", []string{"abc"}},
		{"trailing hyphen", "a-b-", []string{"a", "b"}},
		{"leading hyphen", "-a-b", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTraceParent(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitTraceParent(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("part[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGenerateTraceID(t *testing.T) {
	id := generateTraceID()
	if len(id) != 32 {
		t.Fatalf("generateTraceID() length = %d, want 32", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("generateTraceID() = %q contains non-hex char %q", id, c)
		}
	}
	if generateTraceID() == id {
		t.Error("generateTraceID() returned identical IDs on consecutive calls")
	}
}

func TestGetTraceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		headers map[string]string
		want    string // "" means "expect a generated 32-char id"
	}{
		{"from traceparent", map[string]string{TraceParentHeader: "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"from x-trace-id", map[string]string{TraceIDHeader: "my-trace-123"}, "my-trace-123"},
		{"traceparent malformed falls back to x-trace-id", map[string]string{TraceParentHeader: "garbage", TraceIDHeader: "fallback-id"}, "fallback-id"},
		{"none -> generated", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			c.Request = req

			got := GetTraceID(c)
			if tt.want == "" {
				if len(got) != 32 {
					t.Fatalf("GetTraceID() = %q, want a generated 32-char id", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("GetTraceID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("sets trace id header + context", func(t *testing.T) {
		r := gin.New()
		r.Use(LoggingMiddleware())
		r.GET("/ok", func(c *gin.Context) {
			if _, ok := c.Get("trace_id"); !ok {
				t.Error("trace_id not set in context")
			}
			if _, ok := c.Get("logger"); !ok {
				t.Error("logger not set in context")
			}
			c.String(http.StatusOK, "ok")
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ok", nil)
		req.Header.Set(TraceIDHeader, "trace-from-client")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get(TraceIDHeader); got != "trace-from-client" {
			t.Errorf("response %s = %q, want trace-from-client", TraceIDHeader, got)
		}
	})

	t.Run("logs the error branch on 5xx", func(t *testing.T) {
		r := gin.New()
		r.Use(LoggingMiddleware())
		r.GET("/boom", func(c *gin.Context) { c.String(http.StatusInternalServerError, "boom") })

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}
	})
}

func TestGetLoggerFromGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if GetLoggerFromGinContext(c) == nil {
		t.Error("GetLoggerFromGinContext returned nil, want a non-nil slog.Logger")
	}
}
