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
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/trigger"
	"go.uber.org/zap"
)

var (
	// ErrConfirmationTokenInvalid: the token matches no mentor (dead link).
	ErrConfirmationTokenInvalid = errors.New("invalid confirmation token")
	// ErrConfirmationTokenExpired: the token exists but its 24h window has
	// passed — the client can offer a resend.
	ErrConfirmationTokenExpired = errors.New("confirmation token expired")
)

// confirmationTokenTTL is the validity window of an email confirmation
// link (must match the worker's new-mentor-watcher job).
const confirmationTokenTTL = 24 * time.Hour

// MentorConfirmationRepository is the repository surface of the email
// confirmation flow. *repository.MentorRepository satisfies it.
type MentorConfirmationRepository interface {
	GetByConfirmationToken(ctx context.Context, token string) (*models.MentorConfirmation, error)
	ConfirmMentorEmail(ctx context.Context, mentorID string) error
	SetEmailConfirmation(ctx context.Context, mentorID, token string, expiresAt time.Time) error
}

var _ MentorConfirmationRepository = (*repository.MentorRepository)(nil)

// MentorConfirmationService handles the public email-confirmation
// endpoints of the draft-status registration workflow.
type MentorConfirmationService struct {
	mentorRepo MentorConfirmationRepository
	config     *config.Config
	httpClient httpclient.Client
	tracker    analytics.Tracker
}

// NewMentorConfirmationService wires the confirmation service.
func NewMentorConfirmationService(
	mentorRepo MentorConfirmationRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
) *MentorConfirmationService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	return &MentorConfirmationService{
		mentorRepo: mentorRepo,
		config:     cfg,
		httpClient: httpClient,
		tracker:    tracker,
	}
}

// ConfirmEmail validates a confirmation token and moves the mentor from
// draft to pending (single-use: the token is cleared). Returns
// already=true when the mentor is already past draft (idempotent).
func (s *MentorConfirmationService) ConfirmEmail(ctx context.Context, token string) (already bool, err error) {
	mentor, err := s.mentorRepo.GetByConfirmationToken(ctx, token)
	if err != nil {
		s.track(ctx, "", "", "error")
		logger.Error("Failed to look up confirmation token", zap.Error(err))
		return false, fmt.Errorf("failed to confirm email")
	}
	if mentor == nil {
		s.track(ctx, "", "", "invalid_token")
		return false, ErrConfirmationTokenInvalid
	}

	if mentor.Status != mentorStatusDraft {
		// The profile already moved past draft (e.g. double-click race or
		// a moderator action) — report success idempotently.
		s.track(ctx, mentor.MentorID, mentor.Status, "already")
		return true, nil
	}

	if time.Now().After(mentor.ExpiresAt) {
		s.track(ctx, mentor.MentorID, mentor.Status, "expired")
		return false, ErrConfirmationTokenExpired
	}

	if err := s.mentorRepo.ConfirmMentorEmail(ctx, mentor.MentorID); err != nil {
		s.track(ctx, mentor.MentorID, mentor.Status, "update_failed")
		logger.Error("Failed to confirm mentor email",
			zap.Error(err),
			zap.String("mentor_id", mentor.MentorID))
		return false, fmt.Errorf("failed to confirm email")
	}

	// The mentor-confirmed worker job sends the "application in review"
	// email to the mentor and the new-mentor notification to moderators.
	trigger.CallAsync(ctx, s.config.EventTriggers.MentorConfirmedTriggerURL(), mentor.MentorID, s.config.Worker.AuthToken, s.httpClient)

	s.track(ctx, mentor.MentorID, mentorStatusPending, "success")
	logger.Info("Mentor email confirmed",
		zap.String("mentor_id", mentor.MentorID))
	return false, nil
}

// ResendConfirmation issues a fresh confirmation token for an expired (or
// still valid) token and re-sends the confirmation email. Only applies
// while the mentor is still in 'draft'; returns already=true otherwise.
func (s *MentorConfirmationService) ResendConfirmation(ctx context.Context, token string) (already bool, err error) {
	mentor, err := s.mentorRepo.GetByConfirmationToken(ctx, token)
	if err != nil {
		s.track(ctx, "", "", "resend_error")
		logger.Error("Failed to look up confirmation token for resend", zap.Error(err))
		return false, fmt.Errorf("failed to resend confirmation email")
	}
	if mentor == nil {
		s.track(ctx, "", "", "resend_invalid_token")
		return false, ErrConfirmationTokenInvalid
	}

	if mentor.Status != mentorStatusDraft {
		s.track(ctx, mentor.MentorID, mentor.Status, "resend_already")
		return true, nil
	}

	freshToken, err := generateConfirmationToken()
	if err != nil {
		s.track(ctx, mentor.MentorID, mentor.Status, "resend_token_generation_failed")
		logger.Error("Failed to generate confirmation token", zap.Error(err))
		return false, fmt.Errorf("failed to resend confirmation email")
	}

	if err := s.mentorRepo.SetEmailConfirmation(ctx, mentor.MentorID, freshToken, time.Now().Add(confirmationTokenTTL)); err != nil {
		s.track(ctx, mentor.MentorID, mentor.Status, "resend_update_failed")
		logger.Error("Failed to store fresh confirmation token",
			zap.Error(err),
			zap.String("mentor_id", mentor.MentorID))
		return false, fmt.Errorf("failed to resend confirmation email")
	}

	// The mentor-confirm-email worker job reads the fresh token from the
	// row and re-sends the mentor-confirm-email message.
	trigger.CallAsync(ctx, s.config.EventTriggers.MentorConfirmEmailTriggerURL(), mentor.MentorID, s.config.Worker.AuthToken, s.httpClient)

	s.track(ctx, mentor.MentorID, mentor.Status, "resent")
	logger.Info("Mentor confirmation email resent",
		zap.String("mentor_id", mentor.MentorID))
	return false, nil
}

func (s *MentorConfirmationService) track(ctx context.Context, mentorID, status, outcome string) {
	properties := map[string]interface{}{"outcome": outcome}
	distinctID := analytics.SystemDistinctID("api")
	if mentorID != "" {
		properties["mentor_id"] = mentorID
		distinctID = analytics.MentorDistinctID(mentorID)
	}
	if status != "" {
		properties["mentor_status"] = status
	}
	s.tracker.Track(ctx, analytics.EventMentorEmailConfirmed, distinctID, properties)
}

// generateConfirmationToken creates a secure random email confirmation
// token (mirrors the format the new-mentor-watcher worker job generates).
func generateConfirmationToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "mcf_" + hex.EncodeToString(bytes), nil
}
