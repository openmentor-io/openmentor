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

// ErrDeleteConfirmationMismatch is returned when the username typed into the
// deletion dialog does not match the profile being deleted. It is the whole
// point of the typed confirmation: an accidental click cannot destroy a
// profile, because destroying one requires knowing and typing its name.
var ErrDeleteConfirmationMismatch = errors.New("the username entered does not match this profile")

// ErrProfileAlreadyDeleted is returned when a delete lands on a profile that is
// already deleted — a double-submitted dialog, or a second tab.
var ErrProfileAlreadyDeleted = errors.New("this profile has already been deleted")

// ProfileMentorRepository defines the mentor repository methods used by ProfileService.
// *repository.MentorRepository satisfies this interface.
type ProfileMentorRepository interface {
	GetByMentorId(ctx context.Context, mentorID string, opts models.FilterOptions) (*models.Mentor, error)
	GetTagIDByName(ctx context.Context, tagName string) (string, error)
	Update(ctx context.Context, mentorID string, updates map[string]interface{}) error
	UpdateMentorTags(ctx context.Context, mentorID string, tagIDs []string) error
	TouchUpdatedAt(ctx context.Context, mentorID string) error
	SetMentorStatus(ctx context.Context, mentorID, status string) error
	SoftDeleteMentor(ctx context.Context, mentorID string) (int, error)
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

	// Resolve tags, refusing the save if ANY name is unknown — see
	// resolveTagsStrict: a partial mismatch would otherwise drop just the
	// unresolved associations and still report success.
	tagIDs, unresolvedTags := resolveTagsStrict(ctx, s.mentorRepo, req.Tags)
	if len(unresolvedTags) > 0 {
		s.tracker.Track(ctx, analytics.EventMentorProfileUpdated, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id": mentorID,
			"outcome":   "unknown_tags",
		})
		logger.Error("Profile save rejected: submitted tags do not exist",
			zap.String("mentor_id", mentorID),
			zap.Strings("unresolved_tags", unresolvedTags))
		return apperrors.InvalidInputError("tags",
			"unknown tag(s): "+strings.Join(unresolvedTags, ", "))
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

// UploadPictureByMentorId uploads a profile picture using Mentor ID (UUID) for session-based auth.
// Images are keyed by the mentor UUID (immutable), not the slug (user-changeable).
func (s *ProfileService) UploadPictureByMentorId(ctx context.Context, mentorID string, req *models.UploadProfilePictureRequest) (string, error) {
	// No object storage, no upload — reject before any DB read or write instead
	// of dereferencing a nil client (see ErrUploadsUnavailable).
	if s.storageClient == nil {
		metrics.ProfilePictureUploads.WithLabelValues("uploads_unavailable").Inc()
		logger.Error("Profile picture upload rejected: object storage is not configured",
			zap.String("mentor_id", mentorID))
		return "", ErrUploadsUnavailable
	}

	// Validate and classify the image FIRST: this endpoint takes nothing but a
	// mentor session (no captcha), so a rejected upload must cost neither the
	// three S3 objects nor an unbounded decode.
	photo, err := preparePhoto(ctx, req.Image, req.ContentType)
	if err != nil {
		metrics.ProfilePictureUploads.WithLabelValues("invalid_image").Inc()
		s.tracker.Track(ctx, analytics.EventMentorProfilePictureUploaded, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":    mentorID,
			"content_type": req.ContentType,
			"outcome":      "invalid_image",
		})
		logger.Warn("Profile picture rejected",
			zap.Error(err),
			zap.String("mentor_id", mentorID))
		return "", err
	}

	// Verify the mentor still exists BEFORE writing any images. Keying images by
	// UUID (D29) removed the slug lookup that used to double as this existence
	// check — without it, a deleted mentor holding a still-valid session cookie
	// would orphan three PII objects in S3 (the DB updates below are best-effort
	// and only logged, so the endpoint would still return success).
	if _, err := s.mentorRepo.GetByMentorId(ctx, mentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true}); err != nil {
		return "", fmt.Errorf("mentor not found: %w", err)
	}

	// Upload to S3-compatible object storage in 3 sizes: full, large, small
	// (synchronous), reusing the bytes preparePhoto already decoded.
	fullImageURL, err := s.storageClient.UploadImageAllSizesBytes(ctx, photo.bytes, mentorID, photo.contentType)
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

	// Store the display style detected by preparePhoto ('hero' on light uniform
	// backgrounds, 'frame' otherwise) on the mentor row.
	photoStyle := photo.style
	if err := s.mentorRepo.Update(ctx, mentorID, map[string]interface{}{"photo_style": photoStyle}); err != nil {
		logger.Error("Failed to store photo style after picture upload",
			zap.Error(err),
			zap.String("mentor_id", mentorID),
			zap.String("photo_style", photoStyle))
	}

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

