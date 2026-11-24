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
	"github.com/openmentor-io/openmentor/api/pkg/redact"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"go.uber.org/zap"
)

var (
	ErrMenteeNotFound           = errors.New("mentee not found")
	ErrMenteeInvalidLoginToken  = errors.New("invalid or expired mentee login token")
	ErrMenteeTokenGenerationErr = errors.New("failed to generate mentee login token")
)

// MenteeRepository is the mentee repository surface the service needs.
// *repository.MenteeRepository satisfies it; tests substitute a fake.
type MenteeRepository interface {
	SetLoginToken(ctx context.Context, email, token string, exp time.Time) (string, error)
	ConsumeLoginToken(ctx context.Context, token string) (*repository.ConsumedMenteeLogin, error)
	BumpMenteeSessionVersion(ctx context.Context, menteeID string) error
	ListRequests(ctx context.Context, menteeID string) ([]repository.MenteeRequestRow, error)
}

var _ MenteeRepository = (*repository.MenteeRepository)(nil)

// MenteeService is the mentee dashboard (D95): magic-link login and a read-only
// list of the requests sent from the mentee's email.
type MenteeService struct {
	repo         MenteeRepository
	config       *config.Config
	tokenManager *jwt.TokenManager
	httpClient   httpclient.Client
	tracker      analytics.Tracker
}

