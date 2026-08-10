package services

import (
	"context"
	"fmt"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
	"go.uber.org/zap"
)

// AdminRequestsRepository is the client-request repository surface the admin
// moderation service needs. *repository.ClientRequestRepository satisfies it;
// tests substitute a fake.
type AdminRequestsRepository interface {
	ListByMentorFiltered(ctx context.Context, mentorID string, statuses []models.RequestStatus) ([]*models.MentorClientRequest, error)
	GetByID(ctx context.Context, id string) (*models.MentorClientRequest, error)
	UpdateStatus(ctx context.Context, id string, expected, status models.RequestStatus) (applied bool, err error)
}

var _ AdminRequestsRepository = (*repository.ClientRequestRepository)(nil)

// AdminRequestsService gives admins read access to the requests a mentor
// received plus an unrestricted status override.
//
// Deliberate differences from MentorRequestsService (the mentor's own inbox):
//   - the workflow graph (CanTransitionTo) is NOT enforced — an admin fixes
//     mistakes, which means moving a request BACK out of a terminal status
//     ('done'/'declined'/'unavailable') is the whole point of this endpoint;
//   - no mentee-facing email is triggered. The mentor flow notifies the mentee
//     when a request finishes; a corrective back-office edit must not re-send
//     that mail, so admin overrides stay silent and only touch the row.
type AdminRequestsService struct {
	requestRepo AdminRequestsRepository
	tracker     analytics.Tracker
}

func NewAdminRequestsService(
	requestRepo AdminRequestsRepository,
	tracker analytics.Tracker,
) *AdminRequestsService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &AdminRequestsService{
		requestRepo: requestRepo,
		tracker:     tracker,
	}
}

// ListMentorRequests returns every request a mentor received, optionally
// narrowed to a single status. Admin role only — moderators are scoped to the
// pending-profile queue and have no business reading mentee correspondence.
func (s *AdminRequestsService) ListMentorRequests(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	statusFilter models.RequestStatus,
) (*models.ClientRequestsResponse, error) {

	if session.Role != models.ModeratorRoleAdmin {
		return nil, ErrAdminForbiddenAction
	}

	var statuses []models.RequestStatus
	if statusFilter != "" {
		if !statusFilter.IsValid() {
			return nil, fmt.Errorf("unsupported status filter: %s", statusFilter)
		}
		statuses = []models.RequestStatus{statusFilter}
	}

	requests, err := s.requestRepo.ListByMentorFiltered(ctx, mentorID, statuses)
	if err != nil {
		logger.Error("Failed to list mentor requests for admin",
			zap.String("mentor_id", mentorID),
			zap.String("status_filter", string(statusFilter)),
			zap.Error(err))
		return nil, err
	}

	items := make([]models.MentorClientRequest, 0, len(requests))
	for _, request := range requests {
		items = append(items, *request)
	}

	return &models.ClientRequestsResponse{
		Requests: items,
		Total:    len(items),
	}, nil
}

// GetMentorRequest returns a single request, verifying it belongs to the
// mentor in the URL. A mismatch reads as "not found" so the endpoint can't be
// used to walk requests across mentors.
func (s *AdminRequestsService) GetMentorRequest(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	requestID string,
) (*models.MentorClientRequest, error) {

	if session.Role != models.ModeratorRoleAdmin {
		return nil, ErrAdminForbiddenAction
	}

	request, err := s.requestRepo.GetByID(ctx, requestID)
	if err != nil {
		logger.Warn("Admin request lookup failed",
			zap.String("request_ref", redact.ID(requestID)),
			zap.Error(err))
		return nil, ErrRequestNotFound
	}
	if request.MentorID != mentorID {
		logger.Warn("Admin request lookup mentor mismatch",
			zap.String("request_ref", redact.ID(requestID)),
			zap.String("request_mentor", request.MentorID),
			zap.String("route_mentor", mentorID))
		return nil, ErrRequestNotFound
	}

	return request, nil
}

// UpdateRequestStatus sets a request to any valid status, including out of a
// terminal one. See the type comment for why the mentor-side transition graph
// and the mentee notification are both skipped here.
func (s *AdminRequestsService) UpdateRequestStatus(
	ctx context.Context,
	session *models.AdminSession,
	mentorID string,
	requestID string,
	newStatus models.RequestStatus,
) (*models.MentorClientRequest, error) {

	track := func(fromStatus models.RequestStatus, outcome string) {
		s.tracker.Track(ctx, analytics.EventAdminRequestStatusUpdated, analytics.ModeratorDistinctID(session.ModeratorID), map[string]interface{}{
			"moderator_id":     session.ModeratorID,
			"moderator_role":   string(session.Role),
			"target_mentor_id": mentorID,
			"from_status":      string(fromStatus),
			"to_status":        string(newStatus),
			"outcome":          outcome,
		})
	}

	if !newStatus.IsValid() {
		track("", "invalid_status")
		return nil, fmt.Errorf("unsupported status: %s", newStatus)
	}

	request, err := s.GetMentorRequest(ctx, session, mentorID, requestID)
	if err != nil {
		track("", "request_not_found_or_forbidden")
		return nil, err
	}

	oldStatus := request.Status
	if oldStatus == newStatus {
		track(oldStatus, "no_change")
		return request, nil
	}

	// Compare-and-set on the status this admin was shown. An override is
	// unrestricted about WHICH transitions it may make, not about acting on a
	// stale view: if the mentor moved the request in between, the admin's list is
	// out of date and the answer is to reload, not to overwrite blindly.
	applied, err := s.requestRepo.UpdateStatus(ctx, requestID, oldStatus, newStatus)
	if err != nil {
		track(oldStatus, "update_failed")
		logger.Error("Failed to update request status as admin",
			zap.String("request_ref", redact.ID(requestID)),
			zap.Error(err))
		return nil, fmt.Errorf("failed to update status: %w", err)
	}
	if !applied {
		track(oldStatus, "concurrent_change")
		logger.Warn("Admin status override did not apply: the request had already moved",
			zap.String("request_ref", redact.ID(requestID)),
			zap.String("expected_status", string(oldStatus)),
			zap.String("to_status", string(newStatus)))
		return nil, fmt.Errorf("%w: request is no longer '%s'", ErrInvalidStatusTransition, oldStatus)
	}

	track(oldStatus, "success")
	logger.Info("Request status updated by admin",
		zap.String("request_ref", redact.ID(requestID)),
		zap.String("moderator_id", session.ModeratorID),
		zap.String("from_status", string(oldStatus)),
		zap.String("to_status", string(newStatus)))

	return s.requestRepo.GetByID(ctx, requestID)
}
