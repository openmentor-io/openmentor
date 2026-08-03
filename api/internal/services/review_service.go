package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
	"go.uber.org/zap"
)

var (
	ErrReviewRequestNotFound = errors.New("request not found")
	ErrReviewRequestNotDone  = errors.New("request is not in done status")
	ErrReviewAlreadyExists   = errors.New("review already exists for this request")
	ErrReviewCaptchaFailed   = errors.New("captcha verification failed")
)

// ReviewRepository is the review repository surface the review service needs.
// *repository.ReviewRepository satisfies it; tests substitute a fake.
type ReviewRepository interface {
	CheckCanSubmitReview(ctx context.Context, requestID string) (*repository.ReviewCheckResult, error)
	CreateReview(ctx context.Context, requestID, mentorReview, platformReview, improvements string) (string, error)
}

var _ ReviewRepository = (*repository.ReviewRepository)(nil)

// ReviewService handles review submissions
type ReviewService struct {
	reviewRepo      ReviewRepository
	config          *config.Config
	httpClient      httpclient.Client
	captchaVerifier *turnstile.Verifier
	tracker         analytics.Tracker
}

// NewReviewService creates a new review service instance
func NewReviewService(
	reviewRepo ReviewRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *ReviewService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &ReviewService{
		reviewRepo:      reviewRepo,
		config:          cfg,
		httpClient:      httpClient,
		captchaVerifier: turnstile.NewVerifier(cfg.Turnstile.SecretKey, httpClient),
		tracker:         tracker,
	}
}

// CheckReview checks if a review can be submitted for a given request ID
func (s *ReviewService) CheckReview(ctx context.Context, requestID string) (*models.ReviewCheckResponse, error) {
	// SECURITY (P14): the request id is a bearer capability for this flow, so it
	// never leaves this function verbatim — telemetry gets the mentor it belongs
	// to plus a hashed reference for correlating log lines.
	requestRef := redact.ID(requestID)

	result, err := s.reviewRepo.CheckCanSubmitReview(ctx, requestID)
	if err != nil {
		metrics.ReviewChecks.WithLabelValues("error").Inc()
		s.tracker.Track(ctx, analytics.EventReviewEligibilityChecked, analytics.AnonymousDistinctID(), map[string]interface{}{
			"outcome":    "error",
			"can_submit": false,
		})
		logger.Error("Failed to check review eligibility",
			zap.String("request_ref", requestRef),
			zap.Error(err))
		return nil, fmt.Errorf("failed to check review: %w", err)
	}

	if result.MentorName == "" && !result.CanSubmit {
		metrics.ReviewChecks.WithLabelValues("not_found").Inc()
		s.tracker.Track(ctx, analytics.EventReviewEligibilityChecked, analytics.AnonymousDistinctID(), map[string]interface{}{
			"outcome":    "not_found",
			"can_submit": false,
		})
		logger.Info("Review check: request not found",
			zap.String("request_ref", requestRef))
		return &models.ReviewCheckResponse{
			CanSubmit: false,
			Error:     "Request not found",
		}, ErrReviewRequestNotFound
	}

	if !result.CanSubmit {
		metrics.ReviewChecks.WithLabelValues("ineligible").Inc()
		s.tracker.Track(ctx, analytics.EventReviewEligibilityChecked, analytics.MentorDistinctID(result.MentorID), map[string]interface{}{
			"mentor_id":  result.MentorID,
			"outcome":    "ineligible",
			"can_submit": false,
		})
		logger.Info("Review check: not eligible",
			zap.String("request_ref", requestRef),
			zap.String("mentor_id", result.MentorID))
		return &models.ReviewCheckResponse{
			CanSubmit:  false,
			MentorName: result.MentorName,
			Error:      "Review already submitted or the request is not completed yet",
		}, nil
	}

	metrics.ReviewChecks.WithLabelValues("eligible").Inc()
	s.tracker.Track(ctx, analytics.EventReviewEligibilityChecked, analytics.MentorDistinctID(result.MentorID), map[string]interface{}{
		"mentor_id":  result.MentorID,
		"outcome":    "eligible",
		"can_submit": true,
	})
	logger.Info("Review check: eligible",
		zap.String("request_ref", requestRef),
		zap.String("mentor_id", result.MentorID))

	return &models.ReviewCheckResponse{
		CanSubmit:  true,
		MentorName: result.MentorName,
	}, nil
}

