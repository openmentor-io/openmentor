package services

import (
	"context"
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

// ContactService handles contact form submissions and mentor contact requests
type ContactService struct {
	clientRequestRepo *repository.ClientRequestRepository
	mentorRepo        *repository.MentorRepository
	config            *config.Config
	httpClient        httpclient.Client
	captchaVerifier   *turnstile.Verifier
	tracker           analytics.Tracker
}

// NewContactService creates a new contact service instance
func NewContactService(
	clientRequestRepo *repository.ClientRequestRepository,
	mentorRepo *repository.MentorRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *ContactService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &ContactService{
		clientRequestRepo: clientRequestRepo,
		mentorRepo:        mentorRepo,
		config:            cfg,
		httpClient:        httpClient,
		captchaVerifier:   turnstile.NewVerifier(cfg.Turnstile.SecretKey, httpClient),
		tracker:           tracker,
	}
}

func (s *ContactService) SubmitContactForm(ctx context.Context, req *models.ContactMentorRequest) (*models.ContactMentorResponse, error) {
	baseProperties := map[string]interface{}{
		"mentor_id":              req.MentorID,
		"experience":             req.Experience,
		"has_telegram_username":  strings.TrimSpace(req.TelegramUsername) != "",
		"calendar_url_requested": true,
	}

	// Verify captcha (Cloudflare Turnstile)
	if err := s.captchaVerifier.Verify(req.CaptchaToken); err != nil {
		metrics.ContactFormSubmissions.WithLabelValues("captcha_failed").Inc()
		s.tracker.Track(ctx, analytics.EventMenteeContactSubmitted, analytics.MentorDistinctID(req.MentorID), map[string]interface{}{
			"mentor_id":              req.MentorID,
			"experience":             req.Experience,
			"has_telegram_username":  strings.TrimSpace(req.TelegramUsername) != "",
			"calendar_url_requested": true,
			"outcome":                "captcha_failed",
		})
		logger.Warn("Turnstile verification failed", zap.Error(err))
		return &models.ContactMentorResponse{
			Success: false,
			Error:   "Captcha verification failed",
		}, fmt.Errorf("captcha verification failed: %w", err)
	}

	// Create client request in PostgreSQL
	clientReq := &models.ClientRequest{
		Email:       req.Email,
		Name:        req.Name,
		Level:       req.Experience,
		MentorID:    req.MentorID,
		Description: req.Intro,
		Telegram:    normalizeTelegramHandle(req.TelegramUsername),
	}

	requestID, err := s.clientRequestRepo.Create(ctx, clientReq)
	if err != nil {
		metrics.ContactFormSubmissions.WithLabelValues("error").Inc()
		s.tracker.Track(ctx, analytics.EventMenteeContactSubmitted, analytics.MentorDistinctID(req.MentorID), map[string]interface{}{
			"mentor_id":              req.MentorID,
			"experience":             req.Experience,
			"has_telegram_username":  strings.TrimSpace(req.TelegramUsername) != "",
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
	trigger.CallAsync(ctx, s.config.EventTriggers.MentorRequestCreatedTriggerURL, requestID, s.config.Worker.AuthToken, s.httpClient)

	// Get mentor to retrieve calendar URL
	mentor, err := s.mentorRepo.GetByMentorId(ctx, req.MentorID, models.FilterOptions{ShowHidden: true})
	if err != nil {
		logger.Error("Failed to get mentor for calendar URL", zap.Error(err))
		// Still return success as the request was saved
		metrics.ContactFormSubmissions.WithLabelValues("success").Inc()
		s.tracker.Track(ctx, analytics.EventMenteeContactSubmitted, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"mentor_id":              req.MentorID,
			"request_id":             requestID,
			"experience":             req.Experience,
			"has_telegram_username":  strings.TrimSpace(req.TelegramUsername) != "",
			"calendar_url_requested": true,
			"calendar_url_available": false,
			"outcome":                "success",
		})
		return &models.ContactMentorResponse{
			Success:   true,
			RequestID: requestID,
		}, nil
	}

	metrics.ContactFormSubmissions.WithLabelValues("success").Inc()
	successProperties := make(map[string]interface{}, len(baseProperties)+4)
	for key, value := range baseProperties {
		successProperties[key] = value
	}
	successProperties["request_id"] = requestID
	successProperties["calendar_url_available"] = strings.TrimSpace(mentor.CalendarURL) != ""
	successProperties["outcome"] = "success"
	s.tracker.Track(ctx, analytics.EventMenteeContactSubmitted, analytics.RequestDistinctID(requestID), successProperties)
	return &models.ContactMentorResponse{
		Success:     true,
		RequestID:   requestID,
		CalendarURL: mentor.CalendarURL,
	}, nil
}
