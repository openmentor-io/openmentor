package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/cache"
	"github.com/openmentor-io/openmentor/api/internal/handlers"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/db"
	"github.com/openmentor-io/openmentor/api/pkg/errortracking"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/jwt"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/profiling"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"github.com/openmentor-io/openmentor/api/pkg/tracing"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

// registerAPIRoutes registers common API routes for a given router group
func registerAPIRoutes(
	group *gin.RouterGroup,
	cfg *config.Config,
	generalRateLimiter, contactRateLimiter, registrationRateLimiter, confirmResendRateLimiter *middleware.RateLimiter,
	mentorHandler *handlers.MentorHandler,
	contactHandler *handlers.ContactHandler,
	logsHandler *handlers.LogsHandler,
	registrationHandler *handlers.RegistrationHandler,
	reviewHandler *handlers.ReviewHandler,
	migrationIntentHandler *handlers.MigrationIntentHandler,
	mentorConfirmationHandler *handlers.MentorConfirmationHandler,
) {

	group.GET("/mentors", generalRateLimiter.Middleware(), middleware.TokenAuthMiddleware(cfg.Auth.MentorsAPIToken), mentorHandler.GetPublicMentors)
	group.GET("/mentor/:id", generalRateLimiter.Middleware(), middleware.TokenAuthMiddleware(cfg.Auth.MentorsAPIToken), mentorHandler.GetPublicMentorByID)
	group.POST("/internal/mentors", generalRateLimiter.Middleware(), middleware.InternalAPIAuthMiddleware(cfg.Auth.InternalMentorsAPI), mentorHandler.GetInternalMentors)
	group.POST("/contact-mentor", contactRateLimiter.Middleware(), middleware.BodySizeLimitMiddleware(100*1024), contactHandler.ContactMentor)
	group.POST("/register-mentor", registrationRateLimiter.Middleware(), middleware.BodySizeLimitMiddleware(10*1024*1024), registrationHandler.RegisterMentor)
	// SECURITY: /logs appends to a file on disk, so it is gated behind the
	// internal API token (server-to-server only, same as /internal/mentors) to
	// prevent unauthenticated log injection / disk-fill DoS.
	group.POST("/logs", generalRateLimiter.Middleware(), middleware.InternalAPIAuthMiddleware(cfg.Auth.InternalMentorsAPI), middleware.BodySizeLimitMiddleware(1*1024*1024), logsHandler.ReceiveFrontendLogs)

	// Mentor email confirmation (public, draft-status registration flow).
	// The resend endpoint issues fresh tokens and emails - login-tier limits.
	group.POST("/mentors/confirm", contactRateLimiter.Middleware(), middleware.BodySizeLimitMiddleware(10*1024), mentorConfirmationHandler.Confirm)
	group.POST("/mentors/confirm/resend", confirmResendRateLimiter.Middleware(), middleware.BodySizeLimitMiddleware(10*1024), mentorConfirmationHandler.Resend)

	// Review routes (public - uses captcha for protection)
	group.GET("/reviews/:requestId/check", generalRateLimiter.Middleware(), reviewHandler.CheckReview)
	group.POST("/reviews/:requestId", contactRateLimiter.Middleware(), middleware.BodySizeLimitMiddleware(100*1024), reviewHandler.SubmitReview)

	// getmentor.dev migration opt-ins (public - uses captcha for protection, D22)
	group.POST("/migration/intents", contactRateLimiter.Middleware(), middleware.BodySizeLimitMiddleware(10*1024), migrationIntentHandler.ScheduleMigration)
}

