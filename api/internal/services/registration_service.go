package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"github.com/openmentor-io/openmentor/api/pkg/slug"
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

// RegistrationMentorRepository is the mentor repository surface
// RegistrationService needs. *repository.MentorRepository satisfies it;
// tests substitute a fake (mirrors ProfileMentorRepository).
type RegistrationMentorRepository interface {
	GetTagIDByName(ctx context.Context, tagName string) (string, error)
	CreateMentor(ctx context.Context, fields map[string]interface{}) (string, int, string, error)
	UpdateMentorTags(ctx context.Context, mentorID string, tagIDs []string) error
}

var _ RegistrationMentorRepository = (*repository.MentorRepository)(nil)

// RegistrationService handles mentor registration
type RegistrationService struct {
	mentorRepo      RegistrationMentorRepository
	storageClient   *s3storage.StorageClient
	config          *config.Config
	httpClient      httpclient.Client
	captchaVerifier *turnstile.Verifier
	tracker         analytics.Tracker
}

// NewRegistrationService creates a new registration service instance
func NewRegistrationService(
	mentorRepo RegistrationMentorRepository,
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
		captchaVerifier: newCaptchaVerifier(cfg, httpClient),
		tracker:         tracker,
	}
}

// resolveProfilePhoto vets the submitted profile picture BEFORE the mentor row
// is written: storage must exist, and the image must pass validation and
// classification. Returns the prepared photo (nil when none was submitted), or
// the rejection response together with its error.
//
// Both checks have to precede the INSERT because the upload itself is fired
// into a detached goroutine once the row has committed — too late to tell the
// registrant anything, and a nil storage client there takes the process down
// (recover is per-goroutine, so RecoveryMiddleware never sees it).
func (s *RegistrationService) resolveProfilePhoto(
	ctx context.Context,
	req *models.RegisterMentorRequest,
	baseProperties map[string]interface{},
) (*preparedPhoto, *models.RegisterMentorResponse, error) {

	if req.ProfilePicture.Image == "" {
		return nil, nil, nil
	}

	if s.storageClient == nil {
		metrics.MentorRegistrations.WithLabelValues("uploads_unavailable").Inc()
		s.tracker.Track(ctx, analytics.EventMentorRegistrationSubmitted, analytics.SystemDistinctID("api"),
			registrationProperties(baseProperties, "uploads_unavailable"))
		logger.Error("Registration with a photo rejected: object storage is not configured")
		return nil, &models.RegisterMentorResponse{
			Success: false,
			Error:   "Profile picture uploads are temporarily unavailable — please try again later",
			Reason:  "uploads_unavailable",
		}, ErrUploadsUnavailable
	}

	photo, err := preparePhoto(ctx, req.ProfilePicture.Image, req.ProfilePicture.ContentType)
	if err != nil {
		metrics.MentorRegistrations.WithLabelValues("invalid_photo").Inc()
		s.tracker.Track(ctx, analytics.EventMentorRegistrationSubmitted, analytics.SystemDistinctID("api"),
			registrationProperties(baseProperties, "invalid_photo"))
		logger.Warn("Registration photo rejected", zap.Error(err))
		return nil, &models.RegisterMentorResponse{
			Success: false,
			Error:   err.Error(),
			Reason:  "invalid_photo",
		}, err
	}

	return photo, nil, nil
}

// registrationProperties copies the shared registration analytics properties
// and stamps the outcome on the copy.
func registrationProperties(base map[string]interface{}, outcome string) map[string]interface{} {
	props := make(map[string]interface{}, len(base)+1)
	for key, value := range base {
		props[key] = value
	}
	props["outcome"] = outcome
	return props
}

