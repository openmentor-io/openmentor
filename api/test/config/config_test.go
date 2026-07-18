package config_test

import (
	"os"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/stretchr/testify/assert"
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

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid offline config",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Port:           "8081",
					BaseURL:        "https://example.com",
					AllowedOrigins: []string{"https://example.com"},
				},
				Database: config.DatabaseConfig{
					WorkOffline: true,
				},
				Auth: config.AuthConfig{
					InternalMentorsAPI: "test-token",
					MentorsAPIToken:    "public-token",
				},
				Turnstile: config.TurnstileConfig{
					SecretKey: "turnstile-secret",
				},
			},
			expectError: false,
		},
		{
			name: "valid online config",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Port:           "8081",
					BaseURL:        "https://example.com",
					AllowedOrigins: []string{"https://example.com"},
				},
				Database: config.DatabaseConfig{
					WorkOffline: false,
					URL:         "pg://database.db",
				},
				Auth: config.AuthConfig{
					InternalMentorsAPI: "test-token",
					MentorsAPIToken:    "public-token",
				},
				Turnstile: config.TurnstileConfig{
					SecretKey: "turnstile-secret",
				},
			},
			expectError: false,
		},
		{
			name: "invalid analytics provider",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Port:           "8081",
					BaseURL:        "https://example.com",
					AllowedOrigins: []string{"https://example.com"},
				},
				Database: config.DatabaseConfig{
					WorkOffline: true,
				},
				Auth: config.AuthConfig{
					InternalMentorsAPI: "test-token",
					MentorsAPIToken:    "public-token",
				},
				Analytics: config.AnalyticsConfig{
					Provider: "invalid-provider",
				},
				Turnstile: config.TurnstileConfig{
					SecretKey: "turnstile-secret",
				},
			},
			expectError: true,
			errorMsg:    "ANALYTICS_PROVIDER must be one of",
		},
		{
			name: "posthog provider missing api key",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Port:           "8081",
					BaseURL:        "https://example.com",
					AllowedOrigins: []string{"https://example.com"},
				},
				Database: config.DatabaseConfig{
					WorkOffline: true,
				},
				Auth: config.AuthConfig{
					InternalMentorsAPI: "test-token",
					MentorsAPIToken:    "public-token",
				},
				Analytics: config.AnalyticsConfig{
					Provider: "posthog",
				},
				PostHog: config.PostHogConfig{
					Host: "https://us.i.posthog.com",
				},
				Turnstile: config.TurnstileConfig{
					SecretKey: "turnstile-secret",
				},
			},
			expectError: true,
			errorMsg:    "POSTHOG_API_KEY is required",
		},
		{
			name: "valid posthog provider config",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Port:           "8081",
					BaseURL:        "https://example.com",
					AllowedOrigins: []string{"https://example.com"},
				},
				Database: config.DatabaseConfig{
					WorkOffline: true,
				},
				Auth: config.AuthConfig{
					InternalMentorsAPI: "test-token",
					MentorsAPIToken:    "public-token",
				},
				Analytics: config.AnalyticsConfig{
					Provider: "posthog",
				},
				PostHog: config.PostHogConfig{
					APIKey: "ph-key",
					Host:   "https://us.i.posthog.com",
				},
				Turnstile: config.TurnstileConfig{
					SecretKey: "turnstile-secret",
				},
			},
			expectError: false,
		},
		{
			name: "missing database url",
			cfg: &config.Config{
				Database: config.DatabaseConfig{
					WorkOffline: false,
				},
				Auth: config.AuthConfig{
					InternalMentorsAPI: "test-token",
				},
			},
			expectError: true,
			errorMsg:    "DATABASE_URL is required",
		},
		{
			name: "missing internal API token",
			cfg: &config.Config{
				Database: config.DatabaseConfig{
					WorkOffline: true,
				},
				Auth: config.AuthConfig{},
			},
			expectError: true,
			errorMsg:    "INTERNAL_MENTORS_API is required",
		},
		{
			name: "profiling enabled without endpoint",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Port:           "8081",
					BaseURL:        "https://example.com",
					AllowedOrigins: []string{"https://example.com"},
				},
				Database: config.DatabaseConfig{
					WorkOffline: true,
				},
				Auth: config.AuthConfig{
					InternalMentorsAPI: "test-token",
					MentorsAPIToken:    "public-token",
				},
				Turnstile: config.TurnstileConfig{
					SecretKey: "turnstile-secret",
				},
				Profiling: config.ProfilingConfig{
					Enabled: true,
				},
			},
			expectError: true,
			errorMsg:    "O11Y_PROFILING_ENDPOINT is required",
		},
		{
			name: "profiling enabled with endpoint",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Port:           "8081",
					BaseURL:        "https://example.com",
					AllowedOrigins: []string{"https://example.com"},
				},
				Database: config.DatabaseConfig{
					WorkOffline: true,
				},
				Auth: config.AuthConfig{
					InternalMentorsAPI: "test-token",
					MentorsAPIToken:    "public-token",
				},
				Turnstile: config.TurnstileConfig{
					SecretKey: "turnstile-secret",
				},
				Profiling: config.ProfilingConfig{
					Enabled:  true,
					Endpoint: "http://alloy:4040",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoad_WithDefaults(t *testing.T) {
	// Clean environment
	os.Clearenv()

	// Set only required fields (APP_ENV defaults to production, which also
	// requires JWT_SECRET >= 32 chars and WORKER_AUTH_TOKEN).
	os.Setenv("DB_WORK_OFFLINE", "true")
	os.Setenv("INTERNAL_MENTORS_API", "test-token")
	os.Setenv("MENTORS_API_LIST_AUTH_TOKEN", "public-token")
	os.Setenv("TURNSTILE_SECRET_KEY", "turnstile-secret")
	os.Setenv("JWT_SECRET", "test-jwt-secret-at-least-32-chars-long")
	os.Setenv("WORKER_AUTH_TOKEN", "worker-secret")

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
