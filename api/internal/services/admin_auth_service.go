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
	"github.com/openmentor-io/openmentor/api/pkg/redact"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
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
		logger.Warn("Admin login request for unknown email", zap.String("email", redact.Email(email)), zap.Error(err))
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
		logger.Info("Development admin login URL printed to stdout",
			zap.String("moderator_email", redact.Email(moderator.Email)),
			zap.String("moderator_id", moderator.ID))
		printDevLoginURL("DEVELOPMENT ADMIN LOGIN URL", loginURL)
	}
	s.tracker.Track(ctx, analytics.EventAdminAuthLoginRequested, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
		"moderator_id":            moderator.ID,
		"role":                    string(moderator.Role),
		"login_token_ttl_minutes": s.config.MentorSession.LoginTokenTTLMinutes,
		"outcome":                 "success",
	})

	return &models.AdminRequestLoginResponse{
		Success: true,
		Message: models.GenericLoginMessage,
	}, nil
}

func (s *AdminAuthService) VerifyLogin(ctx context.Context, token string) (*models.AdminSession, string, error) {
	if s.tokenManager == nil {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.SystemDistinctID("api"), map[string]interface{}{
			"outcome": "not_configured",
		})
		return nil, "", ErrAdminJWTSecretNotSet
	}

	// SECURITY (H1): one atomic UPDATE consumes the token, checks the expiry and
	// returns the row; the privileged session is minted only from that row. The
	// old shape read by token, compared the expiry in Go and cleared with a second
	// UPDATE whose failure it logged before minting a session anyway — so two
	// concurrent clicks on one admin link produced two 24-hour admin sessions, and
	// a transient DB error left a privileged link reusable.
	moderator, err := s.moderatorRepo.ConsumeLoginToken(ctx, token)
	if err != nil {
		outcome := "invalid_token"
		if !errors.Is(err, repository.ErrTokenNotConsumable) {
			outcome = outcomeConsumeFailed
		}
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.SystemDistinctID("api"), map[string]interface{}{
			"outcome": outcome,
		})
		if outcome == outcomeConsumeFailed {
			logger.Error("Admin login verification failed to consume the token", logger.RedactedError(err))
			return nil, "", fmt.Errorf("failed to verify admin login token: %w", err)
		}
		return nil, "", ErrAdminInvalidLoginToken
	}

	// Checked from the row the UPDATE returned. The token is already spent —
	// deliberately: a single-use credential presented by an account that may no
	// longer log in is used up, not handed back.
	if !moderator.Role.IsValid() {
		s.tracker.Track(ctx, analytics.EventAdminAuthLoginVerified, analytics.ModeratorDistinctID(moderator.ID), map[string]interface{}{
			"moderator_id": moderator.ID,
			"role":         string(moderator.Role),
			"outcome":      "not_eligible",
		})
		return nil, "", ErrModeratorNotEligible
	}

	jwtToken, err := s.tokenManager.GenerateTokenWithRole(
		moderator.ID,
		0,
		moderator.Email,
		moderator.Name,
		string(moderator.Role),
		moderator.SessionVersion,
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

// RevokeSession invalidates every session issued for the moderator behind
// sessionToken (D58). Best-effort — see MentorAuthService.RevokeSession.
func (s *AdminAuthService) RevokeSession(ctx context.Context, sessionToken string) error {
	if s.tokenManager == nil || sessionToken == "" {
		return nil
	}
	claims, err := s.tokenManager.ValidateAdminToken(sessionToken)
	if err != nil {
		return nil
	}
	if err := s.moderatorRepo.BumpModeratorSessionVersion(ctx, claims.MentorUUID); err != nil {
		if errors.Is(err, repository.ErrSessionSubjectGone) {
			return nil
		}
		return fmt.Errorf("failed to revoke admin sessions: %w", err)
	}
	return nil
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