// SubmitReview creates a new review after verifying captcha and eligibility
func (s *ReviewService) SubmitReview(ctx context.Context, requestID string, req *models.SubmitReviewRequest) (*models.SubmitReviewResponse, error) {
	start := time.Now()
	// SECURITY (P14): see CheckReview — the request id stays out of telemetry.
	requestRef := redact.ID(requestID)
	baseProperties := reviewSubmissionProperties(req)
	trackSubmissionOutcome := func(distinctID, outcome string) {
		properties := make(map[string]interface{}, len(baseProperties)+1)
		for key, value := range baseProperties {
			properties[key] = value
		}
		properties["outcome"] = outcome
		s.tracker.Track(ctx, analytics.EventReviewSubmitted, distinctID, properties)
	}

	// Verify captcha (Cloudflare Turnstile)
	if err := s.captchaVerifier.Verify(req.CaptchaToken); err != nil {
		metrics.ReviewSubmissions.WithLabelValues("captcha_failed").Inc()
		trackSubmissionOutcome(analytics.AnonymousDistinctID(), "captcha_failed")
		logger.Warn("Turnstile verification failed for review",
			zap.String("request_ref", requestRef),
			zap.Error(err))
		return &models.SubmitReviewResponse{
			Success: false,
			Error:   "Captcha verification failed",
		}, ErrReviewCaptchaFailed
	}

	// Check eligibility
	checkResult, err := s.reviewRepo.CheckCanSubmitReview(ctx, requestID)
	if err != nil {
		metrics.ReviewSubmissions.WithLabelValues("error").Inc()
		trackSubmissionOutcome(analytics.AnonymousDistinctID(), "validation_error")
		logger.Error("Failed to check review eligibility",
			zap.String("request_ref", requestRef),
			zap.Error(err))
		return &models.SubmitReviewResponse{
			Success: false,
			Error:   "Failed to validate request",
		}, fmt.Errorf("failed to check review eligibility: %w", err)
	}

	// Outcomes from here on are attributed to the mentor being reviewed; an
	// unknown request has no mentor, and MentorDistinctID("") is the anonymous id.
	mentorDistinctID := analytics.MentorDistinctID(checkResult.MentorID)

	if checkResult.MentorName == "" && !checkResult.CanSubmit {
		metrics.ReviewSubmissions.WithLabelValues("not_found").Inc()
		trackSubmissionOutcome(mentorDistinctID, "not_found")
		return &models.SubmitReviewResponse{
			Success: false,
			Error:   "Request not found",
		}, ErrReviewRequestNotFound
	}

	if !checkResult.CanSubmit {
		metrics.ReviewSubmissions.WithLabelValues("already_exists").Inc()
		trackSubmissionOutcome(mentorDistinctID, "already_exists")
		return &models.SubmitReviewResponse{
			Success: false,
			Error:   "Review already submitted or the request is not completed yet",
		}, ErrReviewAlreadyExists
	}

	// Create review
	reviewID, err := s.reviewRepo.CreateReview(ctx, requestID, req.MentorReview, req.PlatformReview, req.Improvements)
	if err != nil {
		metrics.ReviewSubmissions.WithLabelValues("db_error").Inc()
		trackSubmissionOutcome(mentorDistinctID, "db_error")
		logger.Error("Failed to create review",
			zap.String("request_ref", requestRef),
			logger.RedactedError(err))
		return &models.SubmitReviewResponse{
			Success: false,
			Error:   "Failed to save review",
		}, fmt.Errorf("failed to create review: %w", err)
	}

	// Trigger Azure Function notification (non-blocking)
	trigger.CallAsync(ctx, s.config.EventTriggers.ReviewCreatedTriggerURL, reviewID, s.config.Worker.AuthToken, s.httpClient)

	duration := metrics.MeasureDuration(start)
	metrics.ReviewDuration.Observe(duration)
	metrics.ReviewSubmissions.WithLabelValues("success").Inc()
	successProperties := make(map[string]interface{}, len(baseProperties)+4)
	for key, value := range baseProperties {
		successProperties[key] = value
	}
	successProperties["mentor_id"] = checkResult.MentorID
	successProperties["review_id"] = reviewID
	successProperties["duration_seconds"] = duration
	successProperties["outcome"] = "success"
	s.tracker.Track(ctx, analytics.EventReviewSubmitted, mentorDistinctID, successProperties)
	logger.Info("Review submitted successfully",
		zap.String("request_ref", requestRef),
		zap.String("review_id", reviewID),
		zap.Duration("duration", time.Since(start)))

	return &models.SubmitReviewResponse{
		Success:  true,
		ReviewID: reviewID,
	}, nil
}

func reviewSubmissionProperties(req *models.SubmitReviewRequest) map[string]interface{} {
	return map[string]interface{}{
		"has_platform_review":  strings.TrimSpace(req.PlatformReview) != "",
		"has_improvements":     strings.TrimSpace(req.Improvements) != "",
		"has_mentor_review":    strings.TrimSpace(req.MentorReview) != "",
		"review_payload_size":  len(req.MentorReview) + len(req.PlatformReview) + len(req.Improvements),
		"captcha_token_length": len(req.CaptchaToken),
	}
}