// registerMentorAdminRoutes registers mentor admin routes for authentication, request management, and profile
func registerMentorAdminRoutes(
	router *gin.Engine,
	cfg *config.Config,
	authRateLimiter *middleware.RateLimiter,
	profileRateLimiter *middleware.RateLimiter,
	mentorAuthHandler *handlers.MentorAuthHandler,
	mentorRequestsHandler *handlers.MentorRequestsHandler,
	mentorProfileHandler *handlers.MentorProfileHandler,
	tokenManager *jwt.TokenManager,
) {
	// Skip mentor admin routes if JWT is not configured
	if tokenManager == nil {
		logger.Warn("Mentor admin routes disabled: JWT_SECRET not configured")
		return
	}

	// Authentication routes (public)
	auth := router.Group("/api/v1/auth/mentor")
	auth.POST("/request-login", authRateLimiter.Middleware(), mentorAuthHandler.RequestLogin)
	auth.POST("/verify", mentorAuthHandler.VerifyLogin)
	auth.POST("/logout", mentorAuthHandler.Logout)
	auth.GET("/session", middleware.MentorSessionMiddleware(tokenManager, cfg.MentorSession.CookieDomain, cfg.MentorSession.CookieSecure), mentorAuthHandler.GetSession)

	// Mentor admin routes (protected)
	mentor := router.Group("/api/v1/mentor")
	mentor.Use(middleware.MentorSessionMiddleware(tokenManager, cfg.MentorSession.CookieDomain, cfg.MentorSession.CookieSecure))

	// Request management routes
	mentor.GET("/requests", mentorRequestsHandler.GetRequests)
	mentor.GET("/requests/:id", mentorRequestsHandler.GetRequestByID)
	mentor.POST("/requests/:id/status", mentorRequestsHandler.UpdateStatus)
	mentor.POST("/requests/:id/decline", mentorRequestsHandler.DeclineRequest)

	// Profile routes
	mentor.GET("/profile", mentorProfileHandler.GetProfile)
	mentor.POST("/profile", profileRateLimiter.Middleware(), mentorProfileHandler.UpdateProfile)
	mentor.POST("/profile/status", profileRateLimiter.Middleware(), mentorProfileHandler.UpdateProfileStatus)
	mentor.POST("/profile/submit", profileRateLimiter.Middleware(), mentorProfileHandler.SubmitProfile)
	mentor.POST("/profile/picture", profileRateLimiter.Middleware(), middleware.BodySizeLimitMiddleware(10*1024*1024), mentorProfileHandler.UploadPicture)
}

// registerAdminModerationRoutes registers moderator/admin web routes.
func registerAdminModerationRoutes(
	router *gin.Engine,
	cfg *config.Config,
	authRateLimiter *middleware.RateLimiter,
	profileRateLimiter *middleware.RateLimiter,
	adminAuthHandler *handlers.AdminAuthHandler,
	adminMentorsHandler *handlers.AdminMentorsHandler,
	tokenManager *jwt.TokenManager,
) {

	if tokenManager == nil {
		logger.Warn("Admin moderation routes disabled: JWT_SECRET not configured")
		return
	}

	auth := router.Group("/api/v1/auth/admin")
	auth.POST("/request-login", authRateLimiter.Middleware(), adminAuthHandler.RequestLogin)
	auth.POST("/verify", adminAuthHandler.VerifyLogin)
	auth.POST("/logout", adminAuthHandler.Logout)
	auth.GET("/session", middleware.AdminSessionMiddleware(tokenManager, cfg.MentorSession.CookieDomain, cfg.MentorSession.CookieSecure), adminAuthHandler.GetSession)

	admin := router.Group("/api/v1/admin")
	admin.Use(middleware.AdminSessionMiddleware(tokenManager, cfg.MentorSession.CookieDomain, cfg.MentorSession.CookieSecure))
	admin.GET("/mentors", adminMentorsHandler.ListMentors)
	admin.GET("/mentors/:id", adminMentorsHandler.GetMentor)
	admin.POST("/mentors/:id", profileRateLimiter.Middleware(), adminMentorsHandler.UpdateMentor)
	admin.POST("/mentors/:id/approve", adminMentorsHandler.ApproveMentor)
	admin.POST("/mentors/:id/decline", adminMentorsHandler.DeclineMentor)
	admin.POST("/mentors/:id/return", adminMentorsHandler.ReturnMentor)
	admin.POST("/mentors/:id/status", adminMentorsHandler.UpdateMentorStatus)
	admin.POST("/mentors/:id/picture", profileRateLimiter.Middleware(), middleware.BodySizeLimitMiddleware(10*1024*1024), adminMentorsHandler.UploadMentorPicture)
}

