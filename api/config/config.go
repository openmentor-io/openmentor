package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

const (
	analyticsProviderNone    = "none"
	analyticsProviderPosthog = "posthog"
	defaultEventVersion      = "v1"
)

// Config holds all application configuration
//
//nolint:govet // Field alignment optimization would reduce readability
type Config struct {
	Server        ServerConfig
	Database      DatabaseConfig
	S3Storage     S3StorageConfig
	Auth          AuthConfig
	Analytics     AnalyticsConfig
	PostHog       PostHogConfig
	Turnstile     TurnstileConfig
	EventTriggers EventTriggerFunctionsConfig
	Logging       LoggingConfig
	Observability ObservabilityConfig
	Profiling     ProfilingConfig
	MentorSession MentorSessionConfig
	Worker        WorkerConfig
	Email         EmailConfig
	Cutout        CutoutConfig
}

// CutoutConfig configures the photo background-removal sidecar (rembg) used
// to generate the catalog's "hero" cut-out cards. An empty ServiceURL
// disables the feature: uploads fall back to the border-luminance
// photo-style classifier (pkg/imageclass).
type CutoutConfig struct {
	ServiceURL     string // CUTOUT_SERVICE_URL, e.g. http://rembg:7000
	Model          string // CUTOUT_MODEL, rembg model name
	TimeoutSeconds int    // CUTOUT_TIMEOUT_SECONDS
}

type ServerConfig struct {
	Port           string
	GinMode        string
	AppEnv         string
	BaseURL        string
	AllowedOrigins []string
	// TrustedProxies is the CIDR/IP allowlist Gin uses to resolve the real
	// client IP from X-Forwarded-For. Only these hops may set the forwarded
	// address; anything else is ignored (prevents X-Forwarded-For spoofing of
	// the rate limiter). Defaults to private/loopback ranges because the API
	// is only reachable over the internal Docker network via Traefik/BFF.
	TrustedProxies []string
}

type DatabaseConfig struct {
	URL         string
	MaxConns    int32
	MinConns    int32
	WorkOffline bool
}

// S3StorageConfig configures the S3-compatible object storage used for
// profile pictures. Any S3-compatible provider works (AWS S3, Cloudflare R2,
// Backblaze B2, ...) — select the provider via Endpoint and Region.
type S3StorageConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	Endpoint        string
	Region          string
}

type AuthConfig struct {
	MentorsAPIToken    string
	InternalMentorsAPI string
}

type AnalyticsConfig struct {
	Provider     string
	EventVersion string
}

type PostHogConfig struct {
	Enabled         bool
	APIKey          string
	Host            string
	CaptureEndpoint string
	DisableGeoIP    bool
}

type TurnstileConfig struct {
	SecretKey string
}

type EventTriggerFunctionsConfig struct {
	MentorCreatedTriggerURL          string
	MentorUpdatedTriggerURL          string
	MentorRequestCreatedTriggerURL   string
	MentorLoginEmailTriggerURL       string
	ModeratorLoginEmailTriggerURL    string
	MentorModerationTriggerURL       string
	RequestProcessFinishedTriggerURL string
	ReviewCreatedTriggerURL          string
}

// derivedMentorTriggerURL rewrites MENTOR_CREATED_TRIGGER_URL
// (…/jobs/new-mentor-watcher?mentorId=) to point at a sibling worker job
// with the same ?mentorId= calling convention. This keeps the env contract
// unchanged: new jobs on the same worker need no new env vars. Returns ""
// (trigger skipped) when MENTOR_CREATED_TRIGGER_URL is unset.
func (c EventTriggerFunctionsConfig) derivedMentorTriggerURL(job string) string {
	if !strings.Contains(c.MentorCreatedTriggerURL, "new-mentor-watcher") {
		return ""
	}
	return strings.Replace(c.MentorCreatedTriggerURL, "new-mentor-watcher", job, 1)
}

// MentorConfirmedTriggerURL is the worker trigger fired when a mentor
// confirms their email (or resubmits a draft profile): the worker sends the
// "in review" mentor email plus the moderator notification.
func (c EventTriggerFunctionsConfig) MentorConfirmedTriggerURL() string {
	return c.derivedMentorTriggerURL("mentor-confirmed")
}

// MentorConfirmEmailTriggerURL is the worker trigger that (re)sends the
// mentor-confirm-email using the confirmation token stored on the row.
func (c EventTriggerFunctionsConfig) MentorConfirmEmailTriggerURL() string {
	return c.derivedMentorTriggerURL("mentor-confirm-email")
}

