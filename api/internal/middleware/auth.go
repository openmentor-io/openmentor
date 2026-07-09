package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/pkg/jwt"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.uber.org/zap"
)

// TokenAuthMiddleware validates authentication tokens
func TokenAuthMiddleware(validTokens ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("mentors_api_auth_token")

		if token == "" {
			logger.Warn("Missing authentication token",
				zap.String("path", c.Request.URL.Path),
				zap.String("client_ip", c.ClientIP()),
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing authentication token"})
			c.Abort()
			return
		}

		valid := false
		for _, validToken := range validTokens {
			if jwt.TimingSafeCompare(token, validToken) {
				valid = true
				break
			}
		}

		if !valid {
			logger.Warn("Invalid authentication token",
				zap.String("path", c.Request.URL.Path),
				zap.String("client_ip", c.ClientIP()),
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authentication token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// InternalAPIAuthMiddleware validates internal API token
func MCPServerAuthMiddleware(validToken string, allowAll bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if allowAll {
			logger.Info("MCP server access allowed for all",
				zap.String("path", c.Request.URL.Path),
				zap.String("client_ip", c.ClientIP()),
			)
			c.Next()
			return
		}

		token := c.GetHeader("x-mcp-auth-token")

		if token == "" || !jwt.TimingSafeCompare(token, validToken) {
			logger.Warn("Invalid MCP server token",
				zap.String("path", c.Request.URL.Path),
				zap.String("client_ip", c.ClientIP()),
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing MCP server token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// InternalAPIAuthMiddleware validates internal API token
func InternalAPIAuthMiddleware(validToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("x-internal-mentors-api-auth-token")

		if token == "" || !jwt.TimingSafeCompare(token, validToken) {
			logger.Warn("Invalid internal API token",
				zap.String("path", c.Request.URL.Path),
				zap.String("client_ip", c.ClientIP()),
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing internal API token"})
			c.Abort()
			return
		}

		c.Next()
	}
}
