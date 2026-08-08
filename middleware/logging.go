package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/duynhlab/pkg/logger/zapx"
	"github.com/duynhlab/pkg/obsx"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const TraceIDHeader = "X-Trace-ID"
const TraceParentHeader = "traceparent"

// GetTraceID extracts trace-id from request headers or generates a new one
func GetTraceID(c *gin.Context) string {
	// Prefer the active OTel span's trace ID for log/trace correlation.
	if id := obsx.TraceIDFromContext(c.Request.Context()); id != "" {
		return id
	}

	// Try W3C Trace Context first (traceparent header)
	if traceParent := c.GetHeader(TraceParentHeader); traceParent != "" {
		// traceparent format: version-trace_id-parent_id-flags
		// Extract trace_id (second part)
		parts := splitTraceParent(traceParent)
		if len(parts) >= 2 && parts[1] != "" {
			return parts[1]
		}
	}

	// Fallback to X-Trace-ID header
	if traceID := c.GetHeader(TraceIDHeader); traceID != "" {
		return traceID
	}

	// Generate new trace-id if not present
	return generateTraceID()
}

// splitTraceParent splits traceparent header value
func splitTraceParent(traceParent string) []string {
	// Simple split by hyphen, traceparent format: 00-<trace_id>-<parent_id>-<flags>
	parts := make([]string, 0, 4)
	start := 0
	for i := range len(traceParent) {
		if traceParent[i] == '-' {
			if start < i {
				parts = append(parts, traceParent[start:i])
			}
			start = i + 1
		}
	}
	if start < len(traceParent) {
		parts = append(parts, traceParent[start:])
	}
	return parts
}

// generateTraceID generates a trace-id using random bytes
func generateTraceID() string {
	// Generate 16 random bytes (32 hex characters)
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// LoggingMiddleware creates a Gin middleware for structured logging with trace-id
func LoggingMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		// spanTraceID is the ONLY id that may reach telemetry: the active span's,
		// or empty. The logged string previously came from GetTraceID, which
		// never consults the span, so a record could advertise an id that is not
		// the trace id — searching by it finds nothing in the backend. The native
		// link (TraceContext, below) was already correct here; this makes the
		// readable field agree with it.
		ctx := c.Request.Context()
		spanTraceID := obsx.TraceIDFromContext(ctx)

		// The response header keeps its previous behaviour, generated fallback
		// included: correlate-by-header is a client contract, separate from what
		// this service puts in its own telemetry.
		headerTraceID := spanTraceID
		if headerTraceID == "" {
			headerTraceID = GetTraceID(c)
		}
		c.Set("trace_id", headerTraceID)

		// TraceContext binds the request context so the otelzap bridge stamps
		// the native trace_id/span_id on every OTLP log record. The readable
		// string field is bound only when a span exists.
		withFields := []zap.Field{obsx.TraceContext(ctx)}
		if spanTraceID != "" {
			withFields = append(withFields, zap.String("trace_id", spanTraceID))
		}
		loggerWithTrace := logger.With(withFields...)

		// Inject logger into context
		ctx = zapx.WithContext(ctx, loggerWithTrace)
		c.Request = c.Request.WithContext(ctx)

		// Create a helper to get logger from gin context explicitly if needed (legacy compatibility)
		c.Set("logger", loggerWithTrace)

		// Add trace-id to response header
		c.Header(TraceIDHeader, headerTraceID)

		// Process request
		c.Next()

		// Calculate duration
		duration := time.Since(start)
		statusCode := c.Writer.Status()

		// Routine successful probes are traffic about the platform, not the
		// domain. TracingMiddleware already excludes them from spans and RED
		// metrics through the same shouldTrace list; excluding them here is what
		// makes that contract true for logs too. A FAILING probe is kept.
		if !shouldTrace(path) && statusCode < 400 {
			return
		}

		// 4xx is a rejected request, not a broken service: an expected business
		// rejection must not read as an infrastructure error.
		level := zapcore.InfoLevel
		switch {
		case statusCode >= 500:
			level = zapcore.ErrorLevel
		case statusCode >= 400:
			level = zapcore.WarnLevel
		}
		loggerWithTrace.Log(level, "HTTP request",
			zap.String("method", method),
			zap.String("path", path),
			zap.Int("status", statusCode),
			zap.Duration("duration", duration),
			zap.String("client_ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
		)
	}
}

// GetLoggerFromGinContext retrieves logger from Gin context (legacy adapter)
// New code should use zapx.FromContext(ctx) directly
func GetLoggerFromGinContext(c *gin.Context) *zap.Logger {
	return zapx.FromContext(c.Request.Context())
}
