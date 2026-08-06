package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"github.com/openmentor-io/openmentor/api/pkg/safeurl"
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
	Review        ReviewConfig
	EventTriggers EventTriggerFunctionsConfig
	Logging       LoggingConfig
	Observability ObservabilityConfig
	Profiling     ProfilingConfig
	MentorSession MentorSessionConfig
	Worker        WorkerConfig
	Email         EmailConfig
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

// TurnstileConfig configures the Cloudflare Turnstile captcha that gates every
// public write. ExpectedHostname/ExpectedAction are optional: siteverify echoes
// the hostname the widget was solved on and the action it was rendered with, and
// pinning them is what stops a token farmed on an attacker's own page — or
// harvested from a different form — being replayed here. Empty means "accept
// whatever Cloudflare reports", which is the pre-existing behavior.
type TurnstileConfig struct {
	SecretKey        string
	ExpectedHostname string
	ExpectedAction   string
}

// ReviewConfig holds the H4 dual-read switch.
type ReviewConfig struct {
	// LegacyRequestIDLinksEnabled keeps the pre-H4 review endpoints alive:
	// GET /api/v1/reviews/:requestId/check and POST /api/v1/reviews/:requestId,
	// which authorize on client_requests.id itself.
	//
	// It defaults to TRUE because turning it off breaks every "leave a review"
	// link already sitting in a mentee's inbox. Flipping it to false is the H4
	// CUTOVER and is an operator decision gated on
	// openmentor_review_legacy_link_uses_total having been zero for longer than
	// reviewtoken.TTL — see
	// docs/runbooks/audit-2026-08/review-capability-cutover.md.
	LegacyRequestIDLinksEnabled bool
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
// mentor-confirm-email. Since D57 the token is hashed on the row, so the caller
// POSTs the confirm URL in the payload rather than letting the job rebuild it.
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

	// ProfilePurgeCron is the schedule of the purge-deleted-profiles job
	// (WORKER_PROFILE_PURGE_CRON), in the same 6-field seconds-first NCRONTAB
	// syntax as every other entry in Handlers.CronJobs. Nightly by default: the
	// job is cheap when nothing is due, and a daily pass means a profile is
	// erased within a day of its retention window closing rather than up to a
	// month later.
	ProfilePurgeCron string

	// ProfilePurgeRetentionDays is how long a deleted profile is kept before
	// the purge erases it and everything attached to it
	// (WORKER_PROFILE_PURGE_RETENTION_DAYS). This is the ONLY window in which a
	// deletion can still be undone by an admin, so it is a product decision as
	// much as an operational one.
	//
	// 0 is rejected, not treated as "purge immediately": an env var that fails
	// to parse reads as 0, and a typo must not silently mean "erase every
	// deleted profile on the next pass".
	ProfilePurgeRetentionDays int
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
	DiscordInviteLink  string // invite URL for the private mentors' Discord
}

// Binary names one of the three binaries built from this module. Each has a
// DIFFERENT production configuration contract, and conflating them is a live
// outage this repo has already had: a57aec2 had to hand S3 credentials to
// cmd/migrate and cmd/worker — neither of which touches object storage — purely
// because all three called one shared Validate() that had grown an S3 check.
// The deploy that discovered this served 404s until the credentials were added.
type Binary string

const (
	// BinaryAPI is cmd/api: the public HTTP surface.
	BinaryAPI Binary = "api"
	// BinaryWorker is cmd/worker: cron plus the internal job endpoints.
	BinaryWorker Binary = "worker"
	// BinaryMigrate is cmd/migrate: applies migrations and exits.
	BinaryMigrate Binary = "migrate"
)

