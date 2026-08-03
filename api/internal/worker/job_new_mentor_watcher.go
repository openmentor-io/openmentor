package worker

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/slug"
)

// confirmationTokenTTL is the validity window of the email confirmation
// link (must match internal/services/mentor_confirmation_service.go).
const confirmationTokenTTL = 24 * time.Hour

// generateConfirmationToken creates a secure random single-use email
// confirmation token (same format as the API's resend flow).
func generateConfirmationToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := cryptorand.Read(bytes); err != nil {
		return "", err
	}
	return "mcf_" + hex.EncodeToString(bytes), nil
}

// NewMentorWatcher ports openmentor-func/new-mentor-watcher/index.ts:
// finalize a fresh mentor registration (adapted for the draft-status
// workflow).
//
// Division of labor with the API (documented per stage-2 spec):
//   - The API's registration flow (internal/services/registration_service.go)
//     already trims the name and contact details, sets status=draft and
//     ALWAYS generates the slug at INSERT time (repository.CreateMentor via
//     pkg/slug). The func app generated a slug only when the record had none.
//   - This handler therefore keeps the existing slug and only generates one
//     (same pkg/slug algorithm, same {name}-{legacy_id} format as the func's
//     getAlias) as a defensive fallback for records that somehow miss it.
//   - What only THIS handler does: duplicate-email check (auto-decline),
//     login_token generation (+100 day expiry), email confirmation token
//     generation (24h), sort_order randomization and the emails. The mentor
//     stays 'draft' until they confirm their email address
//     (POST /api/v1/mentors/confirm -> the mentor-confirmed job); only
//     duplicates get the final 'declined' status here.
func (h *Handlers) NewMentorWatcher(c *gin.Context) {
	ctx := c.Request.Context()

	mentorID := c.Query("mentorId")
	if mentorID == "" {
		h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "missing_mentor_id",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentorId is required"})
		return
	}

	res, err := h.finalizeNewMentor(ctx, mentorID)
	if err != nil {
		if errors.Is(err, errFinalizeMentorNotFound) {
			h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
				"mentor_id": mentorID,
				"outcome":   "mentor_not_found",
			})
			c.JSON(http.StatusNotFound, gin.H{"error": "mentor not found"})
			return
		}
		h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":  mentorID,
			"outcome":    "error",
			"error_type": res.ErrorType,
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": res.Message})
		return
	}

	h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
		"mentor_id":        mentorID,
		"status":           res.Status,
		"duplicates_count": res.Duplicates,
		"outcome":          "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "mentorId": mentorID, "status": res.Status})
}

// errFinalizeMentorNotFound reports that the mentor row is gone (the trigger
// raced a deletion), which is a 404 rather than a retryable failure.
var errFinalizeMentorNotFound = errors.New("mentor not found")

// finalizeResult describes one finalization run for the caller's reporting:
// the HTTP handler turns it into a response + analytics event, the
// finalize-stuck-registrations cron job into a JobSummary.
type finalizeResult struct {
	Status     string // draft | declined
	Duplicates int
	// ErrorType is the analytics error_type and Message the client-facing
	// reason; both are set only when finalizeNewMentor returns an error.
	ErrorType string
	Message   string
}

