package worker

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/openmentor-io/openmentor-api/pkg/trigger"
)

// WorkerTokenHeader is the shared-secret header checked on /jobs/* routes.
// It replaces the Azure Functions ?code= function keys the func app used
// (authLevel "function" on every HTTP-triggered function).
//
// The API's pkg/trigger sends this header (from WORKER_AUTH_TOKEN) on both
// trigger.CallAsync and trigger.CallAsyncWithPayload requests when the
// token is configured; the constant is shared so the two sides can't drift.
const WorkerTokenHeader = trigger.WorkerTokenHeader

// AuthMiddleware validates the X-Worker-Token shared secret on job routes.
// When no token is configured (WORKER_AUTH_TOKEN empty) all requests are
// allowed - the worker is only reachable on the internal Docker network.
func AuthMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}

		provided := c.GetHeader(WorkerTokenHeader)
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Next()
	}
}
