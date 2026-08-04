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
	"github.com/openmentor-io/openmentor/api/pkg/reviewtoken"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
	"go.uber.org/zap"
)

var (
	ErrReviewRequestNotFound = errors.New("request not found")
	ErrReviewRequestNotDone  = errors.New("request is not in done status")
	ErrReviewAlreadyExists   = errors.New("review already exists for this request")
	ErrReviewCaptchaFailed   = errors.New("captcha verification failed")

	// ErrReviewLinkInvalid is the single answer for a capability that cannot be
	// spent — unknown, already consumed, or expired. Callers map it to one status
	// and one message, because distinguishing the three is itself a disclosure.
	ErrReviewLinkInvalid = errors.New("review link is no longer valid")

	// ErrReviewLegacyPathDisabled is returned once the operator flips
	// REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED off — the H4 cutover.
	ErrReviewLegacyPathDisabled = errors.New("legacy review links are no longer accepted")
)

// invalidLinkMessage is the copy every dead-capability outcome shares. It names
// no request, no mentor and no reason.
const invalidLinkMessage = "This review link has already been used or has expired."

// ineligibleMessage is the copy for a valid capability against a request that
// cannot be reviewed (not completed yet, or already reviewed).
const ineligibleMessage = "Review already submitted or the request is not completed yet"

// legacyDisabledMessage is what a request-id link gets after the cutover.
const legacyDisabledMessage = "This review link is no longer supported. Please write to hello@openmentor.io for a new one."

// Analytics `link_type` values. Sanitized enums, never the capability itself —
// the point is that an operator can watch legacy usage fall to zero (the signal
// the cutover is gated on) without the capability being in the event.
const (
	reviewLinkTypeToken  = "token"
	reviewLinkTypeLegacy = "legacy_request_id"
)