type LoggingConfig struct {
	Level string
	Dir   string
}

type ObservabilityConfig struct {
	AlloyEndpoint     string
	ServiceName       string
	ServiceNamespace  string
	ServiceVersion    string
	ServiceInstanceID string
}

type ProfilingConfig struct {
	Enabled               bool
	Endpoint              string
	AppName               string
	SampleTypes           string
	UploadIntervalSeconds int
}

type MentorSessionConfig struct {
	JWTSecret            string
	JWTIssuer            string
	SessionTTLHours      int
	LoginTokenTTLMinutes int
	CookieDomain         string
	CookieSecure         bool
}

// WorkerConfig configures the background worker binary (cmd/worker).
// The worker runs as a separate container from the same image (decision D6):
// an internal HTTP server for async event triggers plus a cron scheduler
// for daily jobs, with its own smaller DB pool.
type WorkerConfig struct {
	Port        string // WORKER_PORT: internal HTTP port (not publicly exposed)
	DBMaxConns  int32  // WORKER_DB_MAX_CONNS: worker's own, smaller pool cap
	CronEnabled bool   // WORKER_CRON_ENABLED: master switch for scheduled jobs
	AuthToken   string // WORKER_AUTH_TOKEN: shared secret for X-Worker-Token
	ServiceName string // O11Y_WORKER_SERVICE_NAME: observability identity

	// HighlightedMentors is the raw HIGHLIGHTED_MENTORS value: a
	// comma-separated list of mentor ids the randomize-sort-order job pins
	// to the top of the catalog. Same env name and comma-split semantics as
	// the func app (openmentor-func/randomize-sort-order/index.ts).
	HighlightedMentors string
}

