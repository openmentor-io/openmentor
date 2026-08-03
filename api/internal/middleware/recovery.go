package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/errortracking"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// RecoveryMiddleware catches panics, reports them to PostHog error tracking,
// and returns a 500 response to the client.
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				stack := debug.Stack()

				// This middleware is the OUTERMOST one, so a panic unwinds past
				// every redaction the observability middleware does after
				// c.Next(). The raw path is a capability of its own
				// (/api/v1/reviews/<uuid> authorizes submitting a review), so it
				// is redacted here too rather than only on the non-panic path.
				logger.Error("panic recovered",
					zap.Any("panic", recovered),
					zap.String("stack", string(stack)),
					zap.String("path", redact.Path(c.Request.URL.Path)),
					zap.String("method", c.Request.Method),
				)

				errortracking.CapturePanic(recovered, stack)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": "internal server error",
				})
			}
		}()
		c.Next()
	}
}
