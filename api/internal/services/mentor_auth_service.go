package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/jwt"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"go.uber.org/zap"
)

var (
	ErrMentorNotFound      = errors.New("mentor not found")
	ErrMentorNotEligible   = errors.New("mentor not eligible for login")
	ErrInvalidLoginToken   = errors.New("invalid or expired login token")
	ErrJWTSecretNotSet     = errors.New("JWT secret not configured")
	ErrTokenGenerationFail = errors.New("failed to generate login token")
)

// MentorAuthService handles mentor authentication
type MentorAuthService struct {
	mentorRepo   *repository.MentorRepository
	config       *config.Config
	tokenManager *jwt.TokenManager
	httpClient   httpclient.Client
	tracker      analytics.Tracker
}

// NewMentorAuthService creates a new MentorAuthService
func NewMentorAuthService(
	mentorRepo *repository.MentorRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *MentorAuthService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	var tokenManager *jwt.TokenManager
	if cfg.MentorSession.JWTSecret != "" {
		tokenManager = jwt.NewTokenManager(
			cfg.MentorSession.JWTSecret,
			cfg.MentorSession.JWTIssuer,
			cfg.MentorSession.SessionTTLHours,
		)
	}

	return &MentorAuthService{
		mentorRepo:   mentorRepo,
		config:       cfg,
		tokenManager: tokenManager,
		httpClient:   httpClient,
		tracker:      tracker,
	}
}

// RequestLogin generates a login token and triggers email sending
func (s *MentorAuthService) RequestLogin(ctx context.Context, email string) (*models.RequestLoginResponse, error) {
	start := time.Now()

	// Find mentor by email
	mentor, err := s.mentorRepo.GetByEmail(ctx, email)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginRequested, analytics.SystemDistinctID("api"), map[string]interface{}{
			"outcome": "mentor_not_found",
		})
		logger.Warn("Login request for unknown email",
			zap.String("email", maskEmail(email)),
			zap.Error(err))
		metrics.MentorAuthLoginRequests.WithLabelValues("mentor_not_found").Inc()
		return nil, ErrMentorNotFound
	}

	// Check if mentor is eligible for login (draft/pending mentors may log
	// in to finish or fix their profile; declined mentors stay blocked)
	if !isLoginEligibleStatus(mentor.Status) {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginRequested, analytics.MentorDistinctID(mentor.MentorID), map[string]interface{}{
			"mentor_id":     mentor.MentorID,
			"mentor_status": mentor.Status,
			"outcome":       "not_eligible",
		})
		logger.Warn("Login request for mentor with ineligible status",
			zap.String("email", maskEmail(email)),
			zap.String("mentor_id", mentor.MentorID),
			zap.String("status", mentor.Status))
		metrics.MentorAuthLoginRequests.WithLabelValues("not_eligible").Inc()
		return nil, ErrMentorNotEligible
	}

	// Generate login token
	token, err := generateLoginToken()
	if err != nil {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginRequested, analytics.MentorDistinctID(mentor.MentorID), map[string]interface{}{
			"mentor_id": mentor.MentorID,
			"outcome":   "token_generation_failed",
		})
		logger.Error("Failed to generate login token", zap.Error(err))
		metrics.MentorAuthLoginRequests.WithLabelValues("token_generation_failed").Inc()
		return nil, ErrTokenGenerationFail
	}

	// Calculate expiration
	expiration := time.Now().Add(time.Duration(s.config.MentorSession.LoginTokenTTLMinutes) * time.Minute)

	// Store token in database
	if err := s.mentorRepo.SetLoginToken(ctx, mentor.MentorID, token, expiration); err != nil {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginRequested, analytics.MentorDistinctID(mentor.MentorID), map[string]interface{}{
			"mentor_id": mentor.MentorID,
			"outcome":   "storage_failed",
		})
		logger.Error("Failed to store login token",
			zap.String("mentor_id", mentor.MentorID),
			zap.Error(err))
		metrics.MentorAuthLoginRequests.WithLabelValues("storage_failed").Inc()
		return nil, fmt.Errorf("failed to store login token: %w", err)
	}

	// Build login URL
	loginURL := fmt.Sprintf("%s/mentor/auth/callback?token=%s", s.config.Server.BaseURL, token)

	// Trigger email sending via webhook
	if s.config.EventTriggers.MentorLoginEmailTriggerURL != "" {
		payload := map[string]interface{}{
			"type":      "mentor_login",
			"mentor_id": mentor.MentorID,
			"login_url": loginURL,
		}
		trigger.CallAsyncWithPayload(ctx, s.config.EventTriggers.MentorLoginEmailTriggerURL, payload, s.config.Worker.AuthToken, s.httpClient)
	} else if s.config.IsDevelopment() {
		// In development mode without email trigger, log the login URL to console
		logger.Info("=== DEVELOPMENT LOGIN URL ===",
			zap.String("mentor_email", maskEmail(email)),
			zap.String("mentor_name", mentor.Name),
			zap.String("login_url", loginURL))
	}

	duration := metrics.MeasureDuration(start)
	metrics.MentorAuthLoginDuration.Observe(duration)
	metrics.MentorAuthLoginRequests.WithLabelValues("success").Inc()
	s.tracker.Track(ctx, analytics.EventMentorAuthLoginRequested, analytics.MentorDistinctID(mentor.MentorID), map[string]interface{}{
		"mentor_id":                mentor.MentorID,
		"mentor_status":            mentor.Status,
		"login_token_ttl_minutes":  s.config.MentorSession.LoginTokenTTLMinutes,
		"request_duration_seconds": duration,
		"outcome":                  "success",
	})

	logger.Info("Login token generated",
		zap.String("mentor_id", mentor.MentorID),
		zap.Duration("duration", time.Since(start)))

	return &models.RequestLoginResponse{
		Success: true,
		Message: models.GenericLoginMessage,
	}, nil
}

