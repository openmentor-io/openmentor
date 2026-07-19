// The worker binary runs the background jobs that used to live in the
// openmentor-func Azure Functions app (decision D6): an internal HTTP
// server that receives the API's async event triggers plus a cron scheduler
// for the daily jobs. It ships in the same image as the api and migrate
// binaries but runs as a separate container with its own crash domain,
// DB pool cap and resource limits.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/worker"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/db"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/errortracking"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/profiling"
	"github.com/openmentor-io/openmentor/api/pkg/slack"
	"github.com/openmentor-io/openmentor/api/pkg/tracing"
)

func main() {
	// Load configuration (same config package as the API)
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger with the worker's own service identity
	err = logger.Initialize(logger.Config{
		Level:       cfg.Logging.Level,
		LogDir:      cfg.Logging.Dir,
		Environment: cfg.Server.AppEnv,
		ServiceName: cfg.Worker.ServiceName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting OpenMentor worker",
		zap.String("environment", cfg.Server.AppEnv),
		zap.String("port", cfg.Worker.Port),
	)

	// Initialize distributed tracing (same Alloy pipeline as the API)
	tracerShutdown, err := tracing.InitTracer(
		cfg.Worker.ServiceName,
		cfg.Observability.ServiceNamespace,
		cfg.Observability.ServiceVersion,
		cfg.Observability.ServiceInstanceID,
		cfg.Server.AppEnv,
		cfg.Observability.AlloyEndpoint,
	)
	if err != nil {
		logger.Fatal("Failed to initialize tracer", zap.Error(err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := tracerShutdown(ctx); shutdownErr != nil {
			logger.Error("Failed to shutdown tracer", zap.Error(shutdownErr))
		}
	}()

	// Initialize continuous profiling (same O11Y_PROFILING_* config and
	// Alloy Pyroscope receiver as the API). The API's profile app name
	// comes from O11Y_PROFILING_APP_NAME; the worker instead reuses its
	// observability identity (O11Y_WORKER_SERVICE_NAME, default
	// "openmentor-worker") so the two binaries' profiles never mix, with
	// no extra env var - consistent with how the logger, tracer and
	// metrics are named above.
	workerProfilingCfg := cfg.Profiling
	workerProfilingCfg.AppName = cfg.Worker.ServiceName
	profilerStop, err := profiling.InitProfiler(
		workerProfilingCfg,
		cfg.Worker.ServiceName,
		cfg.Observability.ServiceNamespace,
		cfg.Observability.ServiceVersion,
		cfg.Observability.ServiceInstanceID,
		cfg.Server.AppEnv,
	)
	if err != nil {
		logger.Fatal("Failed to initialize profiler", zap.Error(err))
	}
	defer profilerStop()

	// Initialize PostHog error tracking (panics + job errors)
	errortracking.Init(
		cfg.PostHog.APIKey,
		cfg.PostHog.Host,
		cfg.Server.AppEnv,
		cfg.Worker.ServiceName,
		cfg.Observability.ServiceVersion,
	)
	defer errortracking.Close()

	// Initialize metrics with the worker's service name
	metrics.Init(cfg.Worker.ServiceName)
	metrics.RecordInfrastructureMetrics()

	// Initialize the worker's own PostgreSQL pool with a smaller cap than
	// the API (noisy-neighbour isolation at the connection level too).
	workerDBConfig := cfg.Database
	workerDBConfig.MaxConns = cfg.Worker.DBMaxConns
	workerDBConfig.MinConns = 1

	pool, err := db.NewPool(context.Background(), workerDBConfig)
	if err != nil {
		logger.Fatal("Failed to initialize database connection pool", zap.Error(err))
	}
	defer pool.Close()

	// Initialize the analytics tracker (same provider config as the API,
	// but events are attributed to the worker source system).
	analyticsTracker := analytics.NewTracker(&analytics.Config{
		Provider:               cfg.ResolvedAnalyticsProvider(),
		SourceSystem:           "worker",
		Environment:            cfg.Server.AppEnv,
		EventVersion:           cfg.ResolvedAnalyticsEventVersion(),
		PostHogEnabled:         cfg.PostHog.Enabled,
		PostHogAPIKey:          cfg.PostHog.APIKey,
		PostHogHost:            cfg.PostHog.Host,
		PostHogCaptureEndpoint: cfg.PostHog.CaptureEndpoint,
		PostHogDisableGeoIP:    cfg.PostHog.DisableGeoIP,
	})

	// The SESv2 email sender used by all job handlers (pkg/email).
	emailSender := email.NewSender(email.Config{
		Region:           cfg.Email.SESRegion,
		AccessKeyID:      cfg.Email.SESAccessKeyID,
		SecretAccessKey:  cfg.Email.SESSecretAccessKey,
		Endpoint:         cfg.Email.SESEndpoint,
		AppEnv:           cfg.Server.AppEnv,
		DevEmailOverride: cfg.Email.DevEmailOverride,
	})

	// Optional community-Slack auto-invite for approved mentors
	// (mentor-moderation-action job). Disabled unless SLACK_ADMIN_TOKEN is
	// set; config.Validate already rejected half-configured setups.
	var slackInviter worker.SlackInviter
	if cfg.Slack.Enabled() {
		slackInviter = slack.NewInviter(slack.Config{
			Token:      cfg.Slack.AdminToken,
			TeamID:     cfg.Slack.TeamID,
			ChannelIDs: cfg.Slack.InviteChannelIDs,
		}, httpclient.NewStandardClient())
		logger.Info("Slack auto-invite for approved mentors enabled",
			zap.String("team_id", cfg.Slack.TeamID))
	}

	// The job handlers: async event triggers (stage 2) and cron jobs
	// (stage 3) share the same dependencies.
	handlers := worker.NewHandlers(
		worker.NewRepository(pool),
		emailSender,
		analyticsTracker,
		slackInviter,
		cfg,
	)

	// Start the cron scheduler for the timer jobs
	var cronScheduler *worker.Cron
	if cfg.Worker.CronEnabled {
		cronScheduler, err = worker.NewCron(handlers)
		if err != nil {
			logger.Fatal("Failed to initialize cron scheduler", zap.Error(err))
		}
		cronScheduler.Start()
	} else {
		logger.Warn("Cron scheduler disabled via WORKER_CRON_ENABLED=false")
	}

	// Start the internal HTTP server for the async event triggers plus the
	// manual POST /jobs/cron/<name> job triggers (staging smoke tests).
	// It is only reachable on the internal Docker network.
	server := worker.NewServer(cfg, pool)
	server.RegisterJobRoutes(handlers)
	server.RegisterCronRoutes(handlers)

	if cfg.Worker.AuthToken == "" {
		logger.Warn("WORKER_AUTH_TOKEN not set: /jobs endpoints accept unauthenticated requests")
	}

	go func() {
		logger.Info("Worker HTTP server started", zap.String("port", cfg.Worker.Port))
		if serveErr := server.Start(cfg.Worker.Port); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Fatal("Worker HTTP server failed", zap.Error(serveErr))
		}
	}()

	// Wait for SIGINT/SIGTERM, then shut down gracefully:
	// stop cron (wait for in-flight jobs), drain HTTP, close pool (deferred).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down worker...")

	if cronScheduler != nil {
		cronCtx := cronScheduler.Stop()
		select {
		case <-cronCtx.Done():
			logger.Info("Cron jobs drained")
		case <-time.After(30 * time.Second):
			logger.Warn("Timed out waiting for in-flight cron jobs")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Worker HTTP server forced to shutdown", zap.Error(err))
	}

	logger.Info("Worker exited")
}
