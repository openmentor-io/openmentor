// Package worker implements the background worker: an internal HTTP server
// that receives the API's async event triggers (formerly Azure Functions
// HTTP triggers) and a cron scheduler for the daily jobs (formerly Azure
// Functions timer triggers). See docs/migration/DECISIONS.md D6.
package worker

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/openmentor-io/openmentor-api/config"
	"github.com/openmentor-io/openmentor-api/internal/middleware"
	"github.com/openmentor-io/openmentor-api/pkg/metrics"
)

// Server is the worker's internal HTTP server. It is NOT publicly exposed:
// it listens on the internal Docker network only and serves the /jobs/*
// endpoints the API calls via pkg/trigger, plus /healthz and /metrics.
type Server struct {
	engine *gin.Engine
	jobs   *gin.RouterGroup
	pool   *pgxpool.Pool
	http   *http.Server
}

// NewServer builds the worker HTTP server: recovery + observability
// middleware (same conventions as the API), a health endpoint, a Prometheus
// metrics endpoint and an authenticated /jobs route group.
func NewServer(cfg *config.Config, pool *pgxpool.Pool) *Server {
	gin.SetMode(cfg.Server.GinMode)
	engine := gin.New()

	engine.Use(middleware.RecoveryMiddleware())
	// Same middleware order as the API (cmd/api/main.go): otelgin starts the
	// server span (and extracts the W3C traceparent the API's pkg/trigger
	// calls carry, so worker job spans nest under the API's request trace)
	// before the metrics/logging middleware.
	engine.Use(otelgin.Middleware(cfg.Worker.ServiceName))
	engine.Use(middleware.ObservabilityMiddleware())

	s := &Server{
		engine: engine,
		pool:   pool,
	}

	engine.GET("/healthz", s.healthz)
	engine.GET("/metrics", gin.WrapH(promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{})))

	// All job endpoints require the shared-secret X-Worker-Token header
	// (when WORKER_AUTH_TOKEN is configured).
	s.jobs = engine.Group("/jobs", AuthMiddleware(cfg.Worker.AuthToken))

	return s
}

// RegisterHandler registers a job endpoint under /jobs for the given HTTP
// methods (default: POST). The stage-2 handlers are registered through
// RegisterJobRoutes (see jobs.go). Endpoints and params mirror the func app:
//
//	POST|GET /jobs/new-mentor-watcher?mentorId=            <- new-mentor-watcher
//	POST|GET /jobs/new-request-watcher?requestId=          <- new-request-watcher
//	POST     /jobs/mentor-login-email      (JSON body)     <- mentor-login-email
//	POST     /jobs/moderator-login-email   (JSON body)     <- moderator-login-email
//	POST     /jobs/mentor-moderation-action (JSON body)    <- mentor-moderation-action
//	POST|GET /jobs/process-mentee-review?reviewId=         <- process-mentee-review
//	GET      /jobs/request-process-finished?requestId=     <- request-process-finished
func (s *Server) RegisterHandler(path string, handler gin.HandlerFunc, methods ...string) {
	if len(methods) == 0 {
		methods = []string{http.MethodPost}
	}
	for _, method := range methods {
		s.jobs.Handle(method, path, handler)
	}
}

// healthz reports liveness/readiness. It pings the database with a short
// timeout so a broken pool marks the container unhealthy.
func (s *Server) healthz(c *gin.Context) {
	if s.pool != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := s.pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  "database ping failed",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Start runs the HTTP server on the configured worker port. It blocks until
// the server stops; callers run it in a goroutine.
func (s *Server) Start(port string) error {
	s.http = &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           s.engine,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // job handlers may do real work
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return s.http.ListenAndServe()
}

// Shutdown drains in-flight requests and stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

// Engine exposes the underlying Gin engine (used by tests).
func (s *Server) Engine() *gin.Engine {
	return s.engine
}
