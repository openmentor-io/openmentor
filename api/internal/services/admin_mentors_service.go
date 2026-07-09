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
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
)

const (
	mentorStatusPending  = "pending"
	mentorStatusActive   = "active"
	mentorStatusInactive = "inactive"
	mentorStatusDeclined = "declined"

	moderationActionApprove = "approve"
	moderationActionDecline = "decline"
)

var (
	ErrAdminForbiddenAction = errors.New("forbidden action for current role")
)

type AdminMentorsService struct {
	mentorRepo     *repository.MentorRepository
	profileService ProfileServiceInterface
	config         *config.Config
	httpClient     httpclient.Client
	tracker        analytics.Tracker
}

func NewAdminMentorsService(
	mentorRepo *repository.MentorRepository,
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

	if permissionErr := validateProfileUpdatePermissions(session, mentor, req); permissionErr != nil {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "forbidden", nil)
		return nil, permissionErr
	}

	telegram := normalizeTelegramHandle(req.Telegram)
	tagIDs := s.resolveTagIDs(ctx, req.Tags)
	if len(tagIDs) == 0 {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "invalid_tags", nil)
		return nil, fmt.Errorf("at least one valid tag is required")
	}

	updates, err := buildProfileUpdates(session, req, telegram)
	if err != nil {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "invalid_payload", nil)
		return nil, err
	}

	if err := s.mentorRepo.Update(ctx, mentorID, updates); err != nil {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "update_failed", nil)
		return nil, err
	}
	if err := s.mentorRepo.UpdateMentorTags(ctx, mentorID, tagIDs); err != nil {
		s.trackAdminProfileUpdate(ctx, session, mentorID, "tags_update_failed", nil)
		return nil, err
	}

	s.trackAdminProfileUpdate(ctx, session, mentorID, "success", map[string]interface{}{
		"tags_count": len(tagIDs),
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

	mentor, err := s.mentorRepo.GetForModerationByID(ctx, mentorID)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventAdminMentorPictureUploaded, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"outcome":          "mentor_not_found",
		})
		return "", err
	}
	uploadURL, err := s.profileService.UploadPictureByMentorId(ctx, mentorID, mentor.Slug, req)
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
	if session.Role == models.ModeratorRoleModerator && mentor.Status != mentorStatusPending {
		s.trackModerationAction(ctx, session, mentorID, action, "forbidden")
		return nil, ErrAdminForbiddenAction
	}

	if err := s.mentorRepo.SetMentorStatus(ctx, mentorID, targetStatus); err != nil {
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
	req *models.AdminMentorProfileUpdateRequest,
) error {

	if session.Role == models.ModeratorRoleModerator && mentor.Status != mentorStatusPending {
		return ErrAdminForbiddenAction
	}
	if session.Role != models.ModeratorRoleAdmin && req.Slug != nil {
		return ErrAdminForbiddenAction
	}
	return nil
}

func normalizeTelegramHandle(input string) string {
	telegram := strings.TrimSpace(input)
	telegram = strings.TrimPrefix(telegram, "@")
	telegram = strings.TrimPrefix(telegram, "https://t.me/")
	telegram = strings.TrimPrefix(telegram, "t.me/")
	return telegram
}

func (s *AdminMentorsService) resolveTagIDs(ctx context.Context, tags []string) []string {
	tagIDs := make([]string, 0, len(tags))
	for _, tagName := range tags {
		tagID, err := s.mentorRepo.GetTagIDByName(ctx, tagName)
		if err == nil && tagID != "" {
			tagIDs = append(tagIDs, tagID)
		}
	}
	return tagIDs
}

func buildProfileUpdates(
	session *models.AdminSession,
	req *models.AdminMentorProfileUpdateRequest,
	telegram string,
) (map[string]interface{}, error) {

	updates := map[string]interface{}{
		"name":         req.Name,
		"email":        req.Email,
		"telegram":     telegram,
		"job_title":    req.Job,
		"workplace":    req.Workplace,
		"experience":   req.Experience,
		"price":        req.Price,
		"details":      req.Description,
		"about":        req.About,
		"competencies": req.Competencies,
		"calendar_url": req.CalendarURL,
	}
	if session.Role != models.ModeratorRoleAdmin {
		return updates, nil
	}

	if req.Slug != nil {
		slug := strings.TrimSpace(*req.Slug)
		if slug == "" {
			return nil, fmt.Errorf("slug cannot be empty")
		}
		updates["slug"] = slug
	}
	return updates, nil
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
