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
	"github.com/openmentor-io/openmentor/api/pkg/price"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
)

const (
	mentorStatusDraft    = "draft"
	mentorStatusPending  = "pending"
	mentorStatusActive   = "active"
	mentorStatusInactive = "inactive"
	mentorStatusDeclined = "declined"

	moderationActionApprove = "approve"
	moderationActionDecline = "decline"
	moderationActionReturn  = "return"

	// moderationReasonMaxLength caps the reviewer note stored in
	// mentors.moderation_note for the 'return' action.
	moderationReasonMaxLength = 2000
)

var (
	ErrAdminForbiddenAction = errors.New("forbidden action for current role")
	// ErrMentorAlreadyActivated guards the 'return' action: a mentor that
	// has ever been active can never be moved back to draft.
	ErrMentorAlreadyActivated = errors.New("mentor has already been activated and cannot be returned to draft")
	// ErrMentorDeleted reports an action aimed at a deleted profile (D70).
	// Deletion makes a profile inert: approve, decline, return, the status
	// toggle, profile edits, picture uploads and username changes all refuse.
	// Restore is the only way out.
	ErrMentorDeleted = errors.New("this profile is deleted; restore it before making changes")
	// ErrMentorAlreadyDeleted / ErrMentorNotDeleted are the two no-op outcomes
	// of the delete and restore actions. Both mean the admin is acting on stale
	// state — a second tab, a double submit — and both answer 409 rather than
	// silently reporting success for a change that did not happen.
	ErrMentorAlreadyDeleted = errors.New("this profile is already deleted")
	ErrMentorNotDeleted     = errors.New("this profile is not deleted")
)

// AdminMentorsRepository is the mentor repository surface the admin
// moderation service needs. *repository.MentorRepository satisfies it;
// tests substitute a fake.
type AdminMentorsRepository interface {
	ListForModeration(ctx context.Context, statuses []string) ([]models.AdminMentorListItem, error)
	ListDeletedForModeration(ctx context.Context) ([]models.AdminMentorListItem, error)
	GetForModerationByID(ctx context.Context, mentorID string) (*models.AdminMentorDetails, error)
	GetTagIDByName(ctx context.Context, tagName string) (string, error)
	Update(ctx context.Context, mentorID string, updates map[string]interface{}) error
	UpdateProfileWithTags(ctx context.Context, mentorID string, updates map[string]interface{}, tagIDs []string) error
	SetMentorStatus(ctx context.Context, mentorID, status string) error
	ApproveMentorModeration(ctx context.Context, mentorID string) error
	ReturnMentorToDraft(ctx context.Context, mentorID, note string) error
	SoftDeleteMentor(ctx context.Context, mentorID string) (int, error)
	RestoreMentor(ctx context.Context, mentorID string) error
}

var _ AdminMentorsRepository = (*repository.MentorRepository)(nil)

type AdminMentorsService struct {
	mentorRepo     AdminMentorsRepository
	profileService ProfileServiceInterface
	config         *config.Config
	httpClient     httpclient.Client
	tracker        analytics.Tracker
}

func NewAdminMentorsService(
	mentorRepo AdminMentorsRepository,
	profileService ProfileServiceInterface,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *AdminMentorsService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &AdminMentorsService{
		mentorRepo:     mentorRepo,
		profileService: profileService,
		config:         cfg,
		httpClient:     httpClient,
		tracker:        tracker,
	}
}

func (s *AdminMentorsService) ListMentors(
	ctx context.Context,
	session *models.AdminSession,
	filter models.MentorModerationFilter,
) ([]models.AdminMentorListItem, error) {

	// The deleted tab is not a status group (deletion is a timestamp, not a
	// status — see migration 000013), so it takes its own repository method
	// rather than a fourth entry in resolveStatuses.
	if filter == models.MentorModerationFilterDeleted {
		if session.Role != models.ModeratorRoleAdmin {
			return nil, ErrAdminForbiddenAction
		}
		return s.mentorRepo.ListDeletedForModeration(ctx)
	}

	statuses, err := resolveStatuses(filter, session.Role)
	if err != nil {
		return nil, err
	}

	mentors, err := s.mentorRepo.ListForModeration(ctx, statuses)
	if err != nil {
		return nil, err
	}

	return mentors, nil
}

