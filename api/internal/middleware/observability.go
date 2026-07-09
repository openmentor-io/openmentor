package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"github.com/openmentor-io/openmentor-api/pkg/metrics"
	"go.uber.org/zap"
)

// sensitiveQueryParams are redacted from logs to avoid leaking secrets.
var sensitiveQueryParams = map[string]bool{
	"token": true, "password": true, "secret": true, "key": true,
	"auth": true, "api_key": true, "apikey": true,
}

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

		// Log request (use actual path for debugging, but route template for metrics)
		actualPath := c.Request.URL.Path
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
					params[p.Key] = p.Value
				}
				fields = append(fields, zap.Any("route_params", params))
			}

			if query := c.Request.URL.Query(); len(query) > 0 {
				sanitized := make(map[string]string, len(query))
				for k, v := range query {
					if !sensitiveQueryParams[strings.ToLower(k)] && len(v) > 0 {
						sanitized[k] = v[0]
					}
				}
				if len(sanitized) > 0 {
					fields = append(fields, zap.Any("query_params", sanitized))
				}
			}

			if len(c.Errors) > 0 {
				fields = append(fields, zap.String("error", c.Errors.String()))
			}
		}

		// Log with actual path for debugging purposes
		logger.LogHTTPRequest(c.Request.Context(), method, actualPath, status, duration, fields...)
	}
}
