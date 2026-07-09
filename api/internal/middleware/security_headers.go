package middleware

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware adds security headers to all HTTP responses
// SECURITY: These headers protect against common web vulnerabilities
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// X-Frame-Options: Prevents clickjacking attacks
		c.Header("X-Frame-Options", "DENY")

		// X-Content-Type-Options: Prevents MIME type sniffing
		c.Header("X-Content-Type-Options", "nosniff")

		// X-XSS-Protection: Enables browser XSS filter (legacy support)
		c.Header("X-XSS-Protection", "1; mode=block")

		// Referrer-Policy: Controls referrer information
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions-Policy: Restricts browser features
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")

		// X-Permitted-Cross-Domain-Policies: Restricts Adobe Flash/PDF cross-domain requests
		c.Header("X-Permitted-Cross-Domain-Policies", "none")

		// Cache-Control: Prevent caching of sensitive API responses
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
		c.Header("Pragma", "no-cache")

		// Process request
		c.Next()
	}
}