func (s *AdminMentorsService) GetMentor(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
) (*models.AdminMentorDetails, error) {

	mentor, err := s.mentorRepo.GetForModerationByID(ctx, mentorID)
	if err != nil {
		return nil, err
	}
	// "Only an admin can see a deleted profile's details" (D70). A moderator is
	// already blocked by the pending check below — deletion leaves status
	// 'inactive' — but that is a consequence of how deletion happens to set
	// status, not a rule about deletion. State the rule.
	if mentor.DeletedAt != nil && session.Role != models.ModeratorRoleAdmin {
		return nil, ErrAdminForbiddenAction
	}
	if session.Role == models.ModeratorRoleModerator && mentor.Status != mentorStatusPending {
		return nil, ErrAdminForbiddenAction
	}
	return mentor, nil
}

func (s *AdminMentorsService) UpdateMentorProfile(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	req *models.AdminMentorProfileUpdateRequest,
) (*models.AdminMentorDetails, error) {

	mentor, err := s.GetMentor(ctx, session, mentorID)
	if err != nil {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "mentor_not_found_or_forbidden", nil)
		return nil, err
	}

	if mentor.DeletedAt != nil {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "mentor_deleted", nil)
		return nil, ErrMentorDeleted
	}

	if permissionErr := validateProfileUpdatePermissions(session, mentor); permissionErr != nil {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "forbidden", nil)
		return nil, permissionErr
	}

	contact := strings.TrimSpace(req.PreferredContact)
	// Reject if ANY tag is unknown, not just when all of them are: tag updates
	// replace the whole set, so a partial mismatch (stale admin UI after a
	// taxonomy change) would silently drop the unresolved associations.
	tagIDs, unresolvedTags := resolveTagsStrict(ctx, s.mentorRepo, req.Tags)
	if len(unresolvedTags) > 0 {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "invalid_tags", nil)
		return nil, fmt.Errorf("unknown tag(s): %s", strings.Join(unresolvedTags, ", "))
	}
	if len(tagIDs) == 0 {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "invalid_tags", nil)
		return nil, fmt.Errorf("at least one valid tag is required")
	}

	updates := buildProfileUpdates(req, contact)

	// One transaction for row + tags (C1), same as the mentor's own save: the two
	// writes used to be sequential, so a tags failure left the admin looking at
	// an error next to a profile whose text had already changed.
	if err := s.mentorRepo.UpdateProfileWithTags(ctx, mentorID, updates, tagIDs); err != nil {
		if errors.Is(err, repository.ErrMentorNotWritable) {
			// A deletion committed between GetMentor above and this write.
			s.trackAdminProfileUpdate(ctx, session, mentorID, "mentor_deleted", nil)
			return nil, ErrMentorDeleted
		}
		s.trackAdminProfileUpdate(ctx, session, mentorID, "update_failed", nil)
		return nil, err
	}

	s.trackAdminProfileUpdate(ctx, session, mentorID, "success", map[string]interface{}{
		"tags_count": len(tagIDs),
		// Bounded (free/negotiable/fixed/invalid), never the raw price. Event
		// property only — buildProfileUpdates' map names database columns and
		// the repository allowlist rejects anything else (PR review).
		"price_kind": price.KindLabel(req.Price),
	})
	return s.mentorRepo.GetForModerationByID(ctx, mentorID)
}

func (s *AdminMentorsService) ApproveMentor(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
) (*models.AdminMentorDetails, error) {

	return s.setModerationStatus(ctx, session, mentorID, moderationActionApprove, mentorStatusActive)
}

func (s *AdminMentorsService) DeclineMentor(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
) (*models.AdminMentorDetails, error) {

	return s.setModerationStatus(ctx, session, mentorID, moderationActionDecline, mentorStatusDeclined)
}

func (s *AdminMentorsService) UpdateMentorStatus(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	status string,
) (*models.AdminMentorDetails, error) {

	if session.Role != models.ModeratorRoleAdmin {
		s.tracker.Track(ctx, analytics.EventAdminMentorStatusUpdated, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"requested_status": status,
			"outcome":          "forbidden",
		})
		return nil, ErrAdminForbiddenAction
	}
	if status != mentorStatusActive && status != mentorStatusInactive {
		s.tracker.Track(ctx, analytics.EventAdminMentorStatusUpdated, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"requested_status": status,
			"outcome":          "unsupported_status",
		})
		return nil, fmt.Errorf("unsupported status: %s", status)
	}

	mentor, err := s.mentorRepo.GetForModerationByID(ctx, mentorID)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventAdminMentorStatusUpdated, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"requested_status": status,
			"outcome":          "mentor_not_found",
		})
		return nil, err
	}
	if mentor.DeletedAt != nil {
		s.tracker.Track(ctx, analytics.EventAdminMentorStatusUpdated, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"requested_status": status,
			"outcome":          "mentor_deleted",
		})
		return nil, ErrMentorDeleted
	}
	if mentor.Status != mentorStatusActive && mentor.Status != mentorStatusInactive {
		s.tracker.Track(ctx, analytics.EventAdminMentorStatusUpdated, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"from_status":      mentor.Status,
			"requested_status": status,
			"outcome":          "invalid_transition",
		})
		return nil, fmt.Errorf("status toggle is available only for approved mentors")
	}

	if err := s.mentorRepo.SetMentorStatus(ctx, mentorID, status); err != nil {
		s.tracker.Track(ctx, analytics.EventAdminMentorStatusUpdated, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"from_status":      mentor.Status,
			"requested_status": status,
			"outcome":          "update_failed",
		})
		return nil, err
	}
	s.tracker.Track(ctx, analytics.EventAdminMentorStatusUpdated, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
		"moderator_id":     session.ModeratorID,
		"moderator_role":   string(session.Role),
		"target_mentor_id": mentorID,
		"from_status":      mentor.Status,
		"requested_status": status,
		"outcome":          "success",
	})
	return s.mentorRepo.GetForModerationByID(ctx, mentorID)
}