// DeleteProfileByMentorId is a mentor deleting their own profile (D70).
//
// confirmUsername is the value typed into the deletion dialog. It is compared
// against the SESSION's own mentor row and never used to select one: the
// profile deleted is always the one behind the session, so a mistyped — or
// maliciously crafted — value can only fail the delete, never redirect it at
// somebody else's profile.
//
// The comparison is case-insensitive and whitespace-trimmed. Slugs are
// lowercase by construction, so a mentor who capitalises their own name, or
// whose phone helpfully capitalises it for them, is confirming correctly and
// should not be told otherwise; that leniency costs nothing, because typing the
// right name in the wrong case is not the accident this guard exists to catch.
func (s *ProfileService) DeleteProfileByMentorId(ctx context.Context, mentorID, confirmUsername string) error {
	// One closure feeds BOTH sinks, so an outcome cannot be tracked in
	// analytics and missing from the trace (or spelled differently in each).
	track := func(outcome string, extra map[string]interface{}) {
		properties := map[string]interface{}{
			"mentor_id": mentorID,
			"outcome":   outcome,
		}
		for key, value := range extra {
			properties[key] = value
		}
		s.tracker.Track(ctx, analytics.EventMentorProfileDeleted, analytics.MentorDistinctID(mentorID), properties)
		annotateDeletionSpan(ctx, deletionActionDelete, deletionInitiatorMentor, mentorID, outcome)
	}

	// IncludeDeleted so an already-deleted profile is reported as such rather
	// than as a missing mentor — every other read in this service deliberately
	// cannot see one.
	mentor, err := s.mentorRepo.GetByMentorId(ctx, mentorID,
		models.FilterOptions{ShowHidden: true, AllowAnyStatus: true, IncludeDeleted: true})
	if err != nil {
		track("mentor_not_found", nil)
		return apperrors.NotFoundError("mentor")
	}
	if mentor.DeletedAt != nil {
		track("already_deleted", nil)
		return ErrProfileAlreadyDeleted
	}

	if !usernameConfirmationMatches(confirmUsername, mentor.Slug) {
		track("confirmation_mismatch", nil)
		logger.Warn("Profile deletion rejected: username confirmation did not match",
			zap.String("mentor_id", mentorID))
		return ErrDeleteConfirmationMismatch
	}

	invitationsRevoked, err := s.mentorRepo.SoftDeleteMentor(ctx, mentorID)
	if err != nil {
		if errors.Is(err, repository.ErrMentorAlreadyDeleted) {
			track("already_deleted", nil)
			return ErrProfileAlreadyDeleted
		}
		track("delete_failed", nil)
		logger.Error("Failed to delete mentor profile",
			zap.Error(err),
			zap.String("mentor_id", mentorID))
		return fmt.Errorf("failed to delete profile")
	}

	// Confirm the deletion by email. Fired AFTER the delete commits, like every
	// other trigger here: a confirmation for a deletion that failed is worse
	// than no confirmation. Fire-and-forget — the mentor's profile is deleted
	// whether or not the email goes out, so a trigger failure must not turn a
	// completed deletion into an error the client will retry.
	trigger.CallAsync(ctx, s.config.EventTriggers.ProfileDeletedTriggerURL(), mentorID, s.config.Worker.AuthToken, s.httpClient)

	track(outcomeSuccess, map[string]interface{}{
		"deleted_by":          deletionInitiatorMentor,
		"from_status":         mentor.Status,
		"invitations_revoked": invitationsRevoked,
	})
	logger.Info("Mentor profile deleted by its owner",
		zap.String("mentor_id", mentorID),
		zap.Int("invitations_revoked", invitationsRevoked))

	return nil
}

// usernameConfirmationMatches compares a typed confirmation against the
// profile's username. Shared by the mentor's own deletion and the admin one so
// the two dialogs cannot come to different conclusions about the same typing.
func usernameConfirmationMatches(typed, username string) bool {
	return strings.EqualFold(strings.TrimSpace(typed), strings.TrimSpace(username))
}
