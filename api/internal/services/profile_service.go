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
	apperrors "github.com/openmentor-io/openmentor/api/pkg/errors"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"go.uber.org/zap"
)

// ErrProfileStatusNotToggleable is returned when a mentor whose profile is not
// yet approved (draft/pending) or was declined tries to change visibility status.
var ErrProfileStatusNotToggleable = errors.New("only active or inactive profiles can change visibility status")

// ErrProfileNotSubmittable is returned when a mentor tries to submit a
// profile for review that is not in 'draft' status.
var ErrProfileNotSubmittable = errors.New("only draft profiles can be submitted for review")

// ProfileMentorRepository defines the mentor repository methods used by ProfileService.
// *repository.MentorRepository satisfies this interface.
type ProfileMentorRepository interface {
	GetByMentorId(ctx context.Context, mentorID string, opts models.FilterOptions) (*models.Mentor, error)
	GetTagIDByName(ctx context.Context, tagName string) (string, error)
	Update(ctx context.Context, mentorID string, updates map[string]interface{}) error
	UpdateMentorTags(ctx context.Context, mentorID string, tagIDs []string) error
	TouchUpdatedAt(ctx context.Context, mentorID string) error
	SetMentorStatus(ctx context.Context, mentorID, status string) error
}

var _ ProfileMentorRepository = (*repository.MentorRepository)(nil)

type ProfileService struct {
	mentorRepo    ProfileMentorRepository
	storageClient *s3storage.StorageClient
	config        *config.Config
	httpClient    httpclient.Client
	tracker       analytics.Tracker
}

func NewProfileService(
	mentorRepo ProfileMentorRepository,
	storageClient *s3storage.StorageClient,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *ProfileService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &ProfileService{
		mentorRepo:    mentorRepo,
		storageClient: storageClient,
		config:        cfg,
		httpClient:    httpClient,
		tracker:       tracker,
	}
}

// SaveProfileByMentorId updates a mentor's profile using Mentor ID (UUID) for session-based auth
func (s *ProfileService) SaveProfileByMentorId(ctx context.Context, mentorID string, req *models.SaveProfileRequest) error {
	// Ensure the mentor exists before applying updates (AllowAnyStatus:
	// draft/pending mentors edit their own profile too)
	if _, err := s.mentorRepo.GetByMentorId(ctx, mentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true}); err != nil {
		s.tracker.Track(ctx, analytics.EventMentorProfileUpdated, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id": mentorID,
			"outcome":   "mentor_not_found",
		})
		return apperrors.NotFoundError("mentor")
	}

	// Get tag IDs
	tagIDs := []string{}
	for _, tagName := range req.Tags {
		tagID, tagErr := s.mentorRepo.GetTagIDByName(ctx, tagName)
		if tagErr == nil && tagID != "" {
			tagIDs = append(tagIDs, tagID)
		}
	}

	// Prepare updates with PostgreSQL column names
	updates := map[string]interface{}{
		"name":         req.Name,
		"job_title":    req.Job,
		"workplace":    req.Workplace,
		"experience":   req.Experience,
		"price":        req.Price,
		"details":      req.Description,
		"about":        req.About,
		"competencies": req.Competencies,
	}

	if req.CalendarURL != "" {
		updates["calendar_url"] = req.CalendarURL
	}

	// Update in database
	if err := s.mentorRepo.Update(ctx, mentorID, updates); err != nil {
		metrics.ProfileUpdates.WithLabelValues("error").Inc()
		s.tracker.Track(ctx, analytics.EventMentorProfileUpdated, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":  mentorID,
			"tags_count": len(tagIDs),
			"outcome":    "update_failed",
		})
		logger.Error("Failed to update mentor profile",
			zap.Error(err),
			zap.String("mentor_id", mentorID))
		return fmt.Errorf("failed to update profile")
	}

	// Update tags in mentor_tags table
	if err := s.mentorRepo.UpdateMentorTags(ctx, mentorID, tagIDs); err != nil {
		logger.Error("Failed to update mentor tags",
			zap.Error(err),
			zap.String("mentor_id", mentorID))
		// Don't fail the whole update if tags fail - log and continue
	}

	metrics.ProfileUpdates.WithLabelValues("success").Inc()
	s.tracker.Track(ctx, analytics.EventMentorProfileUpdated, analytics.MentorDistinctID(mentorID), map[string]interface{}{
		"mentor_id":        mentorID,
		"tags_count":       len(tagIDs),
		"has_calendar_url": strings.TrimSpace(req.CalendarURL) != "",
		"outcome":          "success",
	})
	logger.Info("Mentor profile updated via session",
		zap.String("mentor_id", mentorID))

	return nil
}

