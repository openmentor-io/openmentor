package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/imageclass"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
	"go.uber.org/zap"
)

const (
	// New registrations start as 'draft' until the mentor confirms their
	// email address (the new-mentor-watcher worker job sends the
	// confirmation link; the confirm endpoint moves draft -> pending).
	registrationStatusDraft    = "draft"
	registrationOutcomeSuccess = "success"
)

// RegistrationService handles mentor registration
type RegistrationService struct {
	mentorRepo      *repository.MentorRepository
	storageClient   *s3storage.StorageClient
	config          *config.Config
	httpClient      httpclient.Client
	captchaVerifier *turnstile.Verifier
	tracker         analytics.Tracker
}

// NewRegistrationService creates a new registration service instance
func NewRegistrationService(
	mentorRepo *repository.MentorRepository,
	storageClient *s3storage.StorageClient,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *RegistrationService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &RegistrationService{
		mentorRepo:      mentorRepo,
		storageClient:   storageClient,
		config:          cfg,
		httpClient:      httpClient,
		captchaVerifier: turnstile.NewVerifier(cfg.Turnstile.SecretKey, httpClient),
		tracker:         tracker,
	}
}

// classifyPhotoStyle auto-detects the profile picture display style at
// upload time (pkg/imageclass). Classification failures (or no picture)
// fall back to the safe default 'frame'.
func classifyPhotoStyle(imageData string) string {
	if imageData == "" {
		return imageclass.StyleFrame
	}
	style, err := imageclass.ClassifyBase64(imageData)
	if err != nil {
		logger.Warn("Failed to classify profile picture style, defaulting to frame", zap.Error(err))
		return imageclass.StyleFrame
	}
	return style
}

// RegisterMentor handles the complete mentor registration flow
func (s *RegistrationService) RegisterMentor(ctx context.Context, req *models.RegisterMentorRequest) (*models.RegisterMentorResponse, error) {
	baseProperties := map[string]interface{}{
		"tags_count":          len(req.Tags),
		"has_calendar_url":    strings.TrimSpace(req.CalendarURL) != "",
		"has_profile_picture": req.ProfilePicture.Image != "",
	}

	// 1. Verify captcha (Cloudflare Turnstile)
	if err := s.captchaVerifier.Verify(req.CaptchaToken); err != nil {
		metrics.MentorRegistrations.WithLabelValues("captcha_failed").Inc()
		s.tracker.Track(ctx, analytics.EventMentorRegistrationSubmitted, analytics.SystemDistinctID("api"), map[string]interface{}{
			"tags_count":          len(req.Tags),
			"has_calendar_url":    strings.TrimSpace(req.CalendarURL) != "",
			"has_profile_picture": req.ProfilePicture.Image != "",
			"outcome":             "captcha_failed",
		})
		logger.Warn("Turnstile verification failed", zap.Error(err))
		return &models.RegisterMentorResponse{
			Success: false,
			Error:   "Captcha verification failed",
		}, fmt.Errorf("captcha verification failed: %w", err)
	}

	// 2. Trim the optional free-text contact details
	contact := strings.TrimSpace(req.PreferredContact)

	// 3. Get tag IDs for selected tags
	var tagIDs []string
	for _, tagName := range req.Tags {
		tagID, err := s.mentorRepo.GetTagIDByName(ctx, tagName)
		if err == nil && tagID != "" {
			tagIDs = append(tagIDs, tagID)
		} else {
			logger.Warn("Tag not found", zap.String("tag_name", tagName))
		}
	}

	// 4. Create mentor record in PostgreSQL
	fields := map[string]interface{}{
		"name":              strings.TrimSpace(req.Name),
		"email":             req.Email,
		"preferred_contact": contact,
		"job_title":         req.Job,
		"workplace":         req.Workplace,
		"experience":        req.Experience,
		"price":             req.Price,
		"about":             req.About,
		"details":           req.Description,
		"competencies":      req.Competencies,
		"status":            registrationStatusDraft,
		// Start as 'frame'; the async cutout below upgrades to 'hero' if the
		// background removes cleanly (keeps the registration request fast and
		// independent of the cutout sidecar's availability).
		"photo_style": imageclass.StyleFrame,
	}

	if req.CalendarURL != "" {
		fields["calendar_url"] = req.CalendarURL
	}

	// Note: Tags will be inserted separately into mentor_tags table
	// This is handled by the repository CreateMentor method

	mentorID, legacyID, mentorSlug, err := s.mentorRepo.CreateMentor(ctx, fields)
	if err != nil {
		metrics.MentorRegistrations.WithLabelValues("db_error").Inc()
		s.tracker.Track(ctx, analytics.EventMentorRegistrationSubmitted, analytics.SystemDistinctID("api"), map[string]interface{}{
			"tags_count":          len(req.Tags),
			"has_calendar_url":    strings.TrimSpace(req.CalendarURL) != "",
			"has_profile_picture": req.ProfilePicture.Image != "",
			"outcome":             "db_error",
		})
		logger.Error("Failed to create mentor in database", zap.Error(err))
		return &models.RegisterMentorResponse{
			Success: false,
			Error:   "Failed to create mentor profile",
		}, fmt.Errorf("failed to create mentor: %w", err)
	}

	logger.Info("Mentor created in database",
		zap.String("mentor_id", mentorID),
		zap.Int("legacy_id", legacyID),
		zap.String("email", req.Email))

	// Set mentor tags if any were provided
	if len(tagIDs) > 0 {
		if err := s.mentorRepo.UpdateMentorTags(ctx, mentorID, tagIDs); err != nil {
			logger.Error("Failed to set mentor tags", zap.Error(err))
			// Don't fail registration if tags fail - continue
		}
	}

	// 5. Upload profile picture (non-blocking on failure)
	s.storageClient.UploadImageAllSizesAsync(ctx, req.ProfilePicture.Image, mentorSlug, req.ProfilePicture.ContentType, mentorID)

	// 5b. Generate the hero cut-out in the background (background removal +
	// quality gate + <slug>/hero upload + photo_style upgrade). Detached from
	// the request context so it isn't canceled when the response is returned.
	if req.ProfilePicture.Image != "" {
		image := req.ProfilePicture.Image
		bgCtx := context.WithoutCancel(ctx)
		go applyPhotoStyle(bgCtx, s.config, s.storageClient, s.mentorRepo, mentorID, mentorSlug, image)
	}

	// 6. Trigger mentor created webhook (non-blocking)
	trigger.CallAsync(ctx, s.config.EventTriggers.MentorCreatedTriggerURL, mentorID, s.config.Worker.AuthToken, s.httpClient)

	metrics.MentorRegistrations.WithLabelValues("success").Inc()
	successProperties := make(map[string]interface{}, len(baseProperties)+4)
	for key, value := range baseProperties {
		successProperties[key] = value
	}
	successProperties["mentor_id"] = mentorID
	successProperties["legacy_mentor_id"] = legacyID
	successProperties["status"] = registrationStatusDraft
	successProperties["outcome"] = registrationOutcomeSuccess
	s.tracker.Track(ctx, analytics.EventMentorRegistrationSubmitted, analytics.MentorDistinctID(mentorID), successProperties)

	return &models.RegisterMentorResponse{
		Success:  true,
		Message:  "Registration successful. We'll review your application and contact you soon.",
		MentorID: legacyID, // Return legacy ID for backwards compatibility
	}, nil
}
