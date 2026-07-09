package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/openmentor-io/openmentor-api/config"
	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/openmentor-io/openmentor-api/internal/repository"
	"github.com/openmentor-io/openmentor-api/pkg/analytics"
	"github.com/openmentor-io/openmentor-api/pkg/httpclient"
	"github.com/openmentor-io/openmentor-api/pkg/jwt"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"github.com/openmentor-io/openmentor-api/pkg/trigger"
	"go.uber.org/zap"
)

var (
	ErrModeratorNotFound      = errors.New("moderator not found")
	ErrModeratorNotEligible   = errors.New("moderator not eligible for login")
	ErrAdminInvalidLoginToken = errors.New("invalid or expired admin login token")
	ErrAdminJWTSecretNotSet   = errors.New("JWT secret not configured")
	ErrAdminTokenGeneration   = errors.New("failed to generate admin login token")
)

// AdminAuthService handles moderator/admin one-time login flow.
type AdminAuthService struct {
	moderatorRepo *repository.ModeratorRepository
	config        *config.Config
	tokenManager  *jwt.TokenManager
	httpClient    httpclient.Client
	tracker       analytics.Tracker
}

func NewAdminAuthService(
	moderatorRepo *repository.ModeratorRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *AdminAuthService {

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

	return &AdminAuthService{
		moderatorRepo: moderatorRepo,
		config:        cfg,
		tokenManager:  tokenManager,
		httpClient:    httpClient,
		tracker:       tracker,
	}
}

func (s *AdminAuthService) RequestLogin(ctx context.Context, email string) (*models.AdminRequestLoginResponse, error) {
	moderator, err := s.moderatorRepo.GetByEmail(ctx, email)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginRequested, analytics.SystemDistinctID("api"), map[string]interface{}{
			"outcome": "moderator_not_found",
		})
		logger.Warn("Admin login request for unknown email", zap.String("email", email), zap.Error(err))
		return nil, ErrModeratorNotFound
	}
	if !moderator.Role.IsValid() {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginRequested, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
			"moderator_id": moderator.ID,
			"role":         string(moderator.Role),
			"outcome":      "not_eligible",
		})
		logger.Warn("Admin login request with invalid role",
			zap.String("moderator_id", moderator.ID),
			zap.String("role", string(moderator.Role)))
		return nil, ErrModeratorNotEligible
	}

	token, err := generateAdminLoginToken()
	if err != nil {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginRequested, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
			"moderator_id": moderator.ID,
			"role":         string(moderator.Role),
			"outcome":      "token_generation_failed",
		})
		logger.Error("Failed to generate admin login token", zap.Error(err))
		return nil, ErrAdminTokenGeneration
	}

	expiration := time.Now().Add(time.Duration(s.config.MentorSession.LoginTokenTTLMinutes) * time.Minute)
	if err := s.moderatorRepo.SetLoginToken(ctx, moderator.ID, token, expiration); err != nil {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginRequested, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
			"moderator_id": moderator.ID,
			"role":         string(moderator.Role),
			"outcome":      "storage_failed",
		})
		return nil, fmt.Errorf("failed to store admin login token: %w", err)
	}

	loginURL := fmt.Sprintf("%s/admin/auth/callback?token=%s", s.config.Server.BaseURL, token)
	if s.config.EventTriggers.ModeratorLoginEmailTriggerURL != "" {
		payload := map[string]interface{}{
			"type":            "admin_login",
			"moderator_id":    moderator.ID,
			"moderator_name":  moderator.Name,
			"moderator_email": moderator.Email,
			"login_url":       loginURL,
		}
		trigger.CallAsyncWithPayload(ctx, s.config.EventTriggers.ModeratorLoginEmailTriggerURL, payload, s.config.Worker.AuthToken, s.httpClient)
	} else if s.config.IsDevelopment() {
		logger.Info("=== DEVELOPMENT ADMIN LOGIN URL ===",
			zap.String("moderator_email", moderator.Email),
			zap.String("moderator_name", moderator.Name),
			zap.String("login_url", loginURL))
	}
	s.tracker.Track(ctx, analytics.EventAdminAuthLoginRequested, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
		"moderator_id":            moderator.ID,
		"role":                    string(moderator.Role),
		"login_token_ttl_minutes": s.config.MentorSession.LoginTokenTTLMinutes,
		"outcome":                 "success",
	})

	return &models.AdminRequestLoginResponse{
		Success: true,
		Message: "We've sent a login link to your email",
	}, nil
}

func (s *AdminAuthService) VerifyLogin(ctx context.Context, token string) (*models.AdminSession, string, error) {
	if s.tokenManager == nil {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.SystemDistinctID("api"), map[string]interface{}{
			"outcome": "not_configured",
		})
		return nil, "", ErrAdminJWTSecretNotSet
	}

	moderator, tokenExp, err := s.moderatorRepo.GetByLoginToken(ctx, token)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.SystemDistinctID("api"), map[string]interface{}{
			"outcome": "invalid_token",
		})
		return nil, "", ErrAdminInvalidLoginToken
	}
	if time.Now().After(tokenExp) {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
			"moderator_id": moderator.ID,
			"outcome":      "expired",
		})
		return nil, "", ErrAdminInvalidLoginToken
	}
	if !moderator.Role.IsValid() {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
			"moderator_id": moderator.ID,
			"role":         string(moderator.Role),
			"outcome":      "not_eligible",
		})
		return nil, "", ErrModeratorNotEligible
	}

	if clearErr := s.moderatorRepo.ClearLoginToken(ctx, moderator.ID); clearErr != nil {
		logger.Error("Failed to clear admin login token",
			zap.String("moderator_id", moderator.ID),
			zap.Error(clearErr))
	}

	jwtToken, err := s.tokenManager.GenerateTokenWithRole(
		moderator.ID,
		0,
		moderator.Email,
		moderator.Name,
		string(moderator.Role),
	)
	if err != nil {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
			"moderator_id": moderator.ID,
			"role":         string(moderator.Role),
			"outcome":      "jwt_failed",
		})
		return nil, "", fmt.Errorf("failed to generate admin session token: %w", err)
	}

	now := time.Now()
	session := &models.AdminSession{
		ModeratorID: moderator.ID,
		Email:       moderator.Email,
		Name:        moderator.Name,
		Role:        moderator.Role,
		ExpiresAt:   now.Add(s.tokenManager.GetExpirationTime()).Unix(),
		IssuedAt:    now.Unix(),
	}
	s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
		"moderator_id":      moderator.ID,
		"role":              string(moderator.Role),
		"session_ttl_hours": s.config.MentorSession.SessionTTLHours,
		"outcome":           "success",
	})

	return session, jwtToken, nil
}

func (s *AdminAuthService) GetSessionTTL() int {
	return s.config.MentorSession.SessionTTLHours * 3600
}

func (s *AdminAuthService) GetCookieDomain() string {
	return s.config.MentorSession.CookieDomain
}

func (s *AdminAuthService) GetCookieSecure() bool {
	return s.config.MentorSession.CookieSecure
}

func (s *AdminAuthService) GetTokenManager() *jwt.TokenManager {
	return s.tokenManager
}

func generateAdminLoginToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	timestamp := time.Now().Unix()
	return fmt.Sprintf("atk_%s_%d", hex.EncodeToString(bytes), timestamp), nil
}