// EmailConfig configures the transactional email layer (pkg/email).
// SES env var names match the func app (SES_REGION, SES_ACCESS_KEY_ID,
// SES_SECRET_ACCESS_KEY, SES_ENDPOINT, DEV_EMAIL_OVERRIDE, MODERATORS_EMAIL).
type EmailConfig struct {
	SESRegion          string
	SESAccessKeyID     string
	SESSecretAccessKey string
	SESEndpoint        string // optional: SESv2-compatible endpoint override
	DevEmailOverride   string // non-production: reroute ALL recipients here
	ModeratorsEmail    string // moderators mailbox for notification emails
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("PORT", "8081")
	v.SetDefault("GIN_MODE", "release")
	v.SetDefault("APP_ENV", "production")
	v.SetDefault("BASE_URL", "https://openmentor.io")
	v.SetDefault("ALLOWED_CORS_ORIGINS", "https://openmentor.io,https://www.openmentor.io")
	// Private + loopback ranges: the API sits behind Traefik/BFF on the
	// internal Docker network and is never reached directly from the internet.
	v.SetDefault("TRUSTED_PROXIES", "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16")
	v.SetDefault("LOG_LEVEL", "info")
	v.SetDefault("LOG_DIR", "/app/logs")
	v.SetDefault("O11Y_EXPORTER_ENDPOINT", "alloy:4318") // OTLP over HTTP
	v.SetDefault("O11Y_BE_SERVICE_NAME", "openmentor-api")
	v.SetDefault("O11Y_SERVICE_NAMESPACE", "openmentor-io")
	v.SetDefault("O11Y_BE_SERVICE_VERSION", "1.0.0")
	v.SetDefault("O11Y_PROFILING_ENABLED", false)
	v.SetDefault("O11Y_PROFILING_APP_NAME", "openmentor-api")
	v.SetDefault("O11Y_PROFILING_SAMPLE_TYPES", "cpu,alloc_space,alloc_objects,goroutines,mutex,block")
	v.SetDefault("O11Y_PROFILING_UPLOAD_INTERVAL_SECONDS", 15)
	v.SetDefault("ANALYTICS_PROVIDER", "")
	v.SetDefault("ANALYTICS_EVENT_VERSION", defaultEventVersion)
	v.SetDefault("POSTHOG_ENABLED", false)
	v.SetDefault("POSTHOG_HOST", "https://eu.i.posthog.com")
	v.SetDefault("POSTHOG_DISABLE_GEOIP", true)

	// Worker defaults (background worker binary, cmd/worker)
	v.SetDefault("WORKER_PORT", "8090")
	v.SetDefault("WORKER_DB_MAX_CONNS", 5)
	v.SetDefault("WORKER_CRON_ENABLED", true)
	v.SetDefault("O11Y_WORKER_SERVICE_NAME", "openmentor-worker")

	// Email defaults
	v.SetDefault("MODERATORS_EMAIL", "moderators@openmentor.io")

	// Photo cutout defaults (feature off until CUTOUT_SERVICE_URL is set)
	v.SetDefault("CUTOUT_MODEL", "isnet-general-use")
	v.SetDefault("CUTOUT_TIMEOUT_SECONDS", 30)

	// Mentor session defaults
	v.SetDefault("JWT_ISSUER", "openmentor-api")
	v.SetDefault("SESSION_TTL_HOURS", 24)
	v.SetDefault("LOGIN_TOKEN_TTL_MINUTES", 15)
	v.SetDefault("COOKIE_DOMAIN", "")
	v.SetDefault("COOKIE_SECURE", true)

	// Automatically read environment variables
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Read from .env file if it exists
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	v.AddConfigPath("..")
	_ = v.ReadInConfig() //nolint:errcheck // Ignore error if .env file doesn't exist

	// Parse allowed CORS origins (comma-separated)
	allowedOrigins := []string{}
	originsStr := v.GetString("ALLOWED_CORS_ORIGINS")
	if originsStr != "" {
		for _, origin := range strings.Split(originsStr, ",") {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				allowedOrigins = append(allowedOrigins, origin)
			}
		}
	}

	// Parse trusted proxy CIDRs/IPs (comma-separated)
	trustedProxies := []string{}
	for _, p := range strings.Split(v.GetString("TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			trustedProxies = append(trustedProxies, p)
		}
	}

	analyticsProvider := strings.ToLower(strings.TrimSpace(v.GetString("ANALYTICS_PROVIDER")))
	analyticsEventVersion := strings.TrimSpace(v.GetString("ANALYTICS_EVENT_VERSION"))

	cfg := &Config{
		Server: ServerConfig{
			Port:           v.GetString("PORT"),
			GinMode:        v.GetString("GIN_MODE"),
			AppEnv:         v.GetString("APP_ENV"),
			BaseURL:        v.GetString("BASE_URL"),
			AllowedOrigins: allowedOrigins,
			TrustedProxies: trustedProxies,
		},
		Database: DatabaseConfig{
			URL:         v.GetString("DATABASE_URL"),
			MaxConns:    20,
			MinConns:    2,
			WorkOffline: v.GetBool("DB_WORK_OFFLINE"),
		},
		S3Storage: S3StorageConfig{
			AccessKeyID:     v.GetString("S3_STORAGE_ACCESS_KEY"),
			SecretAccessKey: v.GetString("S3_STORAGE_SECRET_KEY"),
			BucketName:      v.GetString("S3_STORAGE_BUCKET"),
			Endpoint:        v.GetString("S3_STORAGE_ENDPOINT"),
			Region:          v.GetString("S3_STORAGE_REGION"),
		},
		Auth: AuthConfig{
			MentorsAPIToken:    v.GetString("MENTORS_API_LIST_AUTH_TOKEN"),
			InternalMentorsAPI: v.GetString("INTERNAL_MENTORS_API"),
		},
		Analytics: AnalyticsConfig{
			Provider:     analyticsProvider,
			EventVersion: analyticsEventVersion,
		},
		PostHog: PostHogConfig{
			Enabled:         v.GetBool("POSTHOG_ENABLED"),
			APIKey:          v.GetString("POSTHOG_API_KEY"),
			Host:            v.GetString("POSTHOG_HOST"),
			CaptureEndpoint: v.GetString("POSTHOG_CAPTURE_ENDPOINT"),
			DisableGeoIP:    v.GetBool("POSTHOG_DISABLE_GEOIP"),
		},
		Turnstile: TurnstileConfig{
			SecretKey: v.GetString("TURNSTILE_SECRET_KEY"),
		},
		EventTriggers: EventTriggerFunctionsConfig{
			MentorCreatedTriggerURL:          v.GetString("MENTOR_CREATED_TRIGGER_URL"),
			MentorUpdatedTriggerURL:          v.GetString("MENTOR_UPDATED_TRIGGER_URL"),
			MentorRequestCreatedTriggerURL:   v.GetString("MENTOR_REQUEST_CREATED_TRIGGER_URL"),
			MentorLoginEmailTriggerURL:       v.GetString("MENTOR_LOGIN_EMAIL_TRIGGER_URL"),
			ModeratorLoginEmailTriggerURL:    v.GetString("MODERATOR_LOGIN_EMAIL_TRIGGER_URL"),
			MentorModerationTriggerURL:       v.GetString("MENTOR_MODERATION_TRIGGER_URL"),
			RequestProcessFinishedTriggerURL: v.GetString("REQUEST_PROCESS_FINISHED_TRIGGER_URL"),
			ReviewCreatedTriggerURL:          v.GetString("REVIEW_CREATED_TRIGGER_URL"),
		},
		Logging: LoggingConfig{
			Level: v.GetString("LOG_LEVEL"),
			Dir:   v.GetString("LOG_DIR"),
		},
		Observability: ObservabilityConfig{
			AlloyEndpoint:     v.GetString("O11Y_EXPORTER_ENDPOINT"),
			ServiceName:       v.GetString("O11Y_BE_SERVICE_NAME"),
			ServiceNamespace:  v.GetString("O11Y_SERVICE_NAMESPACE"),
			ServiceVersion:    v.GetString("O11Y_BE_SERVICE_VERSION"),
			ServiceInstanceID: v.GetString("SERVICE_INSTANCE_ID"),
		},
		Profiling: ProfilingConfig{
			Enabled:               v.GetBool("O11Y_PROFILING_ENABLED"),
			Endpoint:              v.GetString("O11Y_PROFILING_ENDPOINT"),
			AppName:               v.GetString("O11Y_PROFILING_APP_NAME"),
			SampleTypes:           v.GetString("O11Y_PROFILING_SAMPLE_TYPES"),
			UploadIntervalSeconds: v.GetInt("O11Y_PROFILING_UPLOAD_INTERVAL_SECONDS"),
		},
		MentorSession: MentorSessionConfig{
			JWTSecret:            v.GetString("JWT_SECRET"),
			JWTIssuer:            v.GetString("JWT_ISSUER"),
			SessionTTLHours:      v.GetInt("SESSION_TTL_HOURS"),
			LoginTokenTTLMinutes: v.GetInt("LOGIN_TOKEN_TTL_MINUTES"),
			CookieDomain:         v.GetString("COOKIE_DOMAIN"),
			CookieSecure:         v.GetBool("COOKIE_SECURE"),
		},
		Worker: WorkerConfig{
			Port:        v.GetString("WORKER_PORT"),
			DBMaxConns:  v.GetInt32("WORKER_DB_MAX_CONNS"),
			CronEnabled: v.GetBool("WORKER_CRON_ENABLED"),
			AuthToken:   v.GetString("WORKER_AUTH_TOKEN"),
			ServiceName: v.GetString("O11Y_WORKER_SERVICE_NAME"),

			HighlightedMentors: v.GetString("HIGHLIGHTED_MENTORS"),
		},
		Email: EmailConfig{
			SESRegion:          v.GetString("SES_REGION"),
			SESAccessKeyID:     v.GetString("SES_ACCESS_KEY_ID"),
			SESSecretAccessKey: v.GetString("SES_SECRET_ACCESS_KEY"),
			SESEndpoint:        v.GetString("SES_ENDPOINT"),
			DevEmailOverride:   v.GetString("DEV_EMAIL_OVERRIDE"),
			ModeratorsEmail:    v.GetString("MODERATORS_EMAIL"),
		},
		Cutout: CutoutConfig{
			ServiceURL:     v.GetString("CUTOUT_SERVICE_URL"),
			Model:          v.GetString("CUTOUT_MODEL"),
			TimeoutSeconds: v.GetInt("CUTOUT_TIMEOUT_SECONDS"),
		},
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks if required configuration values are set
func (c *Config) Validate() error {
	if err := c.validateDatabaseConfig(); err != nil {
		return err
	}
	if err := c.validateAuthConfig(); err != nil {
		return err
	}
	if err := c.validateAnalyticsConfig(); err != nil {
		return err
	}
	if err := c.validateTurnstileConfig(); err != nil {
		return err
	}
	if err := c.validateServerConfig(); err != nil {
		return err
	}
	if err := c.validateSessionConfig(); err != nil {
		return err
	}
	if err := c.validateWorkerConfig(); err != nil {
		return err
	}
	return c.validateProfilingConfig()
}

// minJWTSecretLength is the minimum accepted JWT_SECRET length in bytes. A
// short secret makes the HS256 signature brute-forceable offline, which would
// let an attacker forge mentor AND moderator sessions (one secret signs both).
const minJWTSecretLength = 32

// validateSessionConfig enforces JWT_SECRET presence/length in production.
// In non-production an empty secret is allowed (auth routes self-disable with
// a warning), but a set-but-too-short secret is always rejected.
func (c *Config) validateSessionConfig() error {
	secret := c.MentorSession.JWTSecret
	if c.IsProduction() && secret == "" {
		return fmt.Errorf("JWT_SECRET is required in production")
	}
	if secret != "" && len(secret) < minJWTSecretLength {
		return fmt.Errorf("JWT_SECRET must be at least %d characters", minJWTSecretLength)
	}
	return nil
}

// validateWorkerConfig enforces WORKER_AUTH_TOKEN in production so the
// worker's /jobs/* endpoints (email dispatch, moderation actions) can never
// fail open to an unauthenticated caller on the internal network.
func (c *Config) validateWorkerConfig() error {
	if c.IsProduction() && c.Worker.AuthToken == "" {
		return fmt.Errorf("WORKER_AUTH_TOKEN is required in production")
	}
	return nil
}

func (c *Config) validateDatabaseConfig() error {
	if !c.Database.WorkOffline && c.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL is required when not in offline mode")
	}
	return nil
}

