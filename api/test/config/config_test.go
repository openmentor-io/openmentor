package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_IsDevelopment(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected bool
	}{
		{
			name: "development environment",
			cfg: &config.Config{
				Server: config.ServerConfig{AppEnv: "development"},
			},
			expected: true,
		},
		{
			name: "debug gin mode",
			cfg: &config.Config{
				Server: config.ServerConfig{GinMode: "debug"},
			},
			expected: true,
		},
		{
			name: "production environment",
			cfg: &config.Config{
				Server: config.ServerConfig{AppEnv: "production"},
			},
			expected: false,
		},
		{
			name: "release mode",
			cfg: &config.Config{
				Server: config.ServerConfig{GinMode: "release", AppEnv: "production"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.IsDevelopment()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_IsProduction(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected bool
	}{
		{
			name: "production environment",
			cfg: &config.Config{
				Server: config.ServerConfig{AppEnv: "production"},
			},
			expected: true,
		},
		{
			name: "development environment",
			cfg: &config.Config{
				Server: config.ServerConfig{AppEnv: "development"},
			},
			expected: false,
		},
		{
			name: "staging environment",
			cfg: &config.Config{
				Server: config.ServerConfig{AppEnv: "staging"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.cfg.IsProduction()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_ResolvedAnalyticsProvider(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name: "explicit provider",
			cfg: &config.Config{
				Analytics: config.AnalyticsConfig{Provider: "posthog"},
				PostHog:   config.PostHogConfig{Enabled: true},
			},
			expected: "posthog",
		},
		{
			name: "legacy posthog fallback",
			cfg: &config.Config{
				PostHog: config.PostHogConfig{Enabled: true},
			},
			expected: "posthog",
		},
		{
			name:     "default none",
			cfg:      &config.Config{},
			expected: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cfg.ResolvedAnalyticsProvider())
		})
	}
}

// TestConfig_ValidateForAPI covers the API contract field by field. Each case
// starts from apiConfig (which passes) and breaks one thing.
func TestConfig_ValidateForAPI(t *testing.T) {
	tests := []struct {
		name     string
		appEnv   string
		mutate   func(*config.Config)
		errorMsg string
	}{
		{name: "complete production config", appEnv: "production"},
		{name: "complete development config", appEnv: "development"},
		{
			name: "offline database", appEnv: "development",
			mutate: func(c *config.Config) { c.Database.URL = ""; c.Database.WorkOffline = true },
		},
		{
			name: "missing database url", appEnv: "development",
			mutate:   func(c *config.Config) { c.Database.URL = "" },
			errorMsg: "DATABASE_URL is required",
		},
		{
			name: "invalid analytics provider", appEnv: "development",
			mutate:   func(c *config.Config) { c.Analytics.Provider = "invalid-provider" },
			errorMsg: "ANALYTICS_PROVIDER must be one of",
		},
		{
			name: "posthog provider missing api key", appEnv: "development",
			mutate: func(c *config.Config) {
				c.Analytics.Provider = "posthog"
				c.PostHog.Host = "https://eu.i.posthog.com"
			},
			errorMsg: "POSTHOG_API_KEY is required",
		},
		{
			name: "posthog provider missing host and endpoint", appEnv: "development",
			mutate: func(c *config.Config) {
				c.Analytics.Provider = "posthog"
				c.PostHog.APIKey = "ph-key"
			},
			errorMsg: "POSTHOG_HOST or POSTHOG_CAPTURE_ENDPOINT is required",
		},
		{
			name: "valid posthog provider config", appEnv: "development",
			mutate: func(c *config.Config) {
				c.Analytics.Provider = "posthog"
				c.PostHog.APIKey = "ph-key"
				c.PostHog.Host = "https://eu.i.posthog.com"
			},
		},
		{
			name: "missing internal API token", appEnv: "development",
			mutate:   func(c *config.Config) { c.Auth.InternalMentorsAPI = "" },
			errorMsg: "INTERNAL_MENTORS_API is required",
		},
		{
			// AllowCredentials is true, so a wildcard origin would let any site
			// make credentialed cross-origin requests.
			name: "wildcard CORS origin", appEnv: "production",
			mutate:   func(c *config.Config) { c.Server.AllowedOrigins = []string{"*"} },
			errorMsg: "must not contain '*'",
		},
		{
			name: "profiling enabled without endpoint", appEnv: "development",
			mutate:   func(c *config.Config) { c.Profiling.Enabled = true },
			errorMsg: "O11Y_PROFILING_ENDPOINT is required",
		},
		{
			name: "profiling enabled with endpoint", appEnv: "development",
			mutate: func(c *config.Config) {
				c.Profiling.Enabled = true
				c.Profiling.Endpoint = "http://alloy:4040"
			},
		},
		{
			name: "short JWT secret is rejected off production too", appEnv: "development",
			mutate:   func(c *config.Config) { c.MentorSession.JWTSecret = "too-short" },
			errorMsg: "JWT_SECRET must be at least 32 characters",
		},
		{
			// Auth self-disables without a secret off production.
			name: "empty JWT secret is allowed off production", appEnv: "development",
			mutate: func(c *config.Config) { c.MentorSession.JWTSecret = "" },
		},
		{
			// viper parses "24h" as 0, which mints already-expired sessions and
			// locks every mentor out with no error anywhere.
			name: "zero session TTL", appEnv: "production",
			mutate:   func(c *config.Config) { c.MentorSession.SessionTTLHours = 0 },
			errorMsg: "SESSION_TTL_HOURS must be between",
		},
		{
			name: "absurd session TTL", appEnv: "production",
			mutate:   func(c *config.Config) { c.MentorSession.SessionTTLHours = 24 * 365 },
			errorMsg: "SESSION_TTL_HOURS must be between",
		},
		{
			name: "zero login token TTL", appEnv: "production",
			mutate:   func(c *config.Config) { c.MentorSession.LoginTokenTTLMinutes = 0 },
			errorMsg: "LOGIN_TOKEN_TTL_MINUTES must be between",
		},
		{
			name: "week-long magic links", appEnv: "production",
			mutate:   func(c *config.Config) { c.MentorSession.LoginTokenTTLMinutes = 7 * 24 * 60 },
			errorMsg: "LOGIN_TOKEN_TTL_MINUTES must be between",
		},
		{
			name: "missing JWT issuer", appEnv: "production",
			mutate:   func(c *config.Config) { c.MentorSession.JWTIssuer = "" },
			errorMsg: "JWT_ISSUER is required",
		},
		{
			name: "insecure cookies in production", appEnv: "production",
			mutate:   func(c *config.Config) { c.MentorSession.CookieSecure = false },
			errorMsg: "COOKIE_SECURE must not be disabled in production",
		},
		{
			name: "insecure cookies off production", appEnv: "development",
			mutate: func(c *config.Config) { c.MentorSession.CookieSecure = false },
		},
		{
			name: "plain http BASE_URL in production", appEnv: "production",
			mutate:   func(c *config.Config) { c.Server.BaseURL = "http://openmentor.example" },
			errorMsg: "BASE_URL must be an absolute https URL",
		},
		{
			name: "localhost BASE_URL off production", appEnv: "development",
			mutate: func(c *config.Config) { c.Server.BaseURL = "http://localhost:3000" },
		},
		{
			name: "javascript BASE_URL", appEnv: "production",
			mutate:   func(c *config.Config) { c.Server.BaseURL = "javascript:alert(1)" },
			errorMsg: "BASE_URL must be an absolute https URL",
		},
		{
			name: "turnstile hostname given as a URL", appEnv: "production",
			mutate:   func(c *config.Config) { c.Turnstile.ExpectedHostname = "https://openmentor.io/" },
			errorMsg: "TURNSTILE_EXPECTED_HOSTNAME must be a bare hostname",
		},
		{
			name: "turnstile hostname given as a host", appEnv: "production",
			mutate: func(c *config.Config) {
				c.Turnstile.ExpectedHostname = "openmentor.io"
				c.Turnstile.ExpectedAction = "contact-mentor"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := apiConfig(tt.appEnv)
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			err := cfg.ValidateForAPI()
			if tt.errorMsg == "" {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

// TestConfig_ValidateForWorker covers the settings only the worker reads.
func TestConfig_ValidateForWorker(t *testing.T) {
	tests := []struct {
		name     string
		appEnv   string
		mutate   func(*config.Config)
		errorMsg string
	}{
		{name: "complete production config", appEnv: "production"},
		{name: "complete development config", appEnv: "development"},
		{
			// pkg/email only builds a real SESv2 client when the credentials are
			// present, so a half-configured sender boots green and then silently
			// drops every mentor's login link.
			name: "partial SES block is a typo in any environment", appEnv: "development",
			mutate:   func(c *config.Config) { c.Email.SESAccessKeyID = "" },
			errorMsg: "SES email delivery is partially configured: SES_ACCESS_KEY_ID",
		},
		{
			name: "empty SES block off production", appEnv: "development",
			mutate: func(c *config.Config) {
				c.Email.SESRegion, c.Email.SESAccessKeyID, c.Email.SESSecretAccessKey = "", "", ""
			},
		},
		{
			name: "empty SES block in production", appEnv: "production",
			mutate: func(c *config.Config) {
				c.Email.SESRegion, c.Email.SESAccessKeyID, c.Email.SESSecretAccessKey = "", "", ""
			},
			errorMsg: "SES email delivery is required in production",
		},
		{
			// It reroutes ALL recipients to one mailbox: a staging value left in
			// a production .env sends mentors' magic links to a developer.
			name: "DEV_EMAIL_OVERRIDE in production", appEnv: "production",
			mutate:   func(c *config.Config) { c.Email.DevEmailOverride = "dev@example.test" },
			errorMsg: "DEV_EMAIL_OVERRIDE must be empty in production",
		},
		{
			name: "DEV_EMAIL_OVERRIDE off production", appEnv: "development",
			mutate: func(c *config.Config) { c.Email.DevEmailOverride = "dev@example.test" },
		},
		{
			name: "non-https Discord invite link", appEnv: "production",
			mutate:   func(c *config.Config) { c.Email.DiscordInviteLink = "discord.gg/abc" },
			errorMsg: "DISCORD_MENTORS_PRIVATE_INVITE_LINK must be an absolute https URL",
		},
		{
			name: "https Discord invite link", appEnv: "production",
			mutate: func(c *config.Config) { c.Email.DiscordInviteLink = "https://discord.gg/abc" },
		},
		{
			name: "unset Discord invite link", appEnv: "production",
			mutate: func(c *config.Config) { c.Email.DiscordInviteLink = "" },
		},
		{
			name: "plain http BASE_URL in production", appEnv: "production",
			mutate:   func(c *config.Config) { c.Server.BaseURL = "http://openmentor.example" },
			errorMsg: "BASE_URL must be an absolute https URL",
		},
		{
			name: "profiling enabled without endpoint", appEnv: "development",
			mutate:   func(c *config.Config) { c.Profiling.Enabled = true },
			errorMsg: "O11Y_PROFILING_ENDPOINT is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := workerConfig(tt.appEnv)
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			err := cfg.ValidateForWorker()
			if tt.errorMsg == "" {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

// TestConfig_ValidateEventTriggerURLs: the API fires these at the worker over the
// internal network, so http is correct and a missing one loses a whole email path
// with nothing but a debug log to show for it.
func TestConfig_ValidateEventTriggerURLs(t *testing.T) {
	tests := []struct {
		name     string
		appEnv   string
		mutate   func(*config.Config)
		errorMsg string
	}{
		{
			name: "internal http trigger URLs are fine", appEnv: "production",
		},
		{
			name: "one missing trigger in production", appEnv: "production",
			mutate:   func(c *config.Config) { c.EventTriggers.ReviewCreatedTriggerURL = "" },
			errorMsg: "event trigger URLs are required in production: REVIEW_CREATED_TRIGGER_URL",
		},
		{
			name: "missing triggers off production", appEnv: "development",
			mutate: func(c *config.Config) {
				c.EventTriggers = config.EventTriggerFunctionsConfig{}
			},
		},
		{
			name: "relative trigger URL", appEnv: "development",
			mutate:   func(c *config.Config) { c.EventTriggers.MentorLoginEmailTriggerURL = "/jobs/x" },
			errorMsg: "MENTOR_LOGIN_EMAIL_TRIGGER_URL must be an absolute http(s) URL",
		},
		{
			name: "non-http scheme on a trigger URL", appEnv: "development",
			mutate:   func(c *config.Config) { c.EventTriggers.MentorModerationTriggerURL = "file:///etc/passwd" },
			errorMsg: "MENTOR_MODERATION_TRIGGER_URL must be an absolute http(s) URL",
		},
		{
			// Optional, but still has to be well formed when set.
			name: "malformed optional trigger URL", appEnv: "development",
			mutate:   func(c *config.Config) { c.EventTriggers.MentorUpdatedTriggerURL = "javascript:alert(1)" },
			errorMsg: "MENTOR_UPDATED_TRIGGER_URL must be an absolute http(s) URL",
		},
		{
			// MentorConfirmedTriggerURL/MentorConfirmEmailTriggerURL are DERIVED
			// from this one by substituting the job segment, and derivation
			// returns "" (skip the trigger) when the segment is missing — so a
			// URL pointing at any other job silently kills the mentor
			// confirmation emails.
			name: "MENTOR_CREATED_TRIGGER_URL pointing at another job", appEnv: "production",
			mutate: func(c *config.Config) {
				c.EventTriggers.MentorCreatedTriggerURL = "http://worker:8090/jobs/something-else?mentorId="
			},
			errorMsg: `MENTOR_CREATED_TRIGGER_URL must contain "new-mentor-watcher"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := apiConfig(tt.appEnv)
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			err := cfg.ValidateForAPI()
			if tt.errorMsg == "" {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

// TestDerivedMentorTriggerURLsFollowTheValidatedOne ties the check above to the
// behavior it protects.
func TestDerivedMentorTriggerURLsFollowTheValidatedOne(t *testing.T) {
	triggers := apiConfig("production").EventTriggers
	assert.Equal(t, "http://worker:8090/jobs/mentor-confirmed?mentorId=",
		triggers.MentorConfirmedTriggerURL())
	assert.Equal(t, "http://worker:8090/jobs/mentor-confirm-email?mentorId=",
		triggers.MentorConfirmEmailTriggerURL())
	// Profile deletion confirmations (D70) ride the same derivation.
	assert.Equal(t, "http://worker:8090/jobs/profile-deleted?mentorId=",
		triggers.ProfileDeletedTriggerURL())
	assert.Equal(t, "http://worker:8090/jobs/profile-restored?mentorId=",
		triggers.ProfileRestoredTriggerURL())

	triggers.MentorCreatedTriggerURL = "http://worker:8090/jobs/something-else"
	assert.Empty(t, triggers.MentorConfirmedTriggerURL(),
		"derivation silently yields no trigger, which is why the validator rejects the input")
	assert.Empty(t, triggers.ProfileDeletedTriggerURL())
	assert.Empty(t, triggers.ProfileRestoredTriggerURL())
}

func TestConfig_ValidateS3Storage(t *testing.T) {
	missingKey := completeS3
	missingKey.AccessKeyID = ""
	missingRegion := completeS3
	missingRegion.Region = " " // whitespace is not configuration

	tests := []struct {
		name     string
		appEnv   string
		s3       config.S3StorageConfig
		errorMsg string
	}{
		{name: "complete in production", appEnv: "production", s3: completeS3},
		{name: "complete in development", appEnv: "development", s3: completeS3},
		{
			name: "empty in production", appEnv: "production",
			errorMsg: "S3 object storage is required in production",
		},
		{name: "empty in development", appEnv: "development"},
		{
			name: "partial in production", appEnv: "production", s3: missingKey,
			errorMsg: "S3 object storage is partially configured: S3_STORAGE_ACCESS_KEY",
		},
		{
			// A half-configured bucket is a typo everywhere, not a choice.
			name: "partial in development", appEnv: "development", s3: missingRegion,
			errorMsg: "S3 object storage is partially configured: S3_STORAGE_REGION",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := apiConfig(tt.appEnv)
			cfg.S3Storage = tt.s3
			err := cfg.ValidateForAPI()
			if tt.errorMsg == "" {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

// TestValidationErrorsNameSettingsNotValues: these messages reach container logs
// and CI output, so a validator may print the env var NAME and never its value.
func TestValidationErrorsNameSettingsNotValues(t *testing.T) {
	const secret = "s3cr3t-value-that-must-never-be-logged"

	cfg := apiConfig("production")
	cfg.S3Storage.AccessKeyID = ""
	cfg.S3Storage.SecretAccessKey = secret
	err := cfg.ValidateForAPI()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3_STORAGE_ACCESS_KEY")
	assert.NotContains(t, err.Error(), secret)

	worker := workerConfig("production")
	worker.Email.SESAccessKeyID = ""
	worker.Email.SESSecretAccessKey = secret
	err = worker.ValidateForWorker()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SES_ACCESS_KEY_ID")
	assert.NotContains(t, err.Error(), secret)
}

func TestLoad_WithDefaults(t *testing.T) {
	// Clean environment
	os.Clearenv()

	// Set only required fields (APP_ENV defaults to production, which also
	// requires JWT_SECRET >= 32 chars, WORKER_AUTH_TOKEN and a complete
	// S3_STORAGE_* block).
	os.Setenv("DB_WORK_OFFLINE", "true")
	os.Setenv("INTERNAL_MENTORS_API", "test-token")
	os.Setenv("MENTORS_API_LIST_AUTH_TOKEN", "public-token")
	os.Setenv("TURNSTILE_SECRET_KEY", "turnstile-secret")
	os.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars-long")
	os.Setenv("WORKER_AUTH_TOKEN", "worker-secret")
	os.Setenv("S3_STORAGE_ACCESS_KEY", "key")
	os.Setenv("S3_STORAGE_SECRET_KEY", "secret")
	os.Setenv("S3_STORAGE_BUCKET", "mentor-images")
	os.Setenv("S3_STORAGE_ENDPOINT", "https://s3.eu-central-1.amazonaws.com")
	os.Setenv("S3_STORAGE_REGION", "eu-central-1")

	cfg, err := config.Load()

	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	// Check defaults
	assert.Equal(t, "8081", cfg.Server.Port)
	assert.Equal(t, "release", cfg.Server.GinMode)
	assert.Equal(t, "production", cfg.Server.AppEnv)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "/app/logs", cfg.Logging.Dir)
	assert.False(t, cfg.Profiling.Enabled)
	assert.Equal(t, "openmentor-api", cfg.Profiling.AppName)
	assert.Equal(t, "cpu,alloc_space,alloc_objects,goroutines,mutex,block", cfg.Profiling.SampleTypes)
	assert.Equal(t, 15, cfg.Profiling.UploadIntervalSeconds)
}

func TestLoad_WithEnvironmentVariables(t *testing.T) {
	// Clean environment
	os.Clearenv()

	// Set environment variables
	os.Setenv("PORT", "9000")
	os.Setenv("GIN_MODE", "debug")
	os.Setenv("APP_ENV", "development")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("DB_WORK_OFFLINE", "false")
	os.Setenv("DATABASE_URL", "pg://test.db")
	os.Setenv("INTERNAL_MENTORS_API", "internal-token-789")
	os.Setenv("MENTORS_API_LIST_AUTH_TOKEN", "token1")
	os.Setenv("TURNSTILE_SECRET_KEY", "turnstile-secret")
	os.Setenv("O11Y_PROFILING_ENABLED", "true")
	os.Setenv("O11Y_PROFILING_ENDPOINT", "http://alloy:4040")
	os.Setenv("O11Y_PROFILING_APP_NAME", "openmentor-api")
	os.Setenv("O11Y_PROFILING_SAMPLE_TYPES", "cpu,goroutines")
	os.Setenv("O11Y_PROFILING_UPLOAD_INTERVAL_SECONDS", "20")

	cfg, err := config.Load()

	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	// Verify values from environment
	assert.Equal(t, "9000", cfg.Server.Port)
	assert.Equal(t, "debug", cfg.Server.GinMode)
	assert.Equal(t, "development", cfg.Server.AppEnv)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "internal-token-789", cfg.Auth.InternalMentorsAPI)
	assert.Equal(t, "token1", cfg.Auth.MentorsAPIToken)
	assert.Equal(t, "turnstile-secret", cfg.Turnstile.SecretKey)
	assert.True(t, cfg.Profiling.Enabled)
	assert.Equal(t, "http://alloy:4040", cfg.Profiling.Endpoint)
	assert.Equal(t, "openmentor-api", cfg.Profiling.AppName)
	assert.Equal(t, "cpu,goroutines", cfg.Profiling.SampleTypes)
	assert.Equal(t, 20, cfg.Profiling.UploadIntervalSeconds)
}

func TestLoad_ValidationFailure(t *testing.T) {
	// Save current directory and change to a temp directory without .env file
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tempDir := t.TempDir()
	os.Chdir(tempDir)

	// Clean environment - missing required fields
	os.Clearenv()
	os.Setenv("DB_WORK_OFFLINE", "false")

	cfg, err := config.Load()

	assert.Error(t, err)
	assert.Nil(t, cfg)
}

// TestLoadFor_MigrateNeedsNoS3Credentials is the environment-level version of
// TestPerBinaryContracts: it drives the real Load path, from a cleared
// environment holding nothing but the DSN, exactly as the migrate container does.
//
// Before per-binary validation this failed with "S3 object storage is required in
// production" — the 2026-08-04 outage.
func TestLoadFor_MigrateNeedsNoS3Credentials(t *testing.T) {
	inEmptyEnv(t, func() {
		os.Setenv("APP_ENV", "production")
		os.Setenv("DATABASE_URL", "postgres://u:p@postgres:5432/db")

		cfg, err := config.LoadFor(config.BinaryMigrate)
		require.NoError(t, err)
		assert.Empty(t, cfg.S3Storage.AccessKeyID)
	})
}

// TestLoadFor_WorkerNeedsNoS3Credentials: same, for the binary that would have
// failed one step later.
func TestLoadFor_WorkerNeedsNoS3Credentials(t *testing.T) {
	inEmptyEnv(t, func() {
		os.Setenv("APP_ENV", "production")
		os.Setenv("DATABASE_URL", "postgres://u:p@postgres:5432/db")
		os.Setenv("WORKER_AUTH_TOKEN", "worker-secret")
		os.Setenv("SES_REGION", "eu-central-1")
		os.Setenv("SES_ACCESS_KEY_ID", "ses-key-id")
		os.Setenv("SES_SECRET_ACCESS_KEY", "ses-secret")

		cfg, err := config.LoadFor(config.BinaryWorker)
		require.NoError(t, err)
		assert.Empty(t, cfg.S3Storage.SecretAccessKey)
		// BASE_URL and MODERATORS_EMAIL come from the defaults in Load, which is
		// why the worker service passes neither.
		assert.Equal(t, "https://openmentor.io", cfg.Server.BaseURL)
		assert.Equal(t, "moderators@openmentor.io", cfg.Email.ModeratorsEmail)
	})
}

// TestLoadFor_APIStillNeedsS3Credentials: trimming the other two contracts must
// not weaken the one binary that does upload.
func TestLoadFor_APIStillNeedsS3Credentials(t *testing.T) {
	inEmptyEnv(t, func() {
		os.Setenv("APP_ENV", "production")
		os.Setenv("DATABASE_URL", "postgres://u:p@postgres:5432/db")
		os.Setenv("INTERNAL_MENTORS_API", "internal-token")
		os.Setenv("MENTORS_API_LIST_AUTH_TOKEN", "public-token")
		os.Setenv("TURNSTILE_SECRET_KEY", "turnstile-secret")
		os.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars-long")
		os.Setenv("WORKER_AUTH_TOKEN", "worker-secret")
		for name, url := range productionTriggerURLs {
			os.Setenv(name, url)
		}

		_, err := config.LoadFor(config.BinaryAPI)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "S3 object storage is required in production")
	})
}

// inEmptyEnv runs fn with a cleared environment and a working directory that has
// no .env file, so viper sees only what the test sets. The environment is
// restored afterwards.
func inEmptyEnv(t *testing.T, fn func()) {
	t.Helper()

	saved := os.Environ()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Clearenv()
		for _, kv := range saved {
			if key, value, found := strings.Cut(kv, "="); found {
				os.Setenv(key, value)
			}
		}
		_ = os.Chdir(originalDir)
	})

	require.NoError(t, os.Chdir(t.TempDir()))
	os.Clearenv()
	fn()
}
