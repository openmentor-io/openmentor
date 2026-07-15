package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
)

// getmentorSlugRe matches the getmentor.dev slug shape:
// transliterated-name-legacyid, lowercase Latin with dashes
// (e.g. "ivan-petrov-42").
var getmentorSlugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// MigrationIntentService records getmentor.dev migration opt-ins from the
// public /migrate page (DECISIONS D22).
type MigrationIntentService struct {
	repo            *repository.MigrationIntentRepository
	captchaVerifier *turnstile.Verifier
	tracker         analytics.Tracker
}

// NewMigrationIntentService creates a new migration intent service.
func NewMigrationIntentService(
	repo *repository.MigrationIntentRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *MigrationIntentService {
	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}
	return &MigrationIntentService{
		repo:            repo,
		captchaVerifier: turnstile.NewVerifier(cfg.Turnstile.SecretKey, httpClient),
		tracker:         tracker,
	}
}

// ScheduleMigration verifies the captcha and records the intent. Slugs are
// not checked against the getmentor.dev database here (the API has no
// access to it); the migration tooling reports unknown slugs when it runs.
func (s *MigrationIntentService) ScheduleMigration(ctx context.Context, req *models.ScheduleMigrationRequest) (*models.ScheduleMigrationResponse, error) {
	slug := strings.TrimSpace(strings.ToLower(req.Slug))

	track := func(outcome string) {
		s.tracker.Track(ctx, analytics.EventMigrationIntentScheduled, analytics.SystemDistinctID("migration-intent"), map[string]interface{}{
			"slug":    slug,
			"outcome": outcome,
		})
	}

	if err := s.captchaVerifier.Verify(req.CaptchaToken); err != nil {
		track("captcha_failed")
		logger.Warn("Turnstile verification failed for migration intent", zap.Error(err))
		return &models.ScheduleMigrationResponse{
			Success: false,
			Error:   "Captcha verification failed",
		}, fmt.Errorf("captcha verification failed: %w", err)
	}

	if !getmentorSlugRe.MatchString(slug) {
		track("invalid_slug")
		return &models.ScheduleMigrationResponse{
			Success: false,
			Error:   "That doesn't look like a getmentor.dev profile link",
		}, fmt.Errorf("invalid getmentor slug format")
	}

	created, err := s.repo.Create(ctx, slug)
	if err != nil {
		track("db_error")
		logger.Error("Failed to record migration intent", zap.String("slug", slug), zap.Error(err))
		return &models.ScheduleMigrationResponse{
			Success: false,
			Error:   "Failed to schedule the migration",
		}, err
	}

	if created {
		track("scheduled")
		logger.Info("Migration intent recorded", zap.String("slug", slug))
	} else {
		track("already_scheduled")
	}
	return &models.ScheduleMigrationResponse{Success: true, AlreadyScheduled: !created}, nil
}
