package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"go.uber.org/zap"
)

var (
	ErrRequestNotFound         = errors.New("request not found")
	ErrAccessDenied            = errors.New("access denied")
	ErrInvalidStatusTransition = errors.New("invalid status transition")
	ErrCannotDeclineRequest    = errors.New("cannot decline request")
	ErrInvalidRequestGroup     = errors.New("invalid request group")
)

// MentorRequestsService handles mentor request operations
type MentorRequestsService struct {
	requestRepo *repository.ClientRequestRepository
	config      *config.Config
	httpClient  httpclient.Client
	tracker     analytics.Tracker
}

// NewMentorRequestsService creates a new MentorRequestsService
func NewMentorRequestsService(
	requestRepo *repository.ClientRequestRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *MentorRequestsService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &MentorRequestsService{
		requestRepo: requestRepo,
		config:      cfg,
		httpClient:  httpClient,
		tracker:     tracker,
	}
}

// GetRequests retrieves requests for a mentor filtered by group
func (s *MentorRequestsService) GetRequests(ctx context.Context, mentorId string, group string) (*models.ClientRequestsResponse, error) {
	start := time.Now()

	// Validate group
	requestGroup := models.RequestGroup(group)
	statuses := requestGroup.GetStatuses()
	if statuses == nil {
		return nil, ErrInvalidRequestGroup
	}

	// Fetch requests from repository
	requests, err := s.requestRepo.GetByMentor(ctx, mentorId, statuses)
	if err != nil {
		logger.Error("Failed to fetch requests",
			zap.String("mentor_id", mentorId),
			zap.String("group", group),
			zap.Error(err))
		return nil, fmt.Errorf("failed to fetch requests: %w", err)
	}

	// Convert to response format
	responseRequests := make([]models.MentorClientRequest, 0, len(requests))
	for _, req := range requests {
		responseRequests = append(responseRequests, *req)
	}

	duration := metrics.MeasureDuration(start)
	metrics.MentorRequestsListDuration.Observe(duration)
	metrics.MentorRequestsListTotal.WithLabelValues(group).Inc()

	logger.Info("Fetched mentor requests",
		zap.String("mentor_id", mentorId),
		zap.String("group", group),
		zap.Int("count", len(responseRequests)),
		zap.Duration("duration", time.Since(start)))

	return &models.ClientRequestsResponse{
		Requests: responseRequests,
		Total:    len(responseRequests),
	}, nil
}

// GetRequestByID retrieves a single request and verifies ownership
func (s *MentorRequestsService) GetRequestByID(ctx context.Context, mentorId string, requestID string) (*models.MentorClientRequest, error) {
	// Fetch request
	request, err := s.requestRepo.GetByID(ctx, requestID)
	if err != nil {
		logger.Warn("Request not found",
			zap.String("request_id", requestID),
			zap.Error(err))
		return nil, ErrRequestNotFound
	}

	// Verify ownership
	if request.MentorID != mentorId {
		logger.Warn("Access denied to request",
			zap.String("request_id", requestID),
			zap.String("request_mentor", request.MentorID),
			zap.String("requesting_mentor", mentorId))
		return nil, ErrAccessDenied
	}

	return request, nil
}