func (c *Config) validateAuthConfig() error {
	if c.Auth.InternalMentorsAPI == "" {
		return fmt.Errorf("INTERNAL_MENTORS_API is required")
	}
	if c.Auth.MentorsAPIToken == "" {
		return fmt.Errorf("MENTORS_API_LIST_AUTH_TOKEN is required")
	}
	return nil
}

func (c *Config) validateAnalyticsConfig() error {
	provider := c.ResolvedAnalyticsProvider()
	switch provider {
	case analyticsProviderNone, analyticsProviderPosthog:
	default:
		return fmt.Errorf("ANALYTICS_PROVIDER must be one of: none, posthog")
	}

	if provider != analyticsProviderPosthog {
		return nil
	}

	if strings.TrimSpace(c.PostHog.APIKey) == "" {
		return fmt.Errorf("POSTHOG_API_KEY is required when ANALYTICS_PROVIDER=%s", provider)
	}
	if strings.TrimSpace(c.PostHog.CaptureEndpoint) == "" && strings.TrimSpace(c.PostHog.Host) == "" {
		return fmt.Errorf("POSTHOG_HOST or POSTHOG_CAPTURE_ENDPOINT is required when ANALYTICS_PROVIDER=%s", provider)
	}

	return nil
}

func (c *Config) validateTurnstileConfig() error {
	if c.Turnstile.SecretKey == "" {
		return fmt.Errorf("TURNSTILE_SECRET_KEY is required")
	}
	return nil
}