// UploadPictureByMentorId uploads a profile picture using Mentor ID (UUID) for session-based auth
func (s *ProfileService) UploadPictureByMentorId(ctx context.Context, mentorID string, mentorSlug string, req *models.UploadProfilePictureRequest) (string, error) {
	// Upload to S3-compatible object storage in 3 sizes: full, large, small (synchronous)
	// Validation (type and size) is handled automatically by UploadImageAllSizes
	fullImageURL, err := s.storageClient.UploadImageAllSizes(ctx, req.Image, mentorSlug, req.ContentType)
	if err != nil {
		metrics.ProfilePictureUploads.WithLabelValues("error").Inc()
		s.tracker.Track(ctx, analytics.EventMentorProfilePictureUploaded, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":    mentorID,
			"content_type": req.ContentType,
			"outcome":      "upload_failed",
		})
		logger.Error("Failed to upload profile picture to storage",
			zap.Error(err),
			zap.String("mentor_id", mentorID))
		return "", fmt.Errorf("failed to upload image")
	}

	// TODO: Re-enable webhook trigger for thumbnail generation or remove this dead goroutine
	// Update database asynchronously
	// go func() {
	//	 // This webhook will trigger Azure Function to generate thumbnails
	//	 // trigger.CallAsync(ctx, s.config.EventTriggers.MentorUpdatedTriggerURL, mentorID, s.config.Worker.AuthToken, s.httpClient)
	//	 _ = s.config.EventTriggers.MentorUpdatedTriggerURL // Keep for future use
	//	 _ = s.httpClient                                   // Keep for future use
	//	 _ = trigger.CallAsync                              // Keep for future use
	// }()

	// Determine the photo display style. With the cutout service configured
	// this removes the background, quality-gates it, uploads a <slug>/hero
	// alpha PNG and stores 'hero'; otherwise it falls back to the
	// border-luminance classifier. Best-effort — never fails the upload.
	photoStyle := applyPhotoStyle(ctx, s.config, s.storageClient, s.mentorRepo, mentorID, mentorSlug, req.Image)

	if err := s.mentorRepo.TouchUpdatedAt(ctx, mentorID); err != nil {
		logger.Error("Failed to touch updated_at after picture upload",
			zap.Error(err),
			zap.String("mentor_id", mentorID))
	}

	metrics.ProfilePictureUploads.WithLabelValues("success").Inc()
	s.tracker.Track(ctx, analytics.EventMentorProfilePictureUploaded, analytics.MentorDistinctID(mentorID), map[string]interface{}{
		"mentor_id":    mentorID,
		"content_type": req.ContentType,
		"photo_style":  photoStyle,
		"url_returned": strings.TrimSpace(fullImageURL) != "",
		"outcome":      "success",
	})
	logger.Info("Profile picture uploaded via session",
		zap.String("mentor_id", mentorID),
		zap.String("url", fullImageURL))

	return fullImageURL, nil
}

