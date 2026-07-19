package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/openmentor-io/openmentor/api/internal/handlers"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func getTestDBPool(t *testing.T) *pgxpool.Pool {
	// Use DATABASE_URL from environment or skip test if not available
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping database connectivity test")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("Could not connect to database: %v", err)
	}

	return pool
}

func TestHealthHandler_Healthcheck(t *testing.T) {
	// Setup
	pool := getTestDBPool(t)
	defer pool.Close()

	handler := handlers.NewHealthHandler(pool)
	router := gin.New()
	router.GET("/healthcheck", handler.Healthcheck)

	// Create request
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/healthcheck", http.NoBody)

	// Execute
	router.ServeHTTP(w, req)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache, no-store, max-age=0, must-revalidate", w.Header().Get("Cache-Control"))
	assert.JSONEq(t, `{"status":"healthy"}`, w.Body.String())
}