func (c *Config) validateServerConfig() error {
	if c.Server.Port == "" {
		return fmt.Errorf("PORT is required")
	}
	if c.Server.BaseURL == "" {
		return fmt.Errorf("BASE_URL is required")
	}
	if len(c.Server.AllowedOrigins) == 0 {
		return fmt.Errorf("ALLOWED_CORS_ORIGINS is required")
	}
	// A wildcard origin combined with AllowCredentials:true (see main.go)
	// would let any site make credentialed cross-origin requests. Reject it.
	for _, origin := range c.Server.AllowedOrigins {
		if origin == "*" {
			return fmt.Errorf("ALLOWED_CORS_ORIGINS must not contain '*' (credentials are enabled)")
		}
	}
	return nil
}

func (c *Config) validateProfilingConfig() error {
	if c.Profiling.Enabled && c.Profiling.Endpoint == "" {
		return fmt.Errorf("O11Y_PROFILING_ENDPOINT is required when profiling is enabled")
	}
	return nil
}

// ResolvedAnalyticsProvider returns the normalized provider, falling back to
// POSTHOG_ENABLED when ANALYTICS_PROVIDER is unset (default: none).
func (c *Config) ResolvedAnalyticsProvider() string {
	provider := strings.ToLower(strings.TrimSpace(c.Analytics.Provider))
	if provider != "" {
		return provider
	}

	if c.PostHog.Enabled {
		return analyticsProviderPosthog
	}
	return analyticsProviderNone
}

// ResolvedAnalyticsEventVersion returns the analytics event version (default: v1).
func (c *Config) ResolvedAnalyticsEventVersion() string {
	eventVersion := strings.TrimSpace(c.Analytics.EventVersion)
	if eventVersion != "" {
		return eventVersion
	}
	return defaultEventVersion
}

// IsDevelopment returns true if running in development mode
func (c *Config) IsDevelopment() bool {
	return c.Server.AppEnv == "development" || c.Server.GinMode == "debug"
}

// IsProduction returns true if running in production mode
func (c *Config) IsProduction() bool {
	return c.Server.AppEnv == "production"
}
