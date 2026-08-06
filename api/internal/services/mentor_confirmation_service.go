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
	// ErrConfirmationResendLimited: this mentor has used up their resend
	// budget (see resendLimiter).
	ErrConfirmationResendLimited = errors.New("confirmation resend rate limit exceeded")
)

// confirmationTokenTTL is the validity window of an email confirmation
// link (must match the worker's new-mentor-watcher job).
const confirmationTokenTTL = 24 * time.Hour

// MentorConfirmationRepository is the repository surface of the email
// confirmation flow. *repository.MentorRepository satisfies it.
type MentorConfirmationRepository interface {
	GetByConfirmationToken(ctx context.Context, token string) (*models.MentorConfirmation, error)
	ConsumeConfirmationToken(ctx context.Context, token string) (applied bool, err error)
	RotateConfirmationToken(ctx context.Context, oldToken, newToken string, expiresAt time.Time) (applied bool, err error)
}

var _ MentorConfirmationRepository = (*repository.MentorRepository)(nil)

// ResendLimiter is the rate-limit surface the resend flow needs.
// *middleware.RateLimiter satisfies it.
//
// The limit CANNOT live in middleware here: a resend rotates the token, so the
// only stable identity of the sender is the mentor id, and that is known only
// after the token has been looked up. Keying middleware on the submitted token
// handed every resend a brand-new bucket.
type ResendLimiter interface {
	Allow(key string) bool
}

// unlimitedResends is the fallback when no limiter is wired (tests, tooling);
// main.go always passes the real one.
type unlimitedResends struct{}

func (unlimitedResends) Allow(string) bool { return true }

// MentorConfirmationService handles the public email-confirmation
// endpoints of the draft-status registration workflow.
type MentorConfirmationService struct {
	mentorRepo    MentorConfirmationRepository
	config        *config.Config
	httpClient    httpclient.Client
	tracker       analytics.Tracker
	resendLimiter ResendLimiter
}

// NewMentorConfirmationService wires the confirmation service.
func NewMentorConfirmationService(
	mentorRepo MentorConfirmationRepository,
	cfg *config.Config,
	httpClient httpclient.Client,
	tracker analytics.Tracker,
	resendLimiter ResendLimiter,
) *MentorConfirmationService {

	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}
	if resendLimiter == nil {
		resendLimiter = unlimitedResends{}
	}

	return &MentorConfirmationService{
		mentorRepo:    mentorRepo,
		config:        cfg,
		httpClient:    httpClient,
		tracker:       tracker,
		resendLimiter: resendLimiter,
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

	// SECURITY (H1): the transition and the clearing of the single-use token are
	// ONE conditional UPDATE keyed on the token that was just read, so two
	// concurrent clicks produce exactly one draft -> pending transition and exactly
	// one notification. The lookup above still classifies the three outcomes the
	// web client branches on; it no longer authorizes the write.
	applied, err := s.mentorRepo.ConsumeConfirmationToken(ctx, token)
	if err != nil {
		s.track(ctx, mentor.MentorID, mentor.Status, "update_failed")
		logger.Error("Failed to confirm mentor email",
			logger.RedactedError(err),
			zap.String("mentor_id", mentor.MentorID))
		return false, fmt.Errorf("failed to confirm email")
	}
	if !applied {
		// Somebody else consumed the token between the read and this write (a
		// double-clicked link, or a moderator action). Same answer as finding the
		// mentor already past draft: idempotent success, and NO second trigger.
		s.track(ctx, mentor.MentorID, mentor.Status, "already")
		return true, nil
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

	// Keyed on the mentor id, and consumed only now that we know a resend will
	// actually be attempted: a malformed payload or a dead token costs nobody
	// budget, and no amount of token rotation buys a fresh bucket.
	if !s.resendLimiter.Allow(resendLimitKey(mentor.MentorID)) {
		s.track(ctx, mentor.MentorID, mentor.Status, "resend_rate_limited")
		logger.Warn("Mentor confirmation resend rate limited",
			zap.String("mentor_id", mentor.MentorID))
		return false, ErrConfirmationResendLimited
	}

	freshToken, err := generateConfirmationToken()
	if err != nil {
		s.track(ctx, mentor.MentorID, mentor.Status, "resend_token_generation_failed")
		logger.Error("Failed to generate confirmation token", zap.Error(err))
		return false, fmt.Errorf("failed to resend confirmation email")
	}

	// The rotation is a compare-and-swap on the token that was read, so the fresh
	// token replaces the old one in a single write: the previous link stops working
	// at the same instant the new one starts. applied=false means the row moved
	// under us, and then the fresh token was NEVER STORED — emailing it would hand
	// the mentor a link that is dead on arrival, which is the whole reason the send
	// is gated on the claim (same rule as the worker's FinalizeNewMentor).
	applied, err := s.mentorRepo.RotateConfirmationToken(ctx, token, freshToken, time.Now().Add(confirmationTokenTTL))
	if err != nil {
		s.track(ctx, mentor.MentorID, mentor.Status, "resend_update_failed")
		logger.Error("Failed to store fresh confirmation token",
			logger.RedactedError(err),
			zap.String("mentor_id", mentor.MentorID))
		return false, fmt.Errorf("failed to resend confirmation email")
	}
	if !applied {
		s.track(ctx, mentor.MentorID, mentor.Status, "resend_superseded")
		logger.Info("Confirmation resend lost the race for the row; nothing was emailed",
			zap.String("mentor_id", mentor.MentorID))
		return false, ErrConfirmationTokenInvalid
	}

	// The worker cannot read the token back any more — it is hashed at rest (D57) —
	// so the confirm URL travels in the trigger payload, exactly like the magic-link
	// login URL already does. The mentor id stays in the URL: these trigger URLs end
	// in "?mentorId=" by convention (see derivedMentorTriggerURL), and keeping that
	// means the job's own resolution order does not change, only the link source.
	if triggerURL := s.config.EventTriggers.MentorConfirmEmailTriggerURL(); triggerURL != "" {
		trigger.CallAsyncWithPayload(ctx, triggerURL+mentor.MentorID, map[string]interface{}{
			"type":        "mentor_confirm_email",
			"mentor_id":   mentor.MentorID,
			"confirm_url": s.confirmURL(freshToken),
		}, s.config.Worker.AuthToken, s.httpClient)
	}

	s.track(ctx, mentor.MentorID, mentor.Status, "resent")
	logger.Info("Mentor confirmation email resent",
		zap.String("mentor_id", mentor.MentorID))
	return false, nil
}

// confirmURL builds the link that goes into the confirmation email. It must match
// the web route the frontend serves (web/src/pages/mentor/confirm.tsx) and the
// URL the worker's new-mentor-watcher builds for the first send.
func (s *MentorConfirmationService) confirmURL(token string) string {
	return s.config.Server.BaseURL + "/mentor/confirm?token=" + token
}

// resendLimitKey namespaces the bucket so a mentor id can never collide with
// another limiter's key space (the RateLimiter's key map is shared per instance).
func resendLimitKey(mentorID string) string { return "mentor_resend:" + mentorID }

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