func (s *AdminMentorsService) UploadMentorPicture(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	req *models.UploadProfilePictureRequest,
) (string, error) {

	if session.Role != models.ModeratorRoleAdmin {
		s.tracker.Track(ctx, analytics.EventAdminMentorPictureUploaded, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"outcome":          "forbidden",
		})
		return "", ErrAdminForbiddenAction
	}

	// Existence check only — images are keyed by the mentor UUID itself.
	target, err := s.mentorRepo.GetForModerationByID(ctx, mentorID)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventAdminMentorPictureUploaded, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"outcome":          "mentor_not_found",
		})
		return "", err
	}
	if target.DeletedAt != nil {
		s.tracker.Track(ctx, analytics.EventAdminMentorPictureUploaded, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"outcome":          "mentor_deleted",
		})
		return "", ErrMentorDeleted
	}
	uploadURL, err := s.profileService.UploadPictureByMentorId(ctx, mentorID, req)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventAdminMentorPictureUploaded, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"outcome":          "upload_failed",
		})
		return "", err
	}
	s.tracker.Track(ctx, analytics.EventAdminMentorPictureUploaded, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
		"moderator_id":     session.ModeratorID,
		"moderator_role":   string(session.Role),
		"target_mentor_id": mentorID,
		"url_returned":     strings.TrimSpace(uploadURL) != "",
		"outcome":          "success",
	})

	return uploadURL, nil
}

// ReturnMentor implements the 'return' moderation action: a pending
// profile goes back to 'draft' with the reviewer's note saved to
// mentors.moderation_note; the worker emails the mentor (template
// new-mentor-returned). HARD GUARD: a mentor that has ever been active can
// never be returned to draft.
func (s *AdminMentorsService) ReturnMentor(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	reason string,
) (*models.AdminMentorDetails, error) {

	trackReturn := func(outcome string) {
		s.tracker.Track(ctx, analytics.EventAdminMentorReturned, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"reason_length":    len(reason),
			"outcome":          outcome,
		})
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		trackReturn("invalid_reason")
		return nil, fmt.Errorf("a reason is required to return a profile")
	}
	if len(reason) > moderationReasonMaxLength {
		trackReturn("invalid_reason")
		return nil, fmt.Errorf("reason must be at most %d characters", moderationReasonMaxLength)
	}

	mentor, err := s.GetMentor(ctx, session, mentorID)
	if err != nil {
		trackReturn("mentor_not_found_or_forbidden")
		return nil, err
	}
	if mentor.DeletedAt != nil {
		trackReturn("mentor_deleted")
		return nil, ErrMentorDeleted
	}
	if mentor.Status != mentorStatusPending {
		trackReturn("invalid_transition")
		return nil, fmt.Errorf("return is available only for pending mentors")
	}
	if mentor.ActivatedAt != nil {
		trackReturn("forbidden_already_activated")
		return nil, ErrMentorAlreadyActivated
	}

	if err := s.mentorRepo.ReturnMentorToDraft(ctx, mentorID, reason); err != nil {
		if errors.Is(err, repository.ErrMentorWasActivated) {
			trackReturn("forbidden_already_activated")
			return nil, ErrMentorAlreadyActivated
		}
		trackReturn("update_failed")
		return nil, err
	}

	trackReturn("success")
	s.triggerModerationAction(ctx, moderationActionReturn, session, mentorID)

	return s.mentorRepo.GetForModerationByID(ctx, mentorID)
}

