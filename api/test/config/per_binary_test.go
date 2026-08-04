package config_test

import (
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is the regression test for a57aec2, the 2026-08-04 production
// outage: an S3 check added to the ONE shared Validate() made cmd/migrate exit 1
// at startup, and `depends_on: service_completed_successfully` then held the
// whole stack in Created while the site served 404s. The infra fix was to hand S3
// credentials to two binaries that never open a bucket, which also widened the
// per-service secret allowlist P10 had just narrowed.
//
// So the property under test is not "validation works". It is: a setting only
// one binary uses must be required by only that binary's validator.

// validators is the matrix. Each row is one setting, the binaries whose contract
// includes it, and how to remove it from an otherwise-valid config.
type contractCase struct {
	// setting is the env var name, for the failure message.
	setting string
	// required lists the binaries that must reject the config without it.
	required []config.Binary
	// remove blanks the setting.
	remove func(*config.Config)
	// appEnv defaults to production when empty.
	appEnv string
}

func validateFor(t *testing.T, binary config.Binary, cfg *config.Config) error {
	t.Helper()
	switch binary {
	case config.BinaryAPI:
		return cfg.ValidateForAPI()
	case config.BinaryWorker:
		return cfg.ValidateForWorker()
	case config.BinaryMigrate:
		return cfg.ValidateForMigrate()
	}
	t.Fatalf("no validator for binary %q", binary)
	return nil
}

func configFor(t *testing.T, binary config.Binary, appEnv string) *config.Config {
	t.Helper()
	switch binary {
	case config.BinaryAPI:
		return apiConfig(appEnv)
	case config.BinaryWorker:
		return workerConfig(appEnv)
	case config.BinaryMigrate:
		return migrateConfig(appEnv)
	}
	t.Fatalf("no fixture for binary %q", binary)
	return nil
}

var allBinaries = []config.Binary{config.BinaryAPI, config.BinaryWorker, config.BinaryMigrate}

// TestPerBinaryContracts asserts, for every setting, that exactly the binaries
// which USE it reject its absence. The negative half is the one that matters:
// removing S3 must leave worker and migrate booting.
func TestPerBinaryContracts(t *testing.T) {
	cases := []contractCase{
		{
			setting:  "S3_STORAGE_* (the a57aec2 outage)",
			required: []config.Binary{config.BinaryAPI},
			remove:   func(c *config.Config) { c.S3Storage = config.S3StorageConfig{} },
		},
		{
			setting:  "TURNSTILE_SECRET_KEY",
			required: []config.Binary{config.BinaryAPI},
			remove:   func(c *config.Config) { c.Turnstile.SecretKey = "" },
		},
		{
			setting:  "INTERNAL_MENTORS_API",
			required: []config.Binary{config.BinaryAPI},
			remove:   func(c *config.Config) { c.Auth.InternalMentorsAPI = "" },
		},
		{
			setting:  "MENTORS_API_LIST_AUTH_TOKEN",
			required: []config.Binary{config.BinaryAPI},
			remove:   func(c *config.Config) { c.Auth.MentorsAPIToken = "" },
		},
		{
			setting:  "JWT_SECRET",
			required: []config.Binary{config.BinaryAPI},
			remove:   func(c *config.Config) { c.MentorSession.JWTSecret = "" },
		},
		{
			setting:  "ALLOWED_CORS_ORIGINS",
			required: []config.Binary{config.BinaryAPI},
			remove:   func(c *config.Config) { c.Server.AllowedOrigins = nil },
		},
		{
			setting:  "PORT",
			required: []config.Binary{config.BinaryAPI},
			remove:   func(c *config.Config) { c.Server.Port = "" },
		},
		{
			setting:  "the event trigger URLs",
			required: []config.Binary{config.BinaryAPI},
			remove: func(c *config.Config) {
				c.EventTriggers = config.EventTriggerFunctionsConfig{}
			},
		},
		{
			setting:  "SES_*",
			required: []config.Binary{config.BinaryWorker},
			remove: func(c *config.Config) {
				c.Email.SESRegion, c.Email.SESAccessKeyID, c.Email.SESSecretAccessKey = "", "", ""
			},
		},
		{
			setting:  "MODERATORS_EMAIL",
			required: []config.Binary{config.BinaryWorker},
			remove:   func(c *config.Config) { c.Email.ModeratorsEmail = "" },
		},
		{
			setting:  "WORKER_PORT",
			required: []config.Binary{config.BinaryWorker},
			remove:   func(c *config.Config) { c.Worker.Port = "" },
		},
		{
			// BASE_URL is the one server setting the worker does read: every
			// email link is rendered from it.
			setting:  "BASE_URL",
			required: []config.Binary{config.BinaryAPI, config.BinaryWorker},
			remove:   func(c *config.Config) { c.Server.BaseURL = "" },
		},
		{
			// Shared because the API is the caller that must present the token
			// and the worker is the server that checks it.
			setting:  "WORKER_AUTH_TOKEN",
			required: []config.Binary{config.BinaryAPI, config.BinaryWorker},
			remove:   func(c *config.Config) { c.Worker.AuthToken = "" },
		},
		{
			setting:  "DATABASE_URL",
			required: allBinaries,
			remove:   func(c *config.Config) { c.Database.URL = "" },
		},
	}

	for _, tc := range cases {
		t.Run(tc.setting, func(t *testing.T) {
			appEnv := tc.appEnv
			if appEnv == "" {
				appEnv = "production"
			}
			requiredBy := map[config.Binary]bool{}
			for _, b := range tc.required {
				requiredBy[b] = true
			}

			for _, binary := range allBinaries {
				t.Run(string(binary), func(t *testing.T) {
					cfg := configFor(t, binary, appEnv)
					require.NoError(t, validateFor(t, binary, cfg),
						"the fixture itself must be valid before the case breaks it")

					tc.remove(cfg)
					err := validateFor(t, binary, cfg)
					if requiredBy[binary] {
						assert.Error(t, err, "cmd/%s uses %s and must refuse to start without it",
							binary, tc.setting)
						return
					}
					assert.NoError(t, err,
						"cmd/%s does not use %s, so its absence must not stop the binary booting "+
							"(this is the a57aec2 class of defect)", binary, tc.setting)
				})
			}
		})
	}
}

// TestMigrateNeedsOnlyTheDSN pins migrate's contract as a whole rather than
// setting by setting: an empty Config plus a DSN must be enough. Any future
// check that reaches beyond the database fails here, which is the check that
// would have caught the outage on the branch that caused it.
func TestMigrateNeedsOnlyTheDSN(t *testing.T) {
	cfg := &config.Config{
		Server:   config.ServerConfig{AppEnv: "production"},
		Database: config.DatabaseConfig{URL: "postgres://u:p@postgres:5432/db"},
	}
	assert.NoError(t, cfg.ValidateForMigrate())

	cfg.Database.URL = ""
	assert.ErrorContains(t, cfg.ValidateForMigrate(), "DATABASE_URL")
}

// TestMigrateIgnoresWorkOffline: DB_WORK_OFFLINE cannot make migrations run
// against nothing, so honoring it here would turn a missing DSN into a deploy
// that reports success and applies no migrations.
func TestMigrateIgnoresWorkOffline(t *testing.T) {
	cfg := migrateConfig("production")
	cfg.Database.URL = ""
	cfg.Database.WorkOffline = true
	assert.ErrorContains(t, cfg.ValidateForMigrate(), "DATABASE_URL is required to run migrations")
}

// TestLoadForRejectsUnknownBinary keeps the dispatch honest: a new binary added
// without a contract must fail loudly rather than inherit "no validation".
func TestLoadForRejectsUnknownBinary(t *testing.T) {
	_, err := config.LoadFor(config.Binary("indexer"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown binary")
}