// ReviewRepository is the review repository surface the review service needs.
// *repository.ReviewRepository satisfies it; tests substitute a fake.
type ReviewRepository interface {
	// Token path (H4).
	CheckCanSubmitReviewByToken(ctx context.Context, tokenHash string) (*repository.ReviewCheckResult, error)
	SubmitReviewWithToken(ctx context.Context, tokenHash string, content repository.ReviewContent) (*repository.ReviewSubmission, error)

	// Legacy request-id path, live only during the dual-read window.
	CheckCanSubmitReview(ctx context.Context, requestID string) (*repository.ReviewCheckResult, error)
	SubmitReviewForRequest(ctx context.Context, requestID string, content repository.ReviewContent) (*repository.ReviewSubmission, error)
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

// trackCheck records one eligibility check. mentorID is "" when the capability
// did not resolve, which makes the distinct id anonymous.
func (s *ReviewService) trackCheck(ctx context.Context, linkType, mentorID, outcome string, canSubmit bool) {
	metrics.ReviewChecks.WithLabelValues(outcome).Inc()
	properties := map[string]interface{}{
		"link_type":  linkType,
		"outcome":    outcome,
		"can_submit": canSubmit,
	}
	if mentorID != "" {
		properties["mentor_id"] = mentorID
	}
	s.tracker.Track(ctx, analytics.EventReviewEligibilityChecked, analytics.MentorDistinctID(mentorID), properties)
}

// CheckReviewByToken reports whether a capability can still be spent, and only
// then who it is for. It does NOT consume the token: the review page calls this
// on load, and a page view must not burn the mentee's one submission.
func (s *ReviewService) CheckReviewByToken(ctx context.Context, rawToken string) (*models.ReviewCheckResponse, error) {
	// A malformed value is answered without a database round trip, and answered
	// identically to a dead one — the shape of the value must not be observable.
	if !reviewtoken.WellFormed(rawToken) {
		s.trackCheck(ctx, reviewLinkTypeToken, "", "invalid_link", false)
		return &models.ReviewCheckResponse{CanSubmit: false, Error: invalidLinkMessage}, ErrReviewLinkInvalid
	}

	result, err := s.reviewRepo.CheckCanSubmitReviewByToken(ctx, reviewtoken.Hash(rawToken))
	switch {
	case errors.Is(err, repository.ErrReviewTokenInvalid):
		s.trackCheck(ctx, reviewLinkTypeToken, "", "invalid_link", false)
		logger.Info("Review check: link not spendable", zap.String("link_type", reviewLinkTypeToken))
		return &models.ReviewCheckResponse{CanSubmit: false, Error: invalidLinkMessage}, ErrReviewLinkInvalid
	case err != nil:
		s.trackCheck(ctx, reviewLinkTypeToken, "", "error", false)
		logger.Error("Failed to check review eligibility",
			zap.String("link_type", reviewLinkTypeToken), logger.RedactedError(err))
		return nil, fmt.Errorf("failed to check review: %w", err)
	}

	return s.checkResponse(ctx, reviewLinkTypeToken, result), nil
}

// CheckReview is the LEGACY check, authorizing on client_requests.id. Live only
// while REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED is on.
func (s *ReviewService) CheckReview(ctx context.Context, requestID string) (*models.ReviewCheckResponse, error) {
	if !s.config.Review.LegacyRequestIDLinksEnabled {
		metrics.ReviewLegacyLinkUses.WithLabelValues("check", "refused").Inc()
		return &models.ReviewCheckResponse{CanSubmit: false, Error: legacyDisabledMessage}, ErrReviewLegacyPathDisabled
	}
	metrics.ReviewLegacyLinkUses.WithLabelValues("check", "accepted").Inc()

	// SECURITY (P14): the request id is a bearer capability for this flow, so it
	// never leaves this function verbatim — telemetry gets the mentor it belongs
	// to plus a hashed reference for correlating log lines.
	requestRef := redact.ID(requestID)

	result, err := s.reviewRepo.CheckCanSubmitReview(ctx, requestID)
	if err != nil {
		s.trackCheck(ctx, reviewLinkTypeLegacy, "", "error", false)
		logger.Error("Failed to check review eligibility",
			zap.String("request_ref", requestRef),
			logger.RedactedError(err))
		return nil, fmt.Errorf("failed to check review: %w", err)
	}

	// An unknown request and a request whose legacy link was revoked both arrive
	// here with no mentor: identical answers, by design.
	if result.MentorName == "" && !result.CanSubmit {
		s.trackCheck(ctx, reviewLinkTypeLegacy, "", "not_found", false)
		logger.Info("Review check: request not found", zap.String("request_ref", requestRef))
		return &models.ReviewCheckResponse{
			CanSubmit: false,
			Error:     "Request not found",
		}, ErrReviewRequestNotFound
	}

	logger.Info("Review check completed",
		zap.String("request_ref", requestRef),
		zap.String("mentor_id", result.MentorID),
		zap.Bool("can_submit", result.CanSubmit))
	return s.checkResponse(ctx, reviewLinkTypeLegacy, result), nil
}

func (s *ReviewService) checkResponse(ctx context.Context, linkType string, result *repository.ReviewCheckResult) *models.ReviewCheckResponse {
	if !result.CanSubmit {
		s.trackCheck(ctx, linkType, result.MentorID, "ineligible", false)
		return &models.ReviewCheckResponse{
			CanSubmit:  false,
			MentorName: result.MentorName,
			Error:      ineligibleMessage,
		}
	}
	s.trackCheck(ctx, linkType, result.MentorID, "eligible", true)
	return &models.ReviewCheckResponse{
		CanSubmit:  true,
		MentorName: result.MentorName,
	}
}

// submissionTracker builds the per-attempt analytics closure. The base
// properties carry only derived facts about the payload — sizes and booleans,
// never the text and never the capability.
func (s *ReviewService) submissionTracker(ctx context.Context, linkType string, req *models.SubmitReviewRequest) func(mentorID, outcome string, extra map[string]interface{}) {
	baseProperties := reviewSubmissionProperties(linkType, req)
	return func(mentorID, outcome string, extra map[string]interface{}) {
		metrics.ReviewSubmissions.WithLabelValues(outcome).Inc()
		properties := make(map[string]interface{}, len(baseProperties)+len(extra)+2)
		for key, value := range baseProperties {
			properties[key] = value
		}
		for key, value := range extra {
			properties[key] = value
		}
		properties["outcome"] = outcome
		if mentorID != "" {
			properties["mentor_id"] = mentorID
		}
		s.tracker.Track(ctx, analytics.EventReviewSubmitted, analytics.MentorDistinctID(mentorID), properties)
	}
}

// SubmitReviewWithToken spends a capability and stores the review.
//
// Two submissions with the same token yield exactly one success and one
// ErrReviewLinkInvalid (HTTP 409) — enforced by the compare-and-swap in
// repository.SubmitReviewWithToken, not by anything in this function.
func (s *ReviewService) SubmitReviewWithToken(ctx context.Context, rawToken string, req *models.SubmitReviewRequest) (*models.SubmitReviewResponse, error) {
	start := time.Now()
	track := s.submissionTracker(ctx, reviewLinkTypeToken, req)

	if err := s.captchaVerifier.Verify(req.CaptchaToken); err != nil {
		track("", "captcha_failed", nil)
		logger.Warn("Turnstile verification failed for review",
			zap.String("link_type", reviewLinkTypeToken), logger.RedactedError(err))
		return &models.SubmitReviewResponse{Success: false, Error: "Captcha verification failed"}, ErrReviewCaptchaFailed
	}

	if !reviewtoken.WellFormed(rawToken) {
		track("", "invalid_link", nil)
		return &models.SubmitReviewResponse{Success: false, Error: invalidLinkMessage}, ErrReviewLinkInvalid
	}

	submission, err := s.reviewRepo.SubmitReviewWithToken(ctx, reviewtoken.Hash(rawToken), reviewContent(req))
	if err != nil {
		return s.submissionFailure(reviewLinkTypeToken, track, err)
	}

	return s.submissionSuccess(ctx, track, submission, start), nil
}

// SubmitReview is the LEGACY submission path, authorizing on client_requests.id.
// Live only while REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED is on.
func (s *ReviewService) SubmitReview(ctx context.Context, requestID string, req *models.SubmitReviewRequest) (*models.SubmitReviewResponse, error) {
	start := time.Now()
	if !s.config.Review.LegacyRequestIDLinksEnabled {
		metrics.ReviewLegacyLinkUses.WithLabelValues("submit", "refused").Inc()
		return &models.SubmitReviewResponse{Success: false, Error: legacyDisabledMessage}, ErrReviewLegacyPathDisabled
	}
	metrics.ReviewLegacyLinkUses.WithLabelValues("submit", "accepted").Inc()

	// SECURITY (P14): see CheckReview — the request id stays out of telemetry.
	requestRef := redact.ID(requestID)
	track := s.submissionTracker(ctx, reviewLinkTypeLegacy, req)

	if err := s.captchaVerifier.Verify(req.CaptchaToken); err != nil {
		track("", "captcha_failed", nil)
		logger.Warn("Turnstile verification failed for review",
			zap.String("request_ref", requestRef), logger.RedactedError(err))
		return &models.SubmitReviewResponse{Success: false, Error: "Captcha verification failed"}, ErrReviewCaptchaFailed
	}

	submission, err := s.reviewRepo.SubmitReviewForRequest(ctx, requestID, reviewContent(req))
	if err != nil {
		logger.Info("Legacy review submission rejected",
			zap.String("request_ref", requestRef), logger.RedactedError(err))
		return s.submissionFailure(reviewLinkTypeLegacy, track, err)
	}

	return s.submissionSuccess(ctx, track, submission, start), nil
}

// submissionFailure maps a repository error onto the response and the typed
// service error the handler turns into a status code.
func (s *ReviewService) submissionFailure(
	linkType string,
	track func(mentorID, outcome string, extra map[string]interface{}),
	err error,
) (*models.SubmitReviewResponse, error) {

	switch {
	case errors.Is(err, repository.ErrReviewTokenInvalid):
		track("", "invalid_link", nil)
		return &models.SubmitReviewResponse{Success: false, Error: invalidLinkMessage}, ErrReviewLinkInvalid
	case errors.Is(err, repository.ErrReviewIneligible):
		track("", "already_exists", nil)
		return &models.SubmitReviewResponse{Success: false, Error: ineligibleMessage}, ErrReviewAlreadyExists
	default:
		track("", "db_error", nil)
		logger.Error("Failed to create review",
			zap.String("link_type", linkType), logger.RedactedError(err))
		return &models.SubmitReviewResponse{Success: false, Error: "Failed to save review"},
			fmt.Errorf("failed to create review: %w", err)
	}
}

func (s *ReviewService) submissionSuccess(
	ctx context.Context,
	track func(mentorID, outcome string, extra map[string]interface{}),
	submission *repository.ReviewSubmission,
	start time.Time,
) *models.SubmitReviewResponse {
	// Trigger the review-created notification (non-blocking).
	trigger.CallAsync(ctx, s.config.EventTriggers.ReviewCreatedTriggerURL, submission.ReviewID, s.config.Worker.AuthToken, s.httpClient)

	duration := metrics.MeasureDuration(start)
	metrics.ReviewDuration.Observe(duration)
	track(submission.MentorID, "success", map[string]interface{}{
		"review_id":        submission.ReviewID,
		"duration_seconds": duration,
	})
	logger.Info("Review submitted successfully",
		zap.String("review_id", submission.ReviewID),
		zap.String("mentor_id", submission.MentorID),
		zap.Duration("duration", time.Since(start)))

	return &models.SubmitReviewResponse{Success: true, ReviewID: submission.ReviewID}
}

func reviewContent(req *models.SubmitReviewRequest) repository.ReviewContent {
	return repository.ReviewContent{
		MentorReview:   req.MentorReview,
		PlatformReview: req.PlatformReview,
		Improvements:   req.Improvements,
	}
}

func reviewSubmissionProperties(linkType string, req *models.SubmitReviewRequest) map[string]interface{} {
	return map[string]interface{}{
		"link_type":            linkType,
		"has_platform_review":  strings.TrimSpace(req.PlatformReview) != "",
		"has_improvements":     strings.TrimSpace(req.Improvements) != "",
		"has_mentor_review":    strings.TrimSpace(req.MentorReview) != "",
		"review_payload_size":  len(req.MentorReview) + len(req.PlatformReview) + len(req.Improvements),
		"captcha_token_length": len(req.CaptchaToken),
	}
}