// SetProfileStatusByMentorId toggles the mentor's own catalog visibility between
// 'active' and 'inactive'. Only mentors whose current status is already active or
// inactive may toggle — pending/declined profiles are rejected with
// ErrProfileStatusNotToggleable (mirrors the admin status-change rules).
func (s *ProfileService) SetProfileStatusByMentorId(ctx context.Context, mentorID string, status string) error {
	trackStatusChange := func(fromStatus string, outcome string) {
		properties := map[string]interface{}{
			"mentor_id": mentorID,
			"status":    status,
			"outcome":   outcome,
		}
		if fromStatus != "" {
			properties["from_status"] = fromStatus
		}
		s.tracker.Track(ctx, analytics.EventMentorProfileStatusChanged, analytics.MentorDistinctID(mentorID), properties)
	}

	if status != mentorStatusActive && status != mentorStatusInactive {
		trackStatusChange("", "unsupported_status")
		return apperrors.InvalidInputError("status", "must be active or inactive")
	}

	// AllowAnyStatus so draft/pending/declined mentors get the explicit
	// "not toggleable" rejection below instead of a generic not-found.
	mentor, err := s.mentorRepo.GetByMentorId(ctx, mentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true})
	if err != nil {
		trackStatusChange("", "mentor_not_found")
		return apperrors.NotFoundError("mentor")
	}

	if mentor.Status != mentorStatusActive && mentor.Status != mentorStatusInactive {
		trackStatusChange(mentor.Status, "invalid_transition")
		return ErrProfileStatusNotToggleable
	}

	if err := s.mentorRepo.SetMentorStatus(ctx, mentorID, status); err != nil {
		trackStatusChange(mentor.Status, "update_failed")
		logger.Error("Failed to update mentor profile status",
			zap.Error(err),
			zap.String("mentor_id", mentorID),
			zap.String("status", status))
		return fmt.Errorf("failed to update profile status")
	}

	// Notify downstream consumers about the profile update (async, non-blocking)
	trigger.CallAsync(ctx, s.config.EventTriggers.MentorUpdatedTriggerURL, mentorID, s.config.Worker.AuthToken, s.httpClient)

	trackStatusChange(mentor.Status, "success")
	logger.Info("Mentor profile status updated via session",
		zap.String("mentor_id", mentorID),
		zap.String("from_status", mentor.Status),
		zap.String("status", status))

	return nil
}

// SubmitProfileByMentorId resubmits a returned (draft) profile for review:
// draft -> pending, then the mentor-confirmed worker job notifies the
// moderators and sends the mentor the "in review" email. The moderation
// note is intentionally KEPT until approve so the mentor can still see what
// was asked. Only valid from 'draft'.
func (s *ProfileService) SubmitProfileByMentorId(ctx context.Context, mentorID string) error {
	track := func(fromStatus, outcome string) {
		properties := map[string]interface{}{
			"mentor_id": mentorID,
			"outcome":   outcome,
		}
		if fromStatus != "" {
			properties["from_status"] = fromStatus
		}
		s.tracker.Track(ctx, analytics.EventMentorProfileResubmitted, analytics.MentorDistinctID(mentorID), properties)
	}

	mentor, err := s.mentorRepo.GetByMentorId(ctx, mentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true})
	if err != nil {
		track("", "mentor_not_found")
		return apperrors.NotFoundError("mentor")
	}

	if mentor.Status != mentorStatusDraft {
		track(mentor.Status, "invalid_transition")
		return ErrProfileNotSubmittable
	}

	if err := s.mentorRepo.SetMentorStatus(ctx, mentorID, mentorStatusPending); err != nil {
		track(mentor.Status, "update_failed")
		logger.Error("Failed to resubmit mentor profile",
			zap.Error(err),
			zap.String("mentor_id", mentorID))
		return fmt.Errorf("failed to submit profile for review")
	}

	// Notify moderators + send the mentor the "in review" email (same
	// worker job as the email-confirmation step).
	trigger.CallAsync(ctx, s.config.EventTriggers.MentorConfirmedTriggerURL(), mentorID, s.config.Worker.AuthToken, s.httpClient)

	track(mentor.Status, "success")
	logger.Info("Mentor profile resubmitted for review",
		zap.String("mentor_id", mentorID))

	return nil
}
