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
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
	"go.uber.org/zap"
)

// ErrMentorNotContactable is returned when the contact form names a mentor who
// is not accepting requests (never approved, declined, hidden or deleted). The
// verdict comes from the INSERT itself — see
// repository.ClientRequestRepository.Create — so there is no window in which a
// request row exists for a mentor this refuses.
var ErrMentorNotContactable = errors.New("this mentor is not accepting new requests")

// ContactClientRequestRepository is the client-request surface ContactService
// needs. *repository.ClientRequestRepository satisfies it; tests substitute a
// fake (mirrors ProfileMentorRepository).
type ContactClientRequestRepository interface {
	Create(ctx context.Context, req *models.ClientRequest) (*repository.CreatedClientRequest, error)
}

var _ ContactClientRequestRepository = (*repository.ClientRequestRepository)(nil)

// ContactService handles contact form submissions and mentor contact requests
//
// It holds NO mentor repository on purpose (C3): it used to read the mentor after
// creating the request, purely for the calendar link, and that read was both too
// late to gate anything and willing to hand out an inactive mentor's calendar
// URL. The gated INSERT returns the link, or refuses the write.
type ContactService struct {
	clientRequestRepo ContactClientRequestRepository
	config            *config.Config
	httpClient        httpclient.Client
	captchaVerifier   *turnstile.Verifier
	tracker           analytics.Tracker
}

// NewContactService creates a new contact service instance
func NewContactService(
	clientRequestRepo ContactClientRequestRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *ContactService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &ContactService{
		clientRequestRepo: clientRequestRepo,
		config:            cfg,
		httpClient:        httpClient,
		captchaVerifier:   newCaptchaVerifier(cfg, httpClient),
		tracker:           tracker,
	}
}

func (s *ContactService) SubmitContactForm(ctx context.Context, req *models.ContactMentorRequest) (*models.ContactMentorResponse, error) {
	baseProperties := map[string]interface{}{
		"mentor_id":              req.MentorID,
		"experience":             req.Experience,
		"has_contact":            strings.TrimSpace(req.PreferredContact) != "",
		"calendar_url_requested": true,
	}

	// Verify captcha (Cloudflare Turnstile)
	if err := s.captchaVerifier.Verify(ctx, req.CaptchaToken); err != nil {
		metrics.ContactFormSubmissions.WithLabelValues("captcha_failed").Inc()
		s.tracker.Track(ctx, analytics.EventMenteeContactSubmitted, analytics.MentorDistinctID(req.MentorID), map[string]interface{}{
			"mentor_id":              req.MentorID,
			"experience":             req.Experience,
			"has_contact":            strings.TrimSpace(req.PreferredContact) != "",
			"calendar_url_requested": true,
			"outcome":                "captcha_failed",
		})
		logger.Warn("Turnstile verification failed", zap.Error(err))
		return &models.ContactMentorResponse{
			Success: false,
			Error:   "Captcha verification failed",
		}, fmt.Errorf("captcha verification failed: %w", err)
	}

	// Create client request in PostgreSQL. The mentor's eligibility is decided by
	// this statement, not by a read before it (C3), and the calendar link comes
	// back from the same statement — so a mentor who is not contactable gets
	// neither a request row nor their calendar URL disclosed.
	clientReq := &models.ClientRequest{
		Email:            req.Email,
		Name:             req.Name,
		Level:            req.Experience,
		MentorID:         req.MentorID,
		Description:      req.Intro,
		PreferredContact: strings.TrimSpace(req.PreferredContact),
	}

	created, err := s.clientRequestRepo.Create(ctx, clientReq)
	if err != nil {
		if errors.Is(err, repository.ErrMentorNotContactable) {
			metrics.ContactFormSubmissions.WithLabelValues("mentor_not_contactable").Inc()
			s.tracker.Track(ctx, analytics.EventMenteeContactSubmitted, analytics.MentorDistinctID(req.MentorID), map[string]interface{}{
				"mentor_id":              req.MentorID,
				"experience":             req.Experience,
				"has_contact":            strings.TrimSpace(req.PreferredContact) != "",
				"calendar_url_requested": true,
				"calendar_url_available": false,
				"outcome":                "mentor_not_contactable",
			})
			logger.Warn("Contact request rejected: mentor is not accepting requests",
				zap.String("mentor_id", req.MentorID))
			return &models.ContactMentorResponse{
				Success: false,
				// Same wording the profile page shows when it hides the form, so
				// the two never contradict each other.
				Error:  "This mentor is temporarily not accepting new requests.",
				Reason: "mentor_not_contactable",
			}, ErrMentorNotContactable
		}
		metrics.ContactFormSubmissions.WithLabelValues("error").Inc()
		s.tracker.Track(ctx, analytics.EventMenteeContactSubmitted, analytics.MentorDistinctID(req.MentorID), map[string]interface{}{
			"mentor_id":              req.MentorID,
			"experience":             req.Experience,
			"has_contact":            strings.TrimSpace(req.PreferredContact) != "",
			"calendar_url_requested": true,
			"outcome":                "db_error",
		})
		logger.Error("Failed to create client request", zap.Error(err))
		return &models.ContactMentorResponse{
			Success: false,
			Error:   "Failed to save contact request",
		}, fmt.Errorf("failed to create client request: %w", err)
	}

	// Trigger contact created webhook (non-blocking)
	trigger.CallAsync(ctx, s.config.EventTriggers.MentorRequestCreatedTriggerURL, created.ID, s.config.Worker.AuthToken, s.httpClient)

	metrics.ContactFormSubmissions.WithLabelValues("success").Inc()
	successProperties := make(map[string]interface{}, len(baseProperties)+2)
	for key, value := range baseProperties {
		successProperties[key] = value
	}
	successProperties["calendar_url_available"] = strings.TrimSpace(created.CalendarURL) != ""
	successProperties["outcome"] = "success"
	// SECURITY (P14): keyed by mentor, never by the new request id — that id is
	// the capability the confirmation email hands the mentee for the review flow.
	s.tracker.Track(ctx, analytics.EventMenteeContactSubmitted, analytics.MentorDistinctID(req.MentorID), successProperties)
	return &models.ContactMentorResponse{
		Success:     true,
		RequestID:   created.ID,
		CalendarURL: created.CalendarURL,
	}, nil
}