// VerifyLogin verifies a login token and creates a session
func (s *MentorAuthService) VerifyLogin(ctx context.Context, token string) (*models.MentorSession, string, error) {
	start := time.Now()

	if s.tokenManager == nil {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginVerified, analytics.SystemDistinctID("api"), map[string]interface{}{
			"outcome": "not_configured",
		})
		logger.Error("JWT secret not configured")
		metrics.MentorAuthVerifyRequests.WithLabelValues("not_configured").Inc()
		return nil, "", ErrJWTSecretNotSet
	}

	// Find mentor by login token
	// Note: Token validation happens in the SQL WHERE clause (login_token = $1)
	// If a mentor is returned, the token was valid in the database
	mentor, tokenExp, err := s.mentorRepo.GetByLoginToken(ctx, token)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginVerified, analytics.SystemDistinctID("api"), map[string]interface{}{
			"outcome": "invalid_token",
		})
		logger.Warn("Login verification with invalid token", zap.Error(err))
		metrics.MentorAuthVerifyRequests.WithLabelValues("invalid_token").Inc()
		return nil, "", ErrInvalidLoginToken
	}

	// Check expiration
	if time.Now().After(tokenExp) {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginVerified, analytics.MentorDistinctID(mentor.MentorID), map[string]interface{}{
			"mentor_id": mentor.MentorID,
			"outcome":   "expired",
		})
		logger.Warn("Login token expired",
			zap.String("mentor_id", mentor.MentorID),
			zap.Time("expired_at", tokenExp))
		metrics.MentorAuthVerifyRequests.WithLabelValues("expired").Inc()
		return nil, "", ErrInvalidLoginToken
	}

	// Re-check mentor eligibility (status may have changed since token was issued)
	if !isLoginEligibleStatus(mentor.Status) {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginVerified, analytics.MentorDistinctID(mentor.MentorID), map[string]interface{}{
			"mentor_id":     mentor.MentorID,
			"mentor_status": mentor.Status,
			"outcome":       "not_eligible",
		})
		logger.Warn("Login verification for mentor with ineligible status",
			zap.String("mentor_id", mentor.MentorID),
			zap.String("status", mentor.Status))
		metrics.MentorAuthVerifyRequests.WithLabelValues("not_eligible").Inc()
		return nil, "", ErrMentorNotEligible
	}

	// Clear the login token (single-use)
	if clearErr := s.mentorRepo.ClearLoginToken(ctx, mentor.MentorID); clearErr != nil {
		logger.Error("Failed to clear login token",
			zap.String("mentor_id", mentor.MentorID),
			zap.Error(clearErr))
		// Continue with login even if clearing fails
	}

	// Generate JWT session token
	jwtToken, err := s.tokenManager.GenerateToken(mentor.MentorID, mentor.LegacyID, "", mentor.Name)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventMentorAuthLoginVerified, analytics.MentorDistinctID(mentor.MentorID), map[string]interface{}{
			"mentor_id": mentor.MentorID,
			"outcome":   "jwt_failed",
		})
		logger.Error("Failed to generate JWT",
			zap.String("mentor_id", mentor.MentorID),
			zap.Error(err))
		metrics.MentorAuthVerifyRequests.WithLabelValues("jwt_failed").Inc()
		return nil, "", fmt.Errorf("failed to generate session: %w", err)
	}

	now := time.Now()
	session := &models.MentorSession{
		LegacyID:  mentor.LegacyID,
		MentorID:  mentor.MentorID,
		Email:     "",
		Name:      mentor.Name,
		ExpiresAt: now.Add(s.tokenManager.GetExpirationTime()).Unix(),
		IssuedAt:  now.Unix(),
	}

	duration := metrics.MeasureDuration(start)
	metrics.MentorAuthVerifyDuration.Observe(duration)
	metrics.MentorAuthVerifyRequests.WithLabelValues("success").Inc()
	s.tracker.Track(ctx, analytics.EventMentorAuthLoginVerified, analytics.MentorDistinctID(mentor.MentorID), map[string]interface{}{
		"mentor_id":               mentor.MentorID,
		"mentor_status":           mentor.Status,
		"session_ttl_hours":       s.config.MentorSession.SessionTTLHours,
		"verify_duration_seconds": duration,
		"outcome":                 "success",
	})

	logger.Info("Login successful",
		zap.String("mentor_id", mentor.MentorID),
		zap.Duration("duration", time.Since(start)))

	return session, jwtToken, nil
}