func main() { //nolint:gocyclo
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	err = logger.Initialize(logger.Config{
		Level:       cfg.Logging.Level,
		LogDir:      cfg.Logging.Dir,
		Environment: cfg.Server.AppEnv,
		ServiceName: cfg.Observability.ServiceName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting OpenMentor API",
		zap.String("version", "1.0.0"),
		zap.String("environment", cfg.Server.AppEnv),
	)

	// Initialize distributed tracing
	tracerShutdown, err := tracing.InitTracer(
		cfg.Observability.ServiceName,
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

	// Initialize continuous profiling
	profilerStop, err := profiling.InitProfiler(
		cfg.Profiling,
		cfg.Observability.ServiceName,
		cfg.Observability.ServiceNamespace,
		cfg.Observability.ServiceVersion,
		cfg.Observability.ServiceInstanceID,
		cfg.Server.AppEnv,
	)
	if err != nil {
		logger.Fatal("Failed to initialize profiler", zap.Error(err))
	}
	defer profilerStop()

	// Initialize PostHog error tracking (uses same PostHog project as the frontend)
	errortracking.Init(
		cfg.PostHog.APIKey,
		cfg.PostHog.Host,
		cfg.Server.AppEnv,
		cfg.Observability.ServiceName,
		cfg.Observability.ServiceVersion,
	)
	defer errortracking.Close()

	// Initialize metrics with service name from config
	metrics.Init(cfg.Observability.ServiceName)

	// Start infrastructure metrics collection
	metrics.RecordInfrastructureMetrics()

	// Initialize PostgreSQL connection pool
	pool, err := db.NewPool(context.Background(), cfg.Database)
	if err != nil {
		logger.Fatal("Failed to initialize database connection pool", zap.Error(err))
	}
	defer pool.Close()

	// NOTE: Database migrations are now run separately via the migrate command
	// Run migrations before starting the app: ./migrate or docker-compose run migrate

	// Initialize S3-compatible object storage client (profile pictures)
	var storageClient *s3storage.StorageClient
	if cfg.S3Storage.AccessKeyID != "" && cfg.S3Storage.SecretAccessKey != "" {
		storageClient, err = s3storage.NewStorageClient(
			cfg.S3Storage.AccessKeyID,
			cfg.S3Storage.SecretAccessKey,
			cfg.S3Storage.BucketName,
			cfg.S3Storage.Endpoint,
			cfg.S3Storage.Region,
		)
		if err != nil {
			logger.Fatal("Failed to initialize S3 storage client", zap.Error(err))
		}
	}

	// Initialize repositories (needed for cache fetchers)
	// First create caches with dummy fetchers, then update with real fetchers
	mentorCache := cache.NewMentorCache(
		func(ctx context.Context) ([]*models.Mentor, error) {
			// This fetcher will be replaced after repository is fully initialized
			return []*models.Mentor{}, nil
		},
		func(ctx context.Context, slug string) (*models.Mentor, error) {
			// This fetcher will be replaced after repository is fully initialized
			return &models.Mentor{}, nil
		},
		cfg.Cache.MentorTTLSeconds,
	)
	tagsCache := cache.NewTagsCache(
		func(ctx context.Context) (map[string]string, error) {
			// This fetcher will be replaced after repository is fully initialized
			return make(map[string]string), nil
		},
	)

	// Initialize repositories with pool and caches
	mentorRepo := repository.NewMentorRepository(pool, mentorCache, tagsCache, cfg.Cache.DisableMentorsCache)
	moderatorRepo := repository.NewModeratorRepository(pool)
	clientRequestRepo := repository.NewClientRequestRepository(pool)

	// Now update cache with actual fetcher functions from repository
	mentorCache = cache.NewMentorCache(
		mentorRepo.FetchAllMentorsFromDB,
		mentorRepo.FetchSingleMentorFromDB,
		cfg.Cache.MentorTTLSeconds,
	)
	tagsCache = cache.NewTagsCache(mentorRepo.FetchAllTagsFromDB)

	// Re-initialize repository with updated caches
	mentorRepo = repository.NewMentorRepository(pool, mentorCache, tagsCache, cfg.Cache.DisableMentorsCache)

	// Initialize mentor cache synchronously before accepting requests
	// This ensures the cache is populated before the container is marked as healthy
	if cfg.Cache.DisableMentorsCache {
		logger.Warn("Mentor cache is DISABLED - reading from database on every request (experimental feature)")
	} else {
		if err := mentorCache.Initialize(); err != nil {
			logger.Fatal("Failed to initialize mentor cache", zap.Error(err))
		}
	}

	// Initialize tags cache synchronously
	if err := tagsCache.Initialize(); err != nil {
		logger.Fatal("Failed to initialize tags cache", zap.Error(err))
	}

	// Initialize HTTP client for external API calls
	httpClient := httpclient.NewStandardClient()
	analyticsTracker := analytics.NewTracker(&analytics.Config{
		Provider:               cfg.ResolvedAnalyticsProvider(),
		SourceSystem:           "api",
		Environment:            cfg.Server.AppEnv,
		EventVersion:           cfg.ResolvedAnalyticsEventVersion(),
		PostHogEnabled:         cfg.PostHog.Enabled,
		PostHogAPIKey:          cfg.PostHog.APIKey,
		PostHogHost:            cfg.PostHog.Host,
		PostHogCaptureEndpoint: cfg.PostHog.CaptureEndpoint,
		PostHogDisableGeoIP:    cfg.PostHog.DisableGeoIP,
	})

	// Initialize repositories for reviews
	reviewRepo := repository.NewReviewRepository(pool)
	migrationIntentRepo := repository.NewMigrationIntentRepository(pool)

	// Initialize services
	mentorService := services.NewMentorService(mentorRepo, cfg)
	contactService := services.NewContactService(clientRequestRepo, mentorRepo, cfg, httpClient, analyticsTracker)
	profileService := services.NewProfileService(mentorRepo, storageClient, cfg, httpClient, analyticsTracker)
	registrationService := services.NewRegistrationService(mentorRepo, storageClient, cfg, httpClient, analyticsTracker)
	mentorAuthService := services.NewMentorAuthService(mentorRepo, cfg, httpClient, analyticsTracker)
	adminAuthService := services.NewAdminAuthService(moderatorRepo, cfg, httpClient, analyticsTracker)
	mentorRequestsService := services.NewMentorRequestsService(clientRequestRepo, cfg, httpClient, analyticsTracker)
	reviewService := services.NewReviewService(reviewRepo, cfg, httpClient, analyticsTracker)
	adminMentorsService := services.NewAdminMentorsService(mentorRepo, profileService, cfg, httpClient, analyticsTracker)
	migrationIntentService := services.NewMigrationIntentService(migrationIntentRepo, cfg, httpClient, analyticsTracker)
	mentorConfirmationService := services.NewMentorConfirmationService(mentorRepo, cfg, httpClient, analyticsTracker)

	// Initialize handlers
	mentorHandler := handlers.NewMentorHandler(mentorService, cfg.Server.BaseURL)
	contactHandler := handlers.NewContactHandler(contactService)
	registrationHandler := handlers.NewRegistrationHandler(registrationService)
	reviewHandler := handlers.NewReviewHandler(reviewService)
	migrationIntentHandler := handlers.NewMigrationIntentHandler(migrationIntentService)
	mentorConfirmationHandler := handlers.NewMentorConfirmationHandler(mentorConfirmationService)
	// Health check: If cache is disabled, always return true for cache readiness
	cacheReadyFunc := mentorCache.IsReady
	if cfg.Cache.DisableMentorsCache {
		cacheReadyFunc = func() bool { return true }
	}
	healthHandler := handlers.NewHealthHandler(pool, cacheReadyFunc)
	logsHandler := handlers.NewLogsHandler(cfg.Logging.Dir)
	mentorAuthHandler := handlers.NewMentorAuthHandler(mentorAuthService)
	adminAuthHandler := handlers.NewAdminAuthHandler(adminAuthService)
	mentorRequestsHandler := handlers.NewMentorRequestsHandler(mentorRequestsService)
	mentorProfileHandler := handlers.NewMentorProfileHandler(mentorService, profileService)
	adminMentorsHandler := handlers.NewAdminMentorsHandler(adminMentorsService)

	// Set up Gin router
	gin.SetMode(cfg.Server.GinMode)
	router := gin.New()

	// SECURITY: Only resolve the client IP from X-Forwarded-For for these
	// trusted hops (private/loopback by default — the API is internal-only,
	// reached via Traefik/BFF). Without this Gin trusts every proxy and honors
	// a client-supplied X-Forwarded-For, which would let a caller spoof the IP
	// the rate limiter keys on.
	if err := router.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		logger.Fatal("Failed to set trusted proxies", zap.Error(err))
	}

	// Global middleware
	router.Use(middleware.RecoveryMiddleware())
	router.Use(otelgin.Middleware(cfg.Observability.ServiceName)) // OpenTelemetry tracing
	router.Use(middleware.ObservabilityMiddleware())
	router.Use(middleware.SecurityHeadersMiddleware())

	// CORS configuration - SECURITY: Only allow specific origins
	allowedOrigins := cfg.Server.AllowedOrigins
	// Allow localhost in development
	if cfg.IsDevelopment() {
		allowedOrigins = append(allowedOrigins, "http://localhost:3000", "http://127.0.0.1:3000")
	}

	router.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "mentors_api_auth_token", "x-internal-mentors-api-auth-token", "X-Webhook-Secret", "X-Mentor-ID", "X-Auth-Token", "traceparent", "tracestate"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true, // Required for mentor session cookies
		MaxAge:           12 * time.Hour,
	}))

	// SECURITY: Rate limiters to prevent abuse and DoS attacks
	// Different limits for different endpoint types
	generalRateLimiter := middleware.NewRateLimiter(100, 200)         // 100 req/sec, burst of 200
	contactRateLimiter := middleware.NewRateLimiter(5, 10)            // 5 req/sec, burst of 10 (prevent spam)
	profileRateLimiter := middleware.NewRateLimiter(10, 20)           // 10 req/sec, burst of 20
	registrationRateLimiter := middleware.NewRateLimiter(0.00667, 3)  // 2 req/5min (0.00667 req/sec), burst of 3
	mentorAuthRateLimiter := middleware.NewRateLimiter(0.00667, 2)    // 2 req/5min (0.00667 req/sec), burst of 2 (login abuse prevention)
	adminAuthRateLimiter := middleware.NewRateLimiter(0.00667, 2)     // 2 req/5min (0.00667 req/sec), burst of 2 (login abuse prevention)
	confirmResendRateLimiter := middleware.NewRateLimiter(0.00667, 2) // 2 req/5min, burst of 2 (confirmation resend abuse prevention, login tier)

	// API routes
	api := router.Group("/api")
	// Utility endpoints (not versioned - operational endpoints)
	api.GET("/healthcheck", generalRateLimiter.Middleware(), healthHandler.Healthcheck)
	api.GET("/metrics", generalRateLimiter.Middleware(), gin.WrapH(promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{})))

	// API v1 routes
	// SECURITY: Apply body size limits to prevent DoS attacks
	v1 := router.Group("/api/v1")
	registerAPIRoutes(v1, cfg, generalRateLimiter, contactRateLimiter, registrationRateLimiter, confirmResendRateLimiter,
		mentorHandler, contactHandler, logsHandler, registrationHandler, reviewHandler, migrationIntentHandler,
		mentorConfirmationHandler)

	// Mentor admin routes (authentication, request management, and profile)
	registerMentorAdminRoutes(router, cfg, mentorAuthRateLimiter, profileRateLimiter, mentorAuthHandler, mentorRequestsHandler, mentorProfileHandler, mentorAuthService.GetTokenManager())

	// Moderator/Admin web moderation routes
	registerAdminModerationRoutes(router, cfg, adminAuthRateLimiter, profileRateLimiter, adminAuthHandler, adminMentorsHandler, adminAuthService.GetTokenManager())

	// Create HTTP server
	// SECURITY: Bind to all interfaces for Docker Compose networking
	// Network isolation is enforced by Docker Compose (backend has no public ports)
	// In Docker Compose, frontend container needs to access backend via service name
	srv := &http.Server{
		Addr:              "0.0.0.0:" + cfg.Server.Port,
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // SECURITY: 1 MB max header size
	}

	// Start server in a goroutine
	go func() {
		logger.Info("Server started", zap.String("port", cfg.Server.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed to start", zap.Error(err))
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited")
}
