package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readAllHandler drains the body and reports whether the cap stopped it. Reading
// is the point: http.MaxBytesReader only fails at Read time, so a handler that
// never reads never notices, and a test that never reads proves nothing.
func readAllHandler(c *gin.Context) {
	if _, err := io.ReadAll(c.Request.Body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.String(http.StatusRequestEntityTooLarge, "too large")
			return
		}
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Status(http.StatusOK)
}

func postBody(t *testing.T, router *gin.Engine, path string, size int) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Repeat("a", size)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

// TestGlobalCapAppliesToRoutesThatSetNone is the gap H14 names: before the global
// cap, every mentor-profile and admin-moderation POST accepted an unbounded body.
func TestGlobalCapAppliesToRoutesThatSetNone(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
	router.POST("/profile", readAllHandler)

	assert.Equal(t, http.StatusOK, postBody(t, router, "/profile", int(DefaultMaxBodyBytes)))
	assert.Equal(t, http.StatusRequestEntityTooLarge,
		postBody(t, router, "/profile", int(DefaultMaxBodyBytes)+1))
}

// TestRouteOverrideReplacesTheGlobalCap: the override must REPLACE, not nest.
// Nesting would leave the smaller global MaxBytesReader in the chain, so the
// image routes would still fail at 256 KiB and the 10 MiB override would be a
// no-op nobody noticed until an upload failed in production.
func TestRouteOverrideReplacesTheGlobalCap(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
	router.POST("/picture", BodySizeLimitMiddleware(MaxImageBodyBytes), readAllHandler)

	// Comfortably over the global cap, comfortably under the route's.
	assert.Equal(t, http.StatusOK, postBody(t, router, "/picture", int(DefaultMaxBodyBytes)*4))
	assert.Equal(t, http.StatusOK, postBody(t, router, "/picture", int(MaxImageBodyBytes)))
	assert.Equal(t, http.StatusRequestEntityTooLarge,
		postBody(t, router, "/picture", int(MaxImageBodyBytes)+1))
}

// TestRouteOverrideCanTightenTheGlobalCap: the small hand-set caps (4 KiB on the
// username and status routes, 10 KiB on confirm) must still bind.
func TestRouteOverrideCanTightenTheGlobalCap(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
	router.POST("/username", BodySizeLimitMiddleware(4*1024), readAllHandler)

	assert.Equal(t, http.StatusOK, postBody(t, router, "/username", 4*1024))
	assert.Equal(t, http.StatusRequestEntityTooLarge, postBody(t, router, "/username", 4*1024+1))
}

// TestRepeatedOverridesDoNotStack: three layers, the innermost wins in both
// directions.
func TestRepeatedOverridesDoNotStack(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(1024))
	router.POST("/x", BodySizeLimitMiddleware(64), BodySizeLimitMiddleware(4096), readAllHandler)

	assert.Equal(t, http.StatusOK, postBody(t, router, "/x", 4096))
	assert.Equal(t, http.StatusRequestEntityTooLarge, postBody(t, router, "/x", 4097))
}

// TestBodylessMethodsAreUntouched: the middleware must not wrap a GET, whose
// body http.Server reports as http.NoBody.
func TestBodylessMethodsAreUntouched(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(1))
	router.GET("/mentors", readAllHandler)

	req := httptest.NewRequest(http.MethodGet, "/mentors", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestBodyCapAndAdmissionLimiterCoexist: the two bound different things — bytes
// per request and requests in flight — and the image routes carry both, in that
// order. Neither may swallow the other.
func TestBodyCapAndAdmissionLimiterCoexist(t *testing.T) {
	admission := NewAdmissionLimiter(1, 20*time.Millisecond)

	router := gin.New()
	router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
	router.POST("/picture", admission.Middleware(),
		BodySizeLimitMiddleware(MaxImageBodyBytes), readAllHandler)

	// The override still wins with the admission middleware between the two.
	assert.Equal(t, http.StatusOK, postBody(t, router, "/picture", int(DefaultMaxBodyBytes)*4))
	// And the admission slot is released, so a second request is not shed.
	assert.Equal(t, http.StatusOK, postBody(t, router, "/picture", int(DefaultMaxBodyBytes)*4))
	require.Equal(t, 0, admission.InFlight())

	// An over-cap body is still rejected on size, not on admission.
	assert.Equal(t, http.StatusRequestEntityTooLarge,
		postBody(t, router, "/picture", int(MaxImageBodyBytes)+1))
	require.Equal(t, 0, admission.InFlight())
}

// TestProfileSaveFitsUnderTheGlobalCap sizes the cap against the biggest
// legitimate body it applies to: a full profile save, whose text fields are
// capped at 5,000 characters each.
func TestProfileSaveFitsUnderTheGlobalCap(t *testing.T) {
	const maxTextField = 5000
	const textFields = 8
	const tagsAndOverhead = 4096

	assert.Less(t, int64(maxTextField*textFields+tagsAndOverhead), DefaultMaxBodyBytes,
		"the global cap must leave room for the largest non-image request")
}
