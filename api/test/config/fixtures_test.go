package config_test

import "github.com/openmentor-io/openmentor/api/config"

// The three builders below return a configuration that PASSES its binary's
// validator in the given environment. Every case in this package starts from one
// of them and blanks or breaks exactly one field, so a case can only fail for the
// reason it names — the previous style (a hand-written literal per case) meant
// adding a required setting silently broke a dozen unrelated cases.
//
// Secrets are obvious placeholders. No test in this package prints a value.

// apiConfig is a complete cmd/api configuration.
func apiConfig(appEnv string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			AppEnv:         appEnv,
			Port:           "8081",
			BaseURL:        "https://openmentor.example",
			AllowedOrigins: []string{"https://openmentor.example"},
		},
		Database: config.DatabaseConfig{URL: "postgres://u:p@postgres:5432/db"},
		Auth: config.AuthConfig{
			InternalMentorsAPI: "internal-token",
			MentorsAPIToken:    "public-token",
		},
		Turnstile: config.TurnstileConfig{SecretKey: "turnstile-secret"},
		MentorSession: config.MentorSessionConfig{
			JWTSecret:            "test-jwt-secret-at-least-32-chars-long",
			JWTIssuer:            "openmentor-api",
			SessionTTLHours:      24,
			LoginTokenTTLMinutes: 15,
			CookieSecure:         true,
		},
		Worker:    config.WorkerConfig{AuthToken: "worker-secret", Port: "8090"},
		S3Storage: completeS3,
		EventTriggers: config.EventTriggerFunctionsConfig{
			MentorCreatedTriggerURL:          "http://worker:8090/jobs/new-mentor-watcher?mentorId=",
			MentorRequestCreatedTriggerURL:   "http://worker:8090/jobs/new-request-watcher?requestId=",
			MentorLoginEmailTriggerURL:       "http://worker:8090/jobs/mentor-login-email",
			ModeratorLoginEmailTriggerURL:    "http://worker:8090/jobs/moderator-login-email",
			MentorModerationTriggerURL:       "http://worker:8090/jobs/mentor-moderation-action",
			RequestProcessFinishedTriggerURL: "http://worker:8090/jobs/request-process-finished?requestId=",
			ReviewCreatedTriggerURL:          "http://worker:8090/jobs/process-mentee-review?reviewId=",
			// MENTOR_UPDATED_TRIGGER_URL is deliberately unset in every
			// environment: the worker serves no such job.
		},
	}
}

// workerConfig is a complete cmd/worker configuration. Note what is absent and
// stays absent: no S3 block, no Turnstile secret, no mentors-API tokens, no JWT
// secret, no trigger URLs (the worker SERVES those routes).
func workerConfig(appEnv string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			AppEnv:  appEnv,
			BaseURL: "https://openmentor.example",
		},
		Database: config.DatabaseConfig{URL: "postgres://u:p@postgres:5432/db"},
		Worker: config.WorkerConfig{
			Port:        "8090",
			AuthToken:   "worker-secret",
			ServiceName: "openmentor-worker",
		},
		Email: config.EmailConfig{
			SESRegion:          "eu-central-1",
			SESAccessKeyID:     "ses-key-id",
			SESSecretAccessKey: "ses-secret",
			ModeratorsEmail:    "moderators@openmentor.example",
		},
	}
}

// migrateConfig is a complete cmd/migrate configuration: the DSN, and nothing
// else. This is the whole point of H14 — see a57aec2.
func migrateConfig(appEnv string) *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{AppEnv: appEnv},
		Database: config.DatabaseConfig{URL: "postgres://u:p@postgres:5432/db"},
	}
}

var completeS3 = config.S3StorageConfig{
	AccessKeyID:     "key",
	SecretAccessKey: "secret",
	BucketName:      "mentor-images",
	Endpoint:        "https://s3.eu-central-1.amazonaws.com",
	Region:          "eu-central-1",
}

// productionTriggerURLs mirrors infra/.env.production.example. Every one of the
// worker jobs the API fires at; MENTOR_UPDATED_TRIGGER_URL is absent because the
// worker serves no such job.
var productionTriggerURLs = map[string]string{
	"MENTOR_CREATED_TRIGGER_URL":           "http://worker:8090/jobs/new-mentor-watcher?mentorId=",
	"MENTOR_REQUEST_CREATED_TRIGGER_URL":   "http://worker:8090/jobs/new-request-watcher?requestId=",
	"MENTOR_LOGIN_EMAIL_TRIGGER_URL":       "http://worker:8090/jobs/mentor-login-email",
	"MODERATOR_LOGIN_EMAIL_TRIGGER_URL":    "http://worker:8090/jobs/moderator-login-email",
	"MENTOR_MODERATION_TRIGGER_URL":        "http://worker:8090/jobs/mentor-moderation-action",
	"REQUEST_PROCESS_FINISHED_TRIGGER_URL": "http://worker:8090/jobs/request-process-finished?requestId=",
	"REVIEW_CREATED_TRIGGER_URL":           "http://worker:8090/jobs/process-mentee-review?reviewId=",
}