func NewMenteeService(
	repo MenteeRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *MenteeService {

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
	return &MenteeService{
		repo:         repo,
		config:       cfg,
		tokenManager: tokenManager,
		httpClient:   httpClient,
		tracker:      tracker,
	}
}

func (s *MenteeService) trackLogin(ctx context.Context, menteeID, outcome string) {
	metrics.MenteeAuthLoginRequests.WithLabelValues(outcome).Inc()
	props := map[string]interface{}{"outcome": outcome}
	distinctID := analytics.SystemDistinctID("api")
	if menteeID != "" {
		props["mentee_id"] = menteeID
		distinctID = analytics.MenteeDistinctID(menteeID)
	}
	s.tracker.Track(ctx, analytics.EventMenteeAuthLoginRequested, distinctID, props)
}

// RequestLogin mails a sign-in link to an address that has sent at least one
// request. ErrMenteeNotFound is answered by the handler exactly like success.
func (s *MenteeService) RequestLogin(ctx context.Context, email string) (*models.RequestLoginResponse, error) {
	token, err := generateMenteeLoginToken()
	if err != nil {
		s.trackLogin(ctx, "", "token_generation_failed")
		logger.Error("Failed to generate mentee login token", logger.RedactedError(err))
		return nil, ErrMenteeTokenGenerationErr
	}

	expiration := time.Now().Add(time.Duration(s.config.MentorSession.LoginTokenTTLMinutes) * time.Minute)
	menteeID, err := s.repo.SetLoginToken(ctx, email, token, expiration)
	if errors.Is(err, repository.ErrMenteeHasNoRequests) {
		s.trackLogin(ctx, "", "mentee_not_found")
		logger.Info("Mentee login request for an address with no requests",
			zap.String("email", redact.Email(email)))
		return nil, ErrMenteeNotFound
	}
	if err != nil {
		s.trackLogin(ctx, "", "storage_failed")
		logger.Error("Failed to store mentee login token", logger.RedactedError(err))
		return nil, fmt.Errorf("failed to store mentee login token: %w", err)
	}

	loginURL := fmt.Sprintf("%s/mentee/auth/callback?token=%s", s.config.Server.BaseURL, token)
	if triggerURL := s.config.EventTriggers.MenteeLoginEmailTriggerURL(); triggerURL != "" {
		payload := map[string]interface{}{
			"type":      "mentee_login",
			"mentee_id": menteeID,
			"login_url": loginURL,
		}
		trigger.CallAsyncWithPayload(ctx, triggerURL, payload, s.config.Worker.AuthToken, s.httpClient)
	} else if s.config.IsDevelopment() {
		logger.Info("Development mentee login URL printed to stdout", zap.String("mentee_id", menteeID))
		printDevLoginURL("DEVELOPMENT MENTEE LOGIN URL", loginURL)
	}

	s.trackLogin(ctx, menteeID, "success")
	logger.Info("Mentee login token generated", zap.String("mentee_id", menteeID))
	return &models.RequestLoginResponse{Success: true, Message: models.GenericLoginMessage}, nil
}

func (s *MenteeService) trackVerify(ctx context.Context, menteeID, outcome string) {
	metrics.MenteeAuthVerifyRequests.WithLabelValues(outcome).Inc()
	props := map[string]interface{}{"outcome": outcome}
	distinctID := analytics.SystemDistinctID("api")
	if menteeID != "" {
		props["mentee_id"] = menteeID
		props["session_ttl_hours"] = s.config.MentorSession.SessionTTLHours
		distinctID = analytics.MenteeDistinctID(menteeID)
	}
	s.tracker.Track(ctx, analytics.EventMenteeAuthLoginVerified, distinctID, props)
}

// VerifyLogin spends a sign-in link and mints a session from the row the
// consuming UPDATE returned (H1).
func (s *MenteeService) VerifyLogin(ctx context.Context, token string) (*models.MenteeSession, string, error) {
	if s.tokenManager == nil {
		s.trackVerify(ctx, "", "not_configured")
		return nil, "", ErrJWTSecretNotSet
	}

	mentee, err := s.repo.ConsumeLoginToken(ctx, token)
	if err != nil {
		if errors.Is(err, repository.ErrTokenNotConsumable) {
			s.trackVerify(ctx, "", "invalid_token")
			return nil, "", ErrMenteeInvalidLoginToken
		}
		s.trackVerify(ctx, "", outcomeConsumeFailed)
		logger.Error("Mentee login verification failed to consume the token", logger.RedactedError(err))
		return nil, "", fmt.Errorf("failed to verify mentee login token: %w", err)
	}

	jwtToken, err := s.tokenManager.GenerateMenteeToken(mentee.ID, mentee.SessionVersion)
	if err != nil {
		s.trackVerify(ctx, mentee.ID, "jwt_failed")
		return nil, "", fmt.Errorf("failed to generate mentee session: %w", err)
	}

	now := time.Now()
	session := &models.MenteeSession{
		MenteeID:  mentee.ID,
		ExpiresAt: now.Add(s.tokenManager.GetExpirationTime()).Unix(),
		IssuedAt:  now.Unix(),
	}
	s.trackVerify(ctx, mentee.ID, "success")
	logger.Info("Mentee login successful", zap.String("mentee_id", mentee.ID))
	return session, jwtToken, nil
}

// RevokeSession invalidates every session issued for the mentee behind
// sessionToken (D58). Best-effort, like MentorAuthService.RevokeSession.
func (s *MenteeService) RevokeSession(ctx context.Context, sessionToken string) error {
	if s.tokenManager == nil || sessionToken == "" {
		return nil
	}
	claims, err := s.tokenManager.ValidateMenteeToken(sessionToken)
	if err != nil {
		return nil
	}
	if err := s.repo.BumpMenteeSessionVersion(ctx, claims.MentorUUID); err != nil {
		if errors.Is(err, repository.ErrSessionSubjectGone) {
			return nil
		}
		return fmt.Errorf("failed to revoke mentee sessions: %w", err)
	}
	return nil
}

// ListRequests returns the mentee's requests as the dashboard shows them. What
// the mentee may see of the mentor is decided here, not in the UI: the calendar
// link only while a request is pending or active, never a contact detail.
func (s *MenteeService) ListRequests(ctx context.Context, menteeID string) (*models.MenteeRequestsResponse, error) {
	rows, err := s.repo.ListRequests(ctx, menteeID)
	if err != nil {
		logger.Error("Failed to list mentee requests", zap.String("mentee_id", menteeID), logger.RedactedError(err))
		return nil, err
	}

	counts := map[models.MenteeRequestGroup]int{}
	out := make([]models.MenteeRequest, 0, len(rows))
	for i := range rows {
		req := menteeRequestView(&rows[i])
		counts[req.Group]++
		out = append(out, req)
	}

	s.tracker.Track(ctx, analytics.EventMenteeRequestsViewed, analytics.MenteeDistinctID(menteeID), map[string]interface{}{
		"mentee_id":     menteeID,
		"total":         len(out),
		"pending_count": counts[models.MenteeGroupPending],
		"active_count":  counts[models.MenteeGroupActive],
		"done_count":    counts[models.MenteeGroupDone],
		"closed_count":  counts[models.MenteeGroupClosed],
		"reviewable":    countReviewable(out),
		"outcome":       "success",
	})
	return &models.MenteeRequestsResponse{Requests: out}, nil
}

func menteeRequestView(row *repository.MenteeRequestRow) models.MenteeRequest {
	group := models.MenteeGroupFor(row.Status)
	req := models.MenteeRequest{
		ID:              row.ID,
		Status:          row.Status,
		Group:           group,
		Details:         row.Details,
		CreatedAt:       row.CreatedAt,
		StatusChangedAt: row.StatusChangedAt,
		Mentor:          models.MenteeRequestMentor{Available: row.MentorAvailable},
	}
	if row.Status == models.StatusDeclined {
		req.DeclineReason = row.DeclineReason
		req.DeclineComment = row.DeclineComment
	}

	if row.MentorAvailable {
		req.Mentor.Name = row.MentorName
		if row.MentorStatus == "active" {
			req.Mentor.Slug = row.MentorSlug
		}
		if group == models.MenteeGroupPending || group == models.MenteeGroupActive {
			req.Mentor.CalendarURL = row.MentorCalendarURL
		}
	}

	if row.ReviewCreatedAt != nil {
		req.Review = &models.MenteeReview{
			MentorReview:   row.MentorReview,
			PlatformReview: row.PlatformReview,
			Improvements:   row.Improvements,
			CreatedAt:      *row.ReviewCreatedAt,
		}
	}
	// Mirrors the eligibility the review write re-checks in SQL: done, no review
	// yet, and a mentor who has not deleted their profile.
	req.CanReview = group == models.MenteeGroupDone && req.Review == nil && row.MentorAvailable
	return req
}

func countReviewable(reqs []models.MenteeRequest) int {
	n := 0
	for i := range reqs {
		if reqs[i].CanReview {
			n++
		}
	}
	return n
}

func (s *MenteeService) GetSessionTTL() int                 { return s.config.MentorSession.SessionTTLHours * 3600 }
func (s *MenteeService) GetCookieDomain() string            { return s.config.MentorSession.CookieDomain }
func (s *MenteeService) GetCookieSecure() bool              { return s.config.MentorSession.CookieSecure }
func (s *MenteeService) GetTokenManager() *jwt.TokenManager { return s.tokenManager }

// generateMenteeLoginToken mints a 256-bit token. The etk_ prefix is distinct
// from mtk_ (mentor) and atk_ (admin) so a link pasted into the wrong callback is
// recognizable in a support conversation; it carries no authority by itself.
func generateMenteeLoginToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("etk_%s_%d", hex.EncodeToString(b), time.Now().Unix()), nil
}