// finalizeNewMentor is the idempotent core of the new-mentor-watcher job:
// duplicate-email check, slug fallback, the single finalization UPDATE
// (name/contact, status, sort_order, confirmation token) and the resulting
// email. Re-running it is safe — the UPDATE is absolute, not incremental, and a
// fresh confirmation token simply supersedes the previous one — which is what
// lets finalize-stuck-registrations replay a trigger that never arrived.
func (h *Handlers) finalizeNewMentor(ctx context.Context, mentorID string) (finalizeResult, error) {
	const job = "new-mentor-watcher"
	res := finalizeResult{Status: mentorStatusDraft}

	mentor, err := h.repo.GetJobMentorByID(ctx, mentorID)
	if err != nil {
		logger.Error("[New Mentor] Failed to fetch mentor", zap.String("mentor_id", mentorID), zap.Error(err))
		res.ErrorType, res.Message = "db_error", "failed to fetch mentor"
		return res, err
	}
	if mentor == nil {
		logger.Warn("[New Mentor] Mentor not found", zap.String("mentor_id", mentorID))
		return res, errFinalizeMentorNotFound
	}

	duplicates, err := h.repo.CountActiveMentorsByEmail(ctx, mentor.Email)
	if err != nil {
		logger.Error("[New Mentor] Failed to check duplicates", zap.String("mentor_id", mentorID), zap.Error(err))
		res.ErrorType, res.Message = "db_error", "failed to check duplicates"
		return res, err
	}
	res.Duplicates = duplicates
	if duplicates > 0 {
		logger.Info("[New Mentor] Duplicate mentor found", zap.String("mentor_id", mentorID))
		res.Status = mentorStatusDeclined
	}

	mentor.PreferredContact = strings.TrimSpace(mentor.PreferredContact)
	mentor.Name = trimMentorName(mentor.Name)

	// The slug is normally already set at registration; generate only as a
	// fallback (mirrors the func's "if alias is empty" branch).
	if mentor.Slug == "" {
		mentor.Slug = slug.GenerateMentorSlug(mentor.Name, mentor.LegacyID)
	}

	// A fresh (non-duplicate) registration gets a single-use email
	// confirmation token: the mentor stays 'draft' until they click the
	// link. Duplicates are declined and get no token.
	var confirmToken *string
	var confirmExpiresAt *time.Time
	if res.Status == mentorStatusDraft {
		token, tokenErr := generateConfirmationToken()
		if tokenErr != nil {
			logger.Error("[New Mentor] Failed to generate confirmation token", zap.String("mentor_id", mentorID), zap.Error(tokenErr))
			res.ErrorType, res.Message = "token_generation_failed", "failed to generate confirmation token"
			return res, tokenErr
		}
		expiresAt := time.Now().Add(confirmationTokenTTL)
		confirmToken = &token
		confirmExpiresAt = &expiresAt
	}

	// The email goes out BEFORE the row is written, and the order is the whole
	// retry story: the finalization UPDATE is what takes a registration out of
	// finalize-stuck-registrations' replay set, so committing it first and then
	// failing to send left a mentor holding a token they were never told about,
	// which no job would ever look at again. Failing here instead leaves the row
	// untouched, and the next cron pass replays it with a fresh token.
	//
	// A fresh registration only gets the email confirmation request; the
	// "application received" mentor email and the moderator notification
	// move to the mentor-confirmed job (after the mentor clicks the link).
	var sendErr error
	if res.Status == mentorStatusDraft {
		sendErr = h.sendEmail(ctx, job, email.Message{
			TemplateName: "mentor-confirm-email",
			Recipient:    mentor.Email,
			Props: map[string]interface{}{
				"first_name":  mentor.Name,
				"confirm_url": h.baseURL + "/mentor/confirm?token=" + *confirmToken,
			},
		})
	} else {
		sendErr = h.sendEmail(ctx, job, email.Message{
			TemplateName: "new-mentor-duplicate",
			Recipient:    mentor.Email,
			Props:        map[string]interface{}{"first_name": mentor.Name},
		})
	}
	if sendErr != nil {
		res.ErrorType, res.Message = "email_send_failed", "failed to send emails"
		return res, sendErr
	}

	if err := h.repo.FinalizeNewMentor(ctx, FinalizeNewMentorParams{
		MentorID:                   mentor.ID,
		Name:                       mentor.Name,
		PreferredContact:           mentor.PreferredContact,
		Slug:                       mentor.Slug,
		Status:                     res.Status,
		SortOrder:                  rand.IntN(1000), //nolint:gosec // G404: cosmetic catalog shuffle, not a security decision
		EmailConfirmationToken:     confirmToken,
		EmailConfirmationExpiresAt: confirmExpiresAt,
	}); err != nil {
		logger.Error("[New Mentor] Failed to update mentor", zap.String("mentor_id", mentorID), zap.Error(err))
		res.ErrorType, res.Message = "db_error", "failed to update mentor"
		return res, err
	}

	return res, nil
}
