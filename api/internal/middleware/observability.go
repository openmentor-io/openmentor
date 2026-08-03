package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// ObservabilityMiddleware instruments HTTP requests with metrics and logging
func ObservabilityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		method := c.Request.Method

		// Track active requests (method only - route not known until after routing)
		metrics.ActiveRequests.WithLabelValues(method).Inc()
		defer metrics.ActiveRequests.WithLabelValues(method).Dec()

		// Process request - this allows Gin to set the matched route
		c.Next()

		// Get route template AFTER routing (prevents cardinality explosion)
		// c.FullPath() returns the route pattern like "/api/v1/mentor/requests/:id"
		// instead of the actual path like "/api/v1/mentor/requests/recXYZ123"
		path := c.FullPath()
		if path == "" {
			// Fallback for unmatched routes (404s) - use a generic label
			path = "unmatched"
		}

		// Measure duration
		duration := metrics.MeasureDuration(start)
		status := c.Writer.Status()
		statusStr := strconv.Itoa(status)

		// Record metrics with route template (not actual path)
		metrics.HTTPRequestDuration.WithLabelValues(method, path, statusStr).Observe(duration)
		metrics.HTTPRequestTotal.WithLabelValues(method, path, statusStr).Inc()

		// The path itself can be a capability: /api/v1/reviews/<uuid> is a bearer
		// URL for submitting a review, so the id is stripped before the path is
		// logged or left on the span (P14).
		loggedPath := redactedPath(c)
		fields := []zap.Field{
			zap.String("client_ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Int("response_size", c.Writer.Size()),
		}

		// For error responses, add route params and query params for traceability
		if status >= 400 {
			if len(c.Params) > 0 {
				params := make(map[string]string, len(c.Params))
				for _, p := range c.Params {
					switch {
					case redact.IsUUID(p.Value):
						// Hashed, not dropped: a 4xx on one of the request routes
						// is the line an operator has to tie back to one record.
						params[p.Key] = redact.ID(p.Value)
					case redact.SensitiveKey(p.Key):
						params[p.Key] = redact.Placeholder
					default:
						params[p.Key] = p.Value
					}
				}
				fields = append(fields, zap.Any("route_params", params))
			}

			if query := c.Request.URL.Query(); len(query) > 0 {
				fields = append(fields, zap.Any("query_params", redact.Query(query)))
			}

			if len(c.Errors) > 0 {
				fields = append(fields, zap.String("error", c.Errors.String()))
			}
		}

		redactSpanURL(c, loggedPath)

		logger.LogHTTPRequest(c.Request.Context(), method, loggedPath, status, duration, fields...)
	}
}

// redactedPath strips capability-bearing values out of the raw request path.
// The parameter NAME is not enough to find them — the :id in
// /api/v1/mentor/requests/:id is the same client_requests.id that
// /api/v1/reviews/:requestId authorizes on — so redact.Path matches on the
// value's shape, and the name rule then covers secrets that are not UUIDs.
func redactedPath(c *gin.Context) string {
	path := redact.Path(c.Request.URL.Path)
	for _, p := range c.Params {
		if p.Value != "" && redact.SensitiveKey(p.Key) {
			path = strings.ReplaceAll(path, p.Value, redact.Placeholder)
		}
	}
	return path
}

// redactSpanURL overwrites the url.* attributes otelgin tagged the server span
// with at span start. Writing the same key again is what redacts it: the SDK
// keeps the last value per key when it deduplicates attributes.
func redactSpanURL(c *gin.Context, loggedPath string) {
	span := trace.SpanFromContext(c.Request.Context())
	if !span.IsRecording() {
		return
	}

	attrs := make([]attribute.KeyValue, 0, 2)
	if loggedPath != c.Request.URL.Path {
		attrs = append(attrs, attribute.String("url.path", loggedPath))
	}
	if raw := c.Request.URL.RawQuery; raw != "" {
		if redacted := redact.QueryString(raw); redacted != raw {
			attrs = append(attrs, attribute.String("url.query", redacted))
		}
	}
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
}