// RegisterMentor handles the complete mentor registration flow
func (s *RegistrationService) RegisterMentor(ctx context.Context, req *models.RegisterMentorRequest) (*models.RegisterMentorResponse, error) {
	baseProperties := map[string]interface{}{
		"tags_count":          len(req.Tags),
		"has_calendar_url":    strings.TrimSpace(req.CalendarURL) != "",
		"has_profile_picture": req.ProfilePicture.Image != "",
	}

	// 1. Verify captcha (Cloudflare Turnstile)
	if err := s.captchaVerifier.Verify(ctx, req.CaptchaToken); err != nil {
		metrics.MentorRegistrations.WithLabelValues("captcha_failed").Inc()
		s.tracker.Track(ctx, analytics.EventMentorRegistrationSubmitted, analytics.SystemDistinctID("api"),
			registrationProperties(baseProperties, "captcha_failed"))
		logger.Warn("Turnstile verification failed", zap.Error(err))
		return &models.RegisterMentorResponse{
			Success: false,
			Error:   "Captcha verification failed",
		}, fmt.Errorf("captcha verification failed: %w", err)
	}

	// 2. Resolve the submitted photo. It has to happen before the INSERT (see
	// resolveProfilePhoto) but never before the captcha: decoding the image is
	// the most expensive thing this endpoint does, and running it first would
	// make it an unauthenticated image-decode endpoint.
	photo, photoResp, photoErr := s.resolveProfilePhoto(ctx, req, baseProperties)
	if photoErr != nil {
		return photoResp, photoErr
	}

	// 3. Validate the chosen username (optional; the frontend pre-checks
	// availability, but the DB unique constraint in CreateMentor is the
	// authoritative backstop against races).
	username := slug.NormalizeUsername(req.Username)
	if username != "" {
		if err := slug.ValidateUsername(username); err != nil {
			metrics.MentorRegistrations.WithLabelValues("invalid_username").Inc()
			return &models.RegisterMentorResponse{
				Success: false,
				Error:   fmt.Sprintf("Invalid username: %s", err.Error()),
				Reason:  "username_invalid",
			}, fmt.Errorf("invalid username: %w", err)
		}
	}

	// Trim the optional free-text contact details
	contact := strings.TrimSpace(req.PreferredContact)

	// 4. Get tag IDs for selected tags
	var tagIDs []string
	for _, tagName := range req.Tags {
		tagID, err := s.mentorRepo.GetTagIDByName(ctx, tagName)
		if err == nil && tagID != "" {
			tagIDs = append(tagIDs, tagID)
		} else {
			logger.Warn("Tag not found", zap.String("tag_name", tagName))
		}
	}

	// 5. Create mentor record in PostgreSQL
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
		"photo_style":       photo.styleOrDefault(),
	}

	if req.CalendarURL != "" {
		fields["calendar_url"] = req.CalendarURL
	}

	if username != "" {
		fields["slug"] = username
	}

	// Note: Tags will be inserted separately into mentor_tags table
	// This is handled by the repository CreateMentor method

	mentorID, legacyID, mentorSlug, createErr := s.mentorRepo.CreateMentor(ctx, fields)
	if createErr != nil {
		if errors.Is(createErr, repository.ErrSlugTaken) {
			metrics.MentorRegistrations.WithLabelValues("username_taken").Inc()
			return &models.RegisterMentorResponse{
				Success: false,
				Error:   "This username is already taken — please pick another one",
				Reason:  "username_taken",
			}, fmt.Errorf("username taken: %w", createErr)
		}
		metrics.MentorRegistrations.WithLabelValues("db_error").Inc()
		s.tracker.Track(ctx, analytics.EventMentorRegistrationSubmitted, analytics.SystemDistinctID("api"),
			registrationProperties(baseProperties, "db_error"))
		logger.Error("Failed to create mentor in database", zap.Error(createErr))
		return &models.RegisterMentorResponse{
			Success: false,
			Error:   "Failed to create mentor profile",
		}, fmt.Errorf("failed to create mentor: %w", createErr)
	}

	logger.Info("Mentor created in database",
		zap.String("mentor_id", mentorID),
		zap.Int("legacy_id", legacyID),
		zap.String("slug", mentorSlug),
		zap.String("email", req.Email))

	// Set mentor tags if any were provided
	if len(tagIDs) > 0 {
		if err := s.mentorRepo.UpdateMentorTags(ctx, mentorID, tagIDs); err != nil {
			logger.Error("Failed to set mentor tags", zap.Error(err))
			// Don't fail registration if tags fail - continue
		}
	}

	// 6. Upload the already-validated photo bytes (non-blocking on failure).
	// Images are keyed by the mentor UUID, not the slug — usernames are
	// changeable.
	if photo != nil {
		s.storageClient.UploadProfileImageAsync(ctx, photo.bytes, photo.contentType, mentorID)
	}

	// 7. Trigger mentor created webhook (non-blocking)
	trigger.CallAsync(ctx, s.config.EventTriggers.MentorCreatedTriggerURL, mentorID, s.config.Worker.AuthToken, s.httpClient)

	metrics.MentorRegistrations.WithLabelValues("success").Inc()
	successProperties := registrationProperties(baseProperties, registrationOutcomeSuccess)
	successProperties["mentor_id"] = mentorID
	successProperties["legacy_mentor_id"] = legacyID
	successProperties["status"] = registrationStatusDraft
	s.tracker.Track(ctx, analytics.EventMentorRegistrationSubmitted, analytics.MentorDistinctID(mentorID), successProperties)

	return &models.RegisterMentorResponse{
		Success:  true,
		Message:  "Registration successful. We'll review your application and contact you soon.",
		MentorID: legacyID, // Return legacy ID for backwards compatibility
	}, nil
}