// DeleteMentor is an admin deleting a mentor's profile (D70). Admin role only:
// a moderator's remit is the pending queue, and deletion reaches profiles that
// have been live for years.
//
// confirmUsername must match the TARGET profile's username. Unlike the mentor's
// own deletion — where the typed name only has to match the session's profile —
// this is the admin naming which profile they mean, on a page where the next
// profile is one click away. It is still not a selector: the profile deleted is
// the one in the URL, and a mismatch fails the request rather than redirecting
// it.
func (s *AdminMentorsService) DeleteMentor(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	confirmUsername string,
) (*models.AdminMentorDetails, error) {

	track := func(outcome string, extra map[string]interface{}) {
		properties := map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"outcome":          outcome,
		}
		for key, value := range extra {
			properties[key] = value
		}
		s.tracker.Track(ctx, analytics.EventAdminMentorDeleted, analytics.ModeratorDistinctID(session.ModeratorID), properties)
		annotateDeletionSpan(ctx, deletionActionDelete, deletionInitiatorAdmin, mentorID, outcome)
	}

	if session.Role != models.ModeratorRoleAdmin {
		track("forbidden", nil)
		return nil, ErrAdminForbiddenAction
	}

	mentor, err := s.mentorRepo.GetForModerationByID(ctx, mentorID)
	if err != nil {
		track("mentor_not_found", nil)
		return nil, err
	}
	if mentor.DeletedAt != nil {
		track("already_deleted", nil)
		return nil, ErrMentorAlreadyDeleted
	}
	if !usernameConfirmationMatches(confirmUsername, mentor.Slug) {
		track("confirmation_mismatch", nil)
		return nil, ErrDeleteConfirmationMismatch
	}

	invitationsRevoked, err := s.mentorRepo.SoftDeleteMentor(ctx, mentorID)
	if err != nil {
		if errors.Is(err, repository.ErrMentorAlreadyDeleted) {
			track("already_deleted", nil)
			return nil, ErrMentorAlreadyDeleted
		}
		track("delete_failed", nil)
		return nil, err
	}

	// Same confirmation email the mentor's own deletion sends. An admin deleting
	// someone's profile is exactly the case where the mentor most needs to be
	// told — they did not press the button and would otherwise discover it by
	// finding their login broken.
	trigger.CallAsync(ctx, s.config.EventTriggers.ProfileDeletedTriggerURL(), mentorID, s.config.Worker.AuthToken, s.httpClient)

	// Same image stash as the mentor's own deletion (D70): the profile's photos
	// move to the S3 trash prefix, where the bucket lifecycle rule erases them.
	s.profileService.StashDeletedProfileImages(ctx, mentorID)

	track(outcomeSuccess, map[string]interface{}{
		"from_status":         mentor.Status,
		"invitations_revoked": invitationsRevoked,
	})
	return s.mentorRepo.GetForModerationByID(ctx, mentorID)
}

// RestoreMentor brings a deleted profile back as 'inactive' — the ONLY way out
// of the deleted state, and admin-only. Inactive rather than active because the
// profile was off the site while it was deleted; whoever restores it can
// publish it with the status toggle as a separate, deliberate act.
func (s *AdminMentorsService) RestoreMentor(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
) (*models.AdminMentorDetails, error) {

	track := func(outcome string) {
		s.tracker.Track(ctx, analytics.EventAdminMentorRestored, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"outcome":          outcome,
		})
		annotateDeletionSpan(ctx, deletionActionRestore, deletionInitiatorAdmin, mentorID, outcome)
	}

	if session.Role != models.ModeratorRoleAdmin {
		track("forbidden")
		return nil, ErrAdminForbiddenAction
	}

	if err := s.mentorRepo.RestoreMentor(ctx, mentorID); err != nil {
		if errors.Is(err, repository.ErrMentorNotDeleted) {
			track("not_deleted")
			return nil, ErrMentorNotDeleted
		}
		track("restore_failed")
		return nil, err
	}

	// Tell the mentor their profile is back — they were told it was deleted, so
	// leaving the reversal unannounced means their last word on the subject is
	// wrong. It also matters practically: the profile comes back INACTIVE, so
	// there is something for them to do.
	trigger.CallAsync(ctx, s.config.EventTriggers.ProfileRestoredTriggerURL(), mentorID, s.config.Worker.AuthToken, s.httpClient)

	// Bring the profile's images back from the S3 trash prefix, so "nothing was
	// lost" in the restore email covers the photo too (D70).
	s.profileService.RestoreDeletedProfileImages(ctx, mentorID)

	track(outcomeSuccess)
	return s.mentorRepo.GetForModerationByID(ctx, mentorID)
}

