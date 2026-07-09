package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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

// requestLog returns the single "HTTP request" entry recorded by the observer.
func requestLog(t *testing.T, logs *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()
	entries := logs.FilterMessage("HTTP request").All()
	if len(entries) != 1 {
		t.Fatalf("got %d 'HTTP request' log entries, want 1", len(entries))
	}
	return entries[0]
}

func TestLoggingMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("sets trace id header + context", func(t *testing.T) {
		core, logs := observer.New(zapcore.InfoLevel)
		r := gin.New()
		r.Use(LoggingMiddleware(zap.New(core)))
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
		req.Header.Set("User-Agent", "test-agent")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get(TraceIDHeader); got != "trace-from-client" {
			t.Errorf("response %s = %q, want trace-from-client", TraceIDHeader, got)
		}

		entry := requestLog(t, logs)
		if entry.Level != zapcore.InfoLevel {
			t.Errorf("level = %v, want info", entry.Level)
		}
		fields := entry.ContextMap()
		if got := fields["trace_id"]; got != "trace-from-client" {
			t.Errorf("trace_id field = %v, want trace-from-client", got)
		}
		if got := fields["method"]; got != http.MethodGet {
			t.Errorf("method field = %v, want GET", got)
		}
		if got := fields["path"]; got != "/ok" {
			t.Errorf("path field = %v, want /ok", got)
		}
		if got := fields["status"]; got != int64(http.StatusOK) {
			t.Errorf("status field = %v, want 200", got)
		}
		if got := fields["user_agent"]; got != "test-agent" {
			t.Errorf("user_agent field = %v, want test-agent", got)
		}
		for _, key := range []string{"duration", "client_ip"} {
			if _, ok := fields[key]; !ok {
				t.Errorf("missing %q field", key)
			}
		}
	})

	t.Run("logs the error branch on 5xx", func(t *testing.T) {
		core, logs := observer.New(zapcore.InfoLevel)
		r := gin.New()
		r.Use(LoggingMiddleware(zap.New(core)))
		r.GET("/boom", func(c *gin.Context) { c.String(http.StatusInternalServerError, "boom") })

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}

		entry := requestLog(t, logs)
		if entry.Level != zapcore.ErrorLevel {
			t.Errorf("level = %v, want error", entry.Level)
		}
		if got := entry.ContextMap()["status"]; got != int64(http.StatusInternalServerError) {
			t.Errorf("status field = %v, want 500", got)
		}
	})

	t.Run("prefers the active OTel span trace id", func(t *testing.T) {
		core, logs := observer.New(zapcore.InfoLevel)
		tp := sdktrace.NewTracerProvider()
		defer func() { _ = tp.Shutdown(t.Context()) }()

		var wantTraceID string
		r := gin.New()
		// Simulate TracingMiddleware: put an active span on the request context
		// before LoggingMiddleware resolves the trace id.
		r.Use(func(c *gin.Context) {
			ctx, span := tp.Tracer("test").Start(c.Request.Context(), "op")
			defer span.End()
			wantTraceID = span.SpanContext().TraceID().String()
			c.Request = c.Request.WithContext(ctx)
			c.Next()
		})
		r.Use(LoggingMiddleware(zap.New(core)))
		r.GET("/traced", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/traced", nil)
		req.Header.Set(TraceIDHeader, "header-id-must-lose")
		r.ServeHTTP(w, req)

		entry := requestLog(t, logs)
		if got := entry.ContextMap()["trace_id"]; got != wantTraceID {
			t.Errorf("trace_id field = %v, want span trace id %q", got, wantTraceID)
		}
		if got := w.Header().Get(TraceIDHeader); got != wantTraceID {
			t.Errorf("response %s = %q, want span trace id %q", TraceIDHeader, got, wantTraceID)
		}
	})
}

func TestGetLoggerFromGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("falls back to the global logger without middleware", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		if GetLoggerFromGinContext(c) == nil {
			t.Error("GetLoggerFromGinContext returned nil, want a non-nil zap.Logger")
		}
	})

	t.Run("returns the request-scoped logger set by LoggingMiddleware", func(t *testing.T) {
		core, logs := observer.New(zapcore.InfoLevel)
		r := gin.New()
		r.Use(LoggingMiddleware(zap.New(core)))
		r.GET("/scoped", func(c *gin.Context) {
			GetLoggerFromGinContext(c).Info("from handler")
			c.String(http.StatusOK, "ok")
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/scoped", nil)
		req.Header.Set(TraceIDHeader, "scoped-trace")
		r.ServeHTTP(w, req)

		entries := logs.FilterMessage("from handler").All()
		if len(entries) != 1 {
			t.Fatalf("got %d 'from handler' entries, want 1", len(entries))
		}
		if got := entries[0].ContextMap()["trace_id"]; got != "scoped-trace" {
			t.Errorf("trace_id field = %v, want scoped-trace", got)
		}
	})
}
