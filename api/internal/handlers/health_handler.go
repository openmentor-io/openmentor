package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthHandler struct {
	pool             *pgxpool.Pool
	mentorCacheReady func() bool
}

func NewHealthHandler(pool *pgxpool.Pool, mentorCacheReady func() bool) *HealthHandler {
	return &HealthHandler{
		pool:             pool,
		mentorCacheReady: mentorCacheReady,
	}
}

func (h *HealthHandler) Healthcheck(c *gin.Context) {
	c.Header("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")

	// Check database connectivity
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"reason": "database unreachable",
		})
		return
	}

	// Check if mentor cache is ready
	if !h.mentorCacheReady() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"reason": "cache not ready",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}