// UpdateStatus updates the status of a request with workflow validation
func (s *MentorRequestsService) UpdateStatus(ctx context.Context, mentorId string, requestID string, newStatus models.RequestStatus) (*models.MentorClientRequest, error) {
	// Fetch and verify ownership
	request, err := s.GetRequestByID(ctx, mentorId, requestID)
	if err != nil {
		return nil, err
	}

	// Validate status transition
	if !request.Status.CanTransitionTo(newStatus) {
		s.tracker.Track(ctx, analytics.EventMentorRequestStatusUpdated, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id":  requestID,
			"mentor_id":   mentorId,
			"from_status": string(request.Status),
			"to_status":   string(newStatus),
			"outcome":     "invalid_transition",
		})
		logger.Warn("Invalid status transition",
			zap.String("request_id", requestID),
			zap.String("from_status", string(request.Status)),
			zap.String("to_status", string(newStatus)))
		return nil, fmt.Errorf("%w: cannot transition from '%s' to '%s'", ErrInvalidStatusTransition, request.Status, newStatus)
	}

	oldStatus := request.Status

	// Update in repository
	if err := s.requestRepo.UpdateStatus(ctx, requestID, newStatus); err != nil {
		s.tracker.Track(ctx, analytics.EventMentorRequestStatusUpdated, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id":  requestID,
			"mentor_id":   mentorId,
			"from_status": string(oldStatus),
			"to_status":   string(newStatus),
			"outcome":     "db_error",
		})
		logger.Error("Failed to update request status",
			zap.String("request_id", requestID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to update status: %w", err)
	}

	// Trigger email sending via webhook
	if newStatus == models.StatusDone && s.config.EventTriggers.RequestProcessFinishedTriggerURL != "" {
		trigger.CallAsync(ctx, s.config.EventTriggers.RequestProcessFinishedTriggerURL, requestID, s.config.Worker.AuthToken, s.httpClient)
	}

	// Record metrics
	metrics.MentorRequestsStatusUpdates.WithLabelValues(string(oldStatus), string(newStatus)).Inc()
	s.tracker.Track(ctx, analytics.EventMentorRequestStatusUpdated, analytics.RequestDistinctID(requestID), map[string]interface{}{
		"request_id":  requestID,
		"mentor_id":   mentorId,
		"from_status": string(oldStatus),
		"to_status":   string(newStatus),
		"outcome":     "success",
	})

	logger.Info("Request status updated",
		zap.String("request_id", requestID),
		zap.String("from_status", string(oldStatus)),
		zap.String("to_status", string(newStatus)))

	// Fetch updated request
	return s.requestRepo.GetByID(ctx, requestID)
}

// DeclineRequest declines a request with reason
func (s *MentorRequestsService) DeclineRequest(ctx context.Context, mentorId string, requestID string, payload *models.DeclineRequestPayload) (*models.MentorClientRequest, error) {
	// Fetch and verify ownership
	request, err := s.GetRequestByID(ctx, mentorId, requestID)
	if err != nil {
		return nil, err
	}

	// Check if request can be declined
	if request.Status == models.StatusDone {
		s.tracker.Track(ctx, analytics.EventMentorRequestDeclined, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id": requestID,
			"mentor_id":  mentorId,
			"status":     string(request.Status),
			"outcome":    "invalid_state",
		})
		logger.Warn("Cannot decline completed request",
			zap.String("request_id", requestID),
			zap.String("status", string(request.Status)))
		return nil, fmt.Errorf("%w: request with status '%s' cannot be declined", ErrCannotDeclineRequest, request.Status)
	}

	if request.Status.IsTerminalStatus() {
		s.tracker.Track(ctx, analytics.EventMentorRequestDeclined, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id": requestID,
			"mentor_id":  mentorId,
			"status":     string(request.Status),
			"outcome":    "terminal_state",
		})
		logger.Warn("Cannot decline request with terminal status",
			zap.String("request_id", requestID),
			zap.String("status", string(request.Status)))
		return nil, fmt.Errorf("%w: request with status '%s' cannot be declined", ErrCannotDeclineRequest, request.Status)
	}

	// Update in repository
	if err := s.requestRepo.UpdateDecline(ctx, requestID, payload.Reason, payload.Comment); err != nil {
		s.tracker.Track(ctx, analytics.EventMentorRequestDeclined, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id": requestID,
			"mentor_id":  mentorId,
			"reason":     string(payload.Reason),
			"outcome":    "db_error",
		})
		logger.Error("Failed to decline request",
			zap.String("request_id", requestID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to decline request: %w", err)
	}

	// Trigger email sending via webhook
	if s.config.EventTriggers.RequestProcessFinishedTriggerURL != "" {
		trigger.CallAsync(ctx, s.config.EventTriggers.RequestProcessFinishedTriggerURL, requestID, s.config.Worker.AuthToken, s.httpClient)
	}

	// Record metrics
	metrics.MentorRequestsDeclines.WithLabelValues(string(payload.Reason)).Inc()
	s.tracker.Track(ctx, analytics.EventMentorRequestDeclined, analytics.RequestDistinctID(requestID), map[string]interface{}{
		"request_id": requestID,
		"mentor_id":  mentorId,
		"reason":     string(payload.Reason),
		"outcome":    "success",
	})

	logger.Info("Request declined",
		zap.String("request_id", requestID),
		zap.String("reason", string(payload.Reason)))

	// Fetch updated request
	return s.requestRepo.GetByID(ctx, requestID)
}