func (s *AdminMentorsService) setModerationStatus(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	action string,
	targetStatus string,
) (*models.AdminMentorDetails, error) {

	mentor, err := s.GetMentor(ctx, session, mentorID)
	if err != nil {
		s.trackModerationAction(ctx, session, mentorID, action, "mentor_not_found_or_forbidden")
		return nil, err
	}
	if mentor.DeletedAt != nil {
		s.trackModerationAction(ctx, session, mentorID, action, "mentor_deleted")
		return nil, ErrMentorDeleted
	}
	if session.Role == models.ModeratorRoleModerator && mentor.Status != mentorStatusPending {
		s.trackModerationAction(ctx, session, mentorID, action, "forbidden")
		return nil, ErrAdminForbiddenAction
	}

	if action == moderationActionApprove {
		// Approve also stamps activated_at on the first activation (the
		// hard guard against future returns to draft) and clears any
		// moderation note from a previous 'return'.
		err = s.mentorRepo.ApproveMentorModeration(ctx, mentorID)
	} else {
		err = s.mentorRepo.SetMentorStatus(ctx, mentorID, targetStatus)
	}
	if err != nil {
		s.trackModerationAction(ctx, session, mentorID, action, "update_failed")
		return nil, err
	}
	s.trackModerationAction(ctx, session, mentorID, action, "success")
	s.triggerModerationAction(ctx, action, session, mentorID)

	return s.mentorRepo.GetForModerationByID(ctx, mentorID)
}

func validateProfileUpdatePermissions(
	session *models.AdminSession,
	mentor *models.AdminMentorDetails,
) error {

	if session.Role == models.ModeratorRoleModerator && mentor.Status != mentorStatusPending {
		return ErrAdminForbiddenAction
	}
	return nil
}

// buildProfileUpdates maps the request onto DB columns. Slug (username) is
// deliberately NOT part of profile updates — renames go through the dedicated
// username endpoint so history/redirects are maintained.
func buildProfileUpdates(
	req *models.AdminMentorProfileUpdateRequest,
	contact string,
) map[string]interface{} {

	return map[string]interface{}{
		"name":              req.Name,
		"email":             req.Email,
		"preferred_contact": contact,
		"job_title":         req.Job,
		"workplace":         req.Workplace,
		"experience":        req.Experience,
		"price":             req.Price,
		"details":           req.Description,
		"about":             req.About,
		"competencies":      req.Competencies,
		"calendar_url":      req.CalendarURL,
	}
}

func (s *AdminMentorsService) triggerModerationAction(ctx context.Context, action string, session *models.AdminSession, mentorID string) {
	payload := models.AdminModerationTriggerPayload{
		Type:        "mentor_moderation",
		MentorID:    mentorID,
		Action:      action,
		ModeratorID: session.ModeratorID,
		Role:        string(session.Role),
	}
	trigger.CallAsyncWithPayload(ctx, s.config.EventTriggers.MentorModerationTriggerURL, payload, s.config.Worker.AuthToken, s.httpClient)
}

func (s *AdminMentorsService) trackModerationAction(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	action string,
	outcome string,
) {

	s.tracker.Track(ctx, analytics.EventAdminMentorModerationAction, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
		"moderator_id":     session.ModeratorID,
		"moderator_role":   string(session.Role),
		"target_mentor_id": mentorID,
		"action":           action,
		"outcome":          outcome,
	})
}

func (s *AdminMentorsService) trackAdminProfileUpdate(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	outcome string,
	extra map[string]interface{},
) {

	properties := map[string]interface{}{
		"moderator_id":     session.ModeratorID,
		"moderator_role":   string(session.Role),
		"target_mentor_id": mentorID,
		"outcome":          outcome,
	}
	for key, value := range extra {
		properties[key] = value
	}
	s.tracker.Track(ctx, analytics.EventAdminMentorProfileUpdated, analytics.ModeratorDistinctID(session.ModeratorID), properties)
}

func resolveStatuses(filter models.MentorModerationFilter, role models.ModeratorRole) ([]string, error) {
	if role == models.ModeratorRoleModerator {
		if filter != models.MentorModerationFilterPending {
			return nil, ErrAdminForbiddenAction
		}
		return []string{mentorStatusPending}, nil
	}

	switch filter {
	case models.MentorModerationFilterPending:
		return []string{mentorStatusPending}, nil
	case models.MentorModerationFilterApproved:
		return []string{mentorStatusActive, mentorStatusInactive}, nil
	case models.MentorModerationFilterDeclined:
		return []string{mentorStatusDeclined}, nil
	default:
		return nil, fmt.Errorf("unsupported filter: %s", filter)
	}
}
