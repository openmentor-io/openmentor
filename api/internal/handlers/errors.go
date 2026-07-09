package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/openmentor-io/openmentor/api/pkg/errortracking"
)

// attachError attaches err to the gin context so the observability middleware
// can include the reason in the request log. c.Error() returns *gin.Error (not
// the error interface), so we suppress errcheck here intentionally.
func attachError(c *gin.Context, err error) {
	if err != nil {
		_ = c.Error(err) //nolint:errcheck
	}
}

// respondError sends an error JSON response and attaches the error to the gin context
// so the observability middleware can include the reason in the request log.
// Unexpected server errors (5xx) are also reported to PostHog error tracking.
func respondError(c *gin.Context, status int, message string, err error) {
	attachError(c, err)
	if status >= http.StatusInternalServerError && err != nil {
		errortracking.CaptureException(err, map[string]interface{}{
			"http_status": status,
			"http_path":   c.FullPath(),
			"http_method": c.Request.Method,
		})
	}
	c.JSON(status, gin.H{"error": message})
}

// respondErrorWithDetails sends an error response with an additional details field.
func respondErrorWithDetails(c *gin.Context, status int, message string, details any, err error) { //nolint:unparam
	attachError(c, err)
	c.JSON(status, gin.H{"error": message, "details": details})
}