// GetSessionTTL returns the session TTL in seconds
func (s *MentorAuthService) GetSessionTTL() int {
	return s.config.MentorSession.SessionTTLHours * 3600
}

// GetCookieDomain returns the cookie domain
func (s *MentorAuthService) GetCookieDomain() string {
	return s.config.MentorSession.CookieDomain
}

// GetCookieSecure returns whether cookies should be secure
func (s *MentorAuthService) GetCookieSecure() bool {
	return s.config.MentorSession.CookieSecure
}

// GetTokenManager returns the JWT token manager
func (s *MentorAuthService) GetTokenManager() *jwt.TokenManager {
	return s.tokenManager
}

// isLoginEligibleStatus reports whether a mentor with this status may use
// the magic-link login. Draft and pending mentors need portal access to
// complete/fix their profile; declined mentors stay blocked.
func isLoginEligibleStatus(status string) bool {
	switch status {
	case "draft", "pending", "active", "inactive":
		return true
	default:
		return false
	}
}

// generateLoginToken creates a secure random login token
func generateLoginToken() (string, error) {
	// Generate 32 random bytes (256 bits)
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	// Format: mtk_{random_hex}_{timestamp}
	timestamp := time.Now().Unix()
	return fmt.Sprintf("mtk_%s_%d", hex.EncodeToString(bytes), timestamp), nil
}
