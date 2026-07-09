package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openmentor-io/openmentor/api/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func init() {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Initialize logger for tests
	_ = logger.Initialize(logger.Config{
		Level:       "info",
		Environment: "test",
		ServiceName: "openmentor-api-test",
	})
}

func TestTokenAuthMiddleware_ValidToken(t *testing.T) {
	// Setup
	router := gin.New()
	validTokens := []string{"token1", "token2", "token3"}

	// Track if handler was called
	handlerCalled := false
	router.Use(middleware.TokenAuthMiddleware(validTokens...))
	router.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	// Create request with valid token
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("mentors_api_auth_token", "token2")

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	assert.True(t, handlerCalled, "Handler should be called for valid token")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTokenAuthMiddleware_InvalidToken(t *testing.T) {
	// Setup
	router := gin.New()
	validTokens := []string{"token1", "token2"}

	// Track if handler was called
	handlerCalled := false
	router.Use(middleware.TokenAuthMiddleware(validTokens...))
	router.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	// Create request with invalid token
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("mentors_api_auth_token", "invalid-token")

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	assert.False(t, handlerCalled, "Handler should not be called for invalid token")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTokenAuthMiddleware_MissingToken(t *testing.T) {
	// Setup
	router := gin.New()
	validTokens := []string{"token1", "token2"}

	// Track if handler was called
	handlerCalled := false
	router.Use(middleware.TokenAuthMiddleware(validTokens...))
	router.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	// Create request without token
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", http.NoBody)

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	assert.False(t, handlerCalled, "Handler should not be called when token is missing")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTokenAuthMiddleware_EmptyTokenList(t *testing.T) {
	// Setup
	router := gin.New()

	// Track if handler was called
	handlerCalled := false
	router.Use(middleware.TokenAuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	// Create request with a token
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("mentors_api_auth_token", "some-token")

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	assert.False(t, handlerCalled, "Handler should not be called when no valid tokens are configured")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInternalAPIAuthMiddleware_ValidToken(t *testing.T) {
	// Setup
	router := gin.New()
	validToken := "internal-secret-token"

	// Track if handler was called
	handlerCalled := false
	router.Use(middleware.InternalAPIAuthMiddleware(validToken))
	router.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	// Create request with valid token
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("x-internal-mentors-api-auth-token", validToken)

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	assert.True(t, handlerCalled, "Handler should be called for valid internal token")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestInternalAPIAuthMiddleware_InvalidToken(t *testing.T) {
	// Setup
	router := gin.New()
	validToken := "internal-secret-token"

	// Track if handler was called
	handlerCalled := false
	router.Use(middleware.InternalAPIAuthMiddleware(validToken))
	router.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	// Create request with invalid token
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("x-internal-mentors-api-auth-token", "wrong-token")

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	assert.False(t, handlerCalled, "Handler should not be called for invalid internal token")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInternalAPIAuthMiddleware_MissingToken(t *testing.T) {
	// Setup
	router := gin.New()
	validToken := "internal-secret-token"

	// Track if handler was called
	handlerCalled := false
	router.Use(middleware.InternalAPIAuthMiddleware(validToken))
	router.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	// Create request without token
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", http.NoBody)

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	assert.False(t, handlerCalled, "Handler should not be called when internal token is missing")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