// LoadFor reads configuration from the environment and validates it against the
// contract the given binary actually requires. Every binary's main() must use
// this rather than Load, so that a check added for one binary can never make
// another binary's container refuse to start.
func LoadFor(binary Binary) (*Config, error) {
	cfg := load()

	var err error
	switch binary {
	case BinaryAPI:
		err = cfg.ValidateForAPI()
	case BinaryWorker:
		err = cfg.ValidateForWorker()
	case BinaryMigrate:
		err = cfg.ValidateForMigrate()
	default:
		err = fmt.Errorf("unknown binary %q: cannot pick a configuration contract", binary)
	}
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Load reads configuration from environment variables and applies only the
// checks that hold for EVERY binary. Prefer LoadFor: this entry point exists
// for tooling and tests that need a populated Config without committing to one
// binary's contract.
func Load() (*Config, error) {
	cfg := load()
	if err := cfg.validateShared(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// load reads the environment into a Config without validating anything.
func load() *Config {
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

	// H4 dual-read: the pre-H4 request-id review links stay accepted by default.
	// Defaulting this to false would break every review link already in a
	// mentee's inbox the moment this ships.
	v.SetDefault("REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED", true)

	// Worker defaults (background worker binary, cmd/worker)
	v.SetDefault("WORKER_PORT", "8090")
	v.SetDefault("WORKER_DB_MAX_CONNS", 5)
	v.SetDefault("WORKER_CRON_ENABLED", true)
	v.SetDefault("O11Y_WORKER_SERVICE_NAME", "openmentor-worker")
	// Profile purge (D70): nightly at 03:15, keep deleted profiles 30 days.
	// 03:15 stays clear of randomize-sort-order at 01:00 and sessions-watcher
	// at 08:30 — the purge takes row locks on mentors and client_requests, and
	// there is no reason for it to contend with a job that emails people.
	v.SetDefault("WORKER_PROFILE_PURGE_CRON", "0 15 3 * * *")
	v.SetDefault("WORKER_PROFILE_PURGE_RETENTION_DAYS", 30)

	// Email defaults
	v.SetDefault("MODERATORS_EMAIL", "moderators@openmentor.io")

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
			SecretKey:        v.GetString("TURNSTILE_SECRET_KEY"),
			ExpectedHostname: strings.TrimSpace(v.GetString("TURNSTILE_EXPECTED_HOSTNAME")),
			ExpectedAction:   strings.TrimSpace(v.GetString("TURNSTILE_EXPECTED_ACTION")),
		},
		Review: ReviewConfig{
			LegacyRequestIDLinksEnabled: v.GetBool("REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED"),
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

			ProfilePurgeCron:          v.GetString("WORKER_PROFILE_PURGE_CRON"),
			ProfilePurgeRetentionDays: v.GetInt("WORKER_PROFILE_PURGE_RETENTION_DAYS"),
		},
		Email: EmailConfig{
			SESRegion:          v.GetString("SES_REGION"),
			SESAccessKeyID:     v.GetString("SES_ACCESS_KEY_ID"),
			SESSecretAccessKey: v.GetString("SES_SECRET_ACCESS_KEY"),
			SESEndpoint:        v.GetString("SES_ENDPOINT"),
			DevEmailOverride:   v.GetString("DEV_EMAIL_OVERRIDE"),
			ModeratorsEmail:    v.GetString("MODERATORS_EMAIL"),
			DiscordInviteLink:  v.GetString("DISCORD_MENTORS_PRIVATE_INVITE_LINK"),
		},
	}

	return cfg
}

// validateShared holds the checks that are true of all three binaries, because
// they cover values all three read (the database DSN) or a self-inflicted
// misconfiguration of a shared subsystem. Everything else belongs to exactly one
// binary's profile below.
func (c *Config) validateShared() error {
	if err := c.validateDatabaseConfig(); err != nil {
		return err
	}
	return c.validateAnalyticsConfig()
}

// ValidateForAPI enforces cmd/api's production contract: it serves the public
// HTTP surface, mints and reads sessions, uploads to object storage, verifies
// captchas and fires the worker's event triggers.
func (c *Config) ValidateForAPI() error {
	if err := c.validateShared(); err != nil {
		return err
	}
	if err := c.validateAuthConfig(); err != nil {
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
	// The API is the CALLER of the worker's /jobs endpoints, so it needs the
	// shared token to sign those calls; the worker needs the same value to
	// check them. Both profiles therefore require it, and migrate does not.
	if err := c.validateWorkerAuthToken(); err != nil {
		return err
	}
	if err := c.validateEventTriggerConfig(); err != nil {
		return err
	}
	if err := c.validateS3StorageConfig(); err != nil {
		return err
	}
	return c.validateProfilingConfig()
}

// ValidateForWorker enforces cmd/worker's production contract: it sends every
// transactional email and serves the internal /jobs endpoints. It never touches
// object storage, the mentors API tokens, Turnstile or the session secret —
// requiring those was the a57aec2 defect.
func (c *Config) ValidateForWorker() error {
	if err := c.validateShared(); err != nil {
		return err
	}
	if c.Worker.Port == "" {
		return fmt.Errorf("WORKER_PORT is required")
	}
	// WORKER_PROFILE_PURGE_* are deliberately NOT validated here. The worker
	// sends every transactional email on the platform, and refusing to boot it
	// over a purge setting it can safely default is the a57aec2 failure mode
	// again. A missing or nonsensical value instead falls back to the documented
	// default in worker.NewHandlers, which logs the substitution — and the purge
	// repository refuses a non-positive retention outright, so the one dangerous
	// reading ("0 days means erase everything now") is impossible on every path.
	if err := c.validateWorkerAuthToken(); err != nil {
		return err
	}
	if err := c.validateWorkerBaseURL(); err != nil {
		return err
	}
	if err := c.validateEmailConfig(); err != nil {
		return err
	}
	return c.validateProfilingConfig()
}

// ValidateForMigrate enforces cmd/migrate's contract, which is one value: the
// DSN. It applies migrations and exits — it holds no HTTP server, no email
// sender, no storage client and no session key.
//
// DATABASE_URL is required unconditionally here, unlike the shared check:
// DB_WORK_OFFLINE cannot make migrations run against nothing, so honoring it
// would only turn a missing DSN into a silent no-op deploy.
func (c *Config) ValidateForMigrate() error {
	if strings.TrimSpace(c.Database.URL) == "" {
		return fmt.Errorf("DATABASE_URL is required to run migrations")
	}
	return nil
}

// minJWTSecretLength is the minimum accepted JWT_SECRET length in bytes. A
// short secret makes the HS256 signature brute-forceable offline, which would
// let an attacker forge mentor AND moderator sessions (one secret signs both).
const minJWTSecretLength = 32

// maxSessionTTLHours caps SESSION_TTL_HOURS at 30 days. The tokens are
// stateless HS256 bearers with no revocation list, so the TTL *is* the only
// bound on a stolen cookie's usefulness.
const maxSessionTTLHours = 24 * 30

// maxLoginTokenTTLMinutes caps LOGIN_TOKEN_TTL_MINUTES at two hours. A
// magic-link token sits in an inbox — and in whatever scanned it in transit —
// for its whole lifetime.
const maxLoginTokenTTLMinutes = 120

// validateSessionConfig enforces the mentor/moderator session contract:
// JWT_SECRET presence and length, both TTLs inside sane bounds, and a Secure
// cookie in production.
//
// In non-production an empty secret is allowed (auth routes self-disable with
// a warning), but a set-but-too-short secret is always rejected.
//
// The TTL bounds exist because viper turns a typo into a number without
// complaint: SESSION_TTL_HOURS=24h parses as 0, which mints already-expired
// sessions and locks every mentor out of the portal with no error anywhere.
func (c *Config) validateSessionConfig() error {
	secret := c.MentorSession.JWTSecret
	if c.IsProduction() && secret == "" {
		return fmt.Errorf("JWT_SECRET is required in production")
	}
	if secret != "" && len(secret) < minJWTSecretLength {
		return fmt.Errorf("JWT_SECRET must be at least %d characters", minJWTSecretLength)
	}

	if h := c.MentorSession.SessionTTLHours; h < 1 || h > maxSessionTTLHours {
		return fmt.Errorf("SESSION_TTL_HOURS must be between 1 and %d, got %d", maxSessionTTLHours, h)
	}
	if m := c.MentorSession.LoginTokenTTLMinutes; m < 1 || m > maxLoginTokenTTLMinutes {
		return fmt.Errorf("LOGIN_TOKEN_TTL_MINUTES must be between 1 and %d, got %d",
			maxLoginTokenTTLMinutes, m)
	}
	if c.MentorSession.JWTIssuer == "" {
		return fmt.Errorf("JWT_ISSUER is required (it is checked when a session is verified)")
	}

	// A session cookie sent over plain http is readable by anything on the
	// path, and the API is only ever reached through Traefik's TLS in
	// production. COOKIE_SECURE defaults to true, so this only fires when
	// someone has explicitly turned it off.
	if c.IsProduction() && !c.MentorSession.CookieSecure {
		return fmt.Errorf("COOKIE_SECURE must not be disabled in production")
	}
	return nil
}

// validateWorkerAuthToken enforces WORKER_AUTH_TOKEN in production so the
// worker's /jobs/* endpoints (email dispatch, moderation actions) can never
// fail open to an unauthenticated caller on the internal network. Required by
// the API too, which is the caller that has to present it.
func (c *Config) validateWorkerAuthToken() error {
	if c.IsProduction() && c.Worker.AuthToken == "" {
		return fmt.Errorf("WORKER_AUTH_TOKEN is required in production")
	}
	return nil
}

// validateWorkerBaseURL checks the one server setting the worker reads:
// BASE_URL, which it renders into every email link (login magic links, review
// invitations). A relative or javascript: value here would ship in mail to
// mentors and mentees, so the scheme/host rule that guards mentor-supplied URLs
// guards this one too.
func (c *Config) validateWorkerBaseURL() error {
	if c.Server.BaseURL == "" {
		return fmt.Errorf("BASE_URL is required (it is rendered into every email link)")
	}
	if c.IsProduction() && !safeurl.IsHTTPS(c.Server.BaseURL) {
		return fmt.Errorf("BASE_URL must be an absolute https URL with a host in production")
	}
	return nil
}

// validateEmailConfig enforces the worker's SES contract. Same shape as
// validateS3StorageConfig and for the same reason: pkg/email only builds a real
// SESv2 client when the credentials are present, so a half-configured sender
// boots green and then silently drops every mentor's login link.
//
// DEV_EMAIL_OVERRIDE is rejected in production outright — it reroutes ALL
// recipients to one mailbox, so leaving a staging value in a production .env
// would send mentors' magic links to a developer instead of the mentors.
func (c *Config) validateEmailConfig() error {
	missing := missingSettings([]setting{
		{"SES_REGION", c.Email.SESRegion},
		{"SES_ACCESS_KEY_ID", c.Email.SESAccessKeyID},
		{"SES_SECRET_ACCESS_KEY", c.Email.SESSecretAccessKey},
	})
	switch {
	case len(missing) == 3 && c.IsProduction():
		return fmt.Errorf("SES email delivery is required in production: %s", strings.Join(missing, ", "))
	case len(missing) > 0 && len(missing) < 3:
		return fmt.Errorf("SES email delivery is partially configured: %s must also be set",
			strings.Join(missing, ", "))
	}

	if strings.TrimSpace(c.Email.ModeratorsEmail) == "" {
		return fmt.Errorf("MODERATORS_EMAIL is required (moderation notifications have no other recipient)")
	}
	if c.IsProduction() && strings.TrimSpace(c.Email.DevEmailOverride) != "" {
		return fmt.Errorf("DEV_EMAIL_OVERRIDE must be empty in production: it reroutes every recipient")
	}
	if link := strings.TrimSpace(c.Email.DiscordInviteLink); link != "" && !safeurl.IsHTTPS(link) {
		return fmt.Errorf("DISCORD_MENTORS_PRIVATE_INVITE_LINK must be an absolute https URL with a host")
	}
	return nil
}

// mentorCreatedTriggerJob is the path segment derivedMentorTriggerURL rewrites.
// Two further triggers (mentor-confirmed, mentor-confirm-email) are derived from
// MENTOR_CREATED_TRIGGER_URL by substituting it, and derivation returns "" —
// i.e. "skip the trigger" — when the segment is absent. So a MENTOR_CREATED URL
// pointing at any other job silently disables the mentor confirmation emails.
const mentorCreatedTriggerJob = "new-mentor-watcher"

// validateEventTriggerConfig checks the fire-and-forget worker triggers the API
// calls. They are dialed over the internal Docker network as
// http://worker:8090/jobs/..., so http is allowed — but the scheme allowlist
// still matters, and a production deploy missing one of them loses a whole email
// path with nothing but a debug log to show for it.
//
// MENTOR_UPDATED_TRIGGER_URL is deliberately unset (the worker serves no such
// job), so it is optional-but-well-formed rather than required.
func (c *Config) validateEventTriggerConfig() error {
	required := []setting{
		{"MENTOR_CREATED_TRIGGER_URL", c.EventTriggers.MentorCreatedTriggerURL},
		{"MENTOR_REQUEST_CREATED_TRIGGER_URL", c.EventTriggers.MentorRequestCreatedTriggerURL},
		{"MENTOR_LOGIN_EMAIL_TRIGGER_URL", c.EventTriggers.MentorLoginEmailTriggerURL},
		{"MODERATOR_LOGIN_EMAIL_TRIGGER_URL", c.EventTriggers.ModeratorLoginEmailTriggerURL},
		{"MENTOR_MODERATION_TRIGGER_URL", c.EventTriggers.MentorModerationTriggerURL},
		{"REQUEST_PROCESS_FINISHED_TRIGGER_URL", c.EventTriggers.RequestProcessFinishedTriggerURL},
		{"REVIEW_CREATED_TRIGGER_URL", c.EventTriggers.ReviewCreatedTriggerURL},
	}
	if c.IsProduction() {
		if missing := missingSettings(required); len(missing) > 0 {
			return fmt.Errorf("event trigger URLs are required in production: %s",
				strings.Join(missing, ", "))
		}
	}

	optional := make([]setting, 0, len(required)+1)
	optional = append(optional, required...)
	optional = append(optional, setting{"MENTOR_UPDATED_TRIGGER_URL", c.EventTriggers.MentorUpdatedTriggerURL})
	for _, s := range optional {
		value := strings.TrimSpace(s.value)
		if value != "" && !safeurl.IsHTTPOrHTTPS(value) {
			return fmt.Errorf("%s must be an absolute http(s) URL with a host", s.name)
		}
	}

	created := c.EventTriggers.MentorCreatedTriggerURL
	if created != "" && !strings.Contains(created, mentorCreatedTriggerJob) {
		return fmt.Errorf("MENTOR_CREATED_TRIGGER_URL must contain %q: the mentor-confirmed and "+
			"mentor-confirm-email triggers are derived from it by substituting that path segment",
			mentorCreatedTriggerJob)
	}
	return nil
}

// setting pairs an env var NAME with its value. Only the name is ever put in an
// error message — several of these are secrets.
type setting struct {
	name  string
	value string
}

// missingSettings returns the NAMES of the settings that are blank.
func missingSettings(settings []setting) []string {
	var missing []string
	for _, s := range settings {
		if strings.TrimSpace(s.value) == "" {
			missing = append(missing, s.name)
		}
	}
	return missing
}

// validateS3StorageConfig enforces a COMPLETE object storage configuration.
// main.go only builds the storage client when the credentials are present, so
// a half-configured bucket boots green and then nil-derefs on the first profile
// picture — inside a detached goroutine, where recover() cannot save the
// process. Production must be fully configured; off production an entirely
// empty block is allowed (uploads self-disable and the photo paths reject with
// ErrUploadsUnavailable), but a PARTIAL block is always a typo.
func (c *Config) validateS3StorageConfig() error {
	settings := []setting{
		{"S3_STORAGE_ACCESS_KEY", c.S3Storage.AccessKeyID},
		{"S3_STORAGE_SECRET_KEY", c.S3Storage.SecretAccessKey},
		{"S3_STORAGE_BUCKET", c.S3Storage.BucketName},
		{"S3_STORAGE_ENDPOINT", c.S3Storage.Endpoint},
		{"S3_STORAGE_REGION", c.S3Storage.Region},
	}

	missing := missingSettings(settings)

	switch {
	case len(missing) == 0:
		return nil
	case len(missing) < len(settings):
		return fmt.Errorf("S3 object storage is partially configured: %s must also be set",
			strings.Join(missing, ", "))
	case c.IsProduction():
		return fmt.Errorf("S3 object storage is required in production: %s",
			strings.Join(missing, ", "))
	default:
		return nil
	}
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
	// A hostname with a scheme or a path never matches what siteverify echoes
	// back (a bare host), so it would reject every captcha instead of pinning
	// one — a total outage of every public write, configured silently.
	if h := c.Turnstile.ExpectedHostname; h != "" && strings.ContainsAny(h, ":/ ") {
		return fmt.Errorf("TURNSTILE_EXPECTED_HOSTNAME must be a bare hostname, not a URL")
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
	// The API renders BASE_URL into review links and redirect targets, so it is
	// held to the same scheme/host rule as a mentor-supplied URL.
	if c.IsProduction() && !safeurl.IsHTTPS(c.Server.BaseURL) {
		return fmt.Errorf("BASE_URL must be an absolute https URL with a host in production")
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
