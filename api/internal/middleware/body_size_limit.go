package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodySizeLimitMiddleware limits the size of request bodies
// SECURITY: Prevents denial-of-service attacks through oversized payloads
func BodySizeLimitMiddleware(maxBodySize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip for GET, HEAD, OPTIONS requests (no body)
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		// Limit the request body size
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodySize)

		c.Next()
	}
}
