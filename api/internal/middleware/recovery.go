package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/errortracking"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// RecoveryMiddleware catches panics, reports them to PostHog error tracking,
// and returns a 500 response to the client.
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				stack := debug.Stack()

				logger.Error("panic recovered",
					zap.Any("panic", recovered),
					zap.String("stack", string(stack)),
					zap.String("path", c.Request.URL.Path),
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
