package worker

import (
	cryptorand "crypto/rand"
	"encoding/hex"
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
	const job = "new-mentor-watcher"

	mentorID := c.Query("mentorId")
	if mentorID == "" {
		h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "missing_mentor_id",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentorId is required"})
		return
	}

	mentor, err := h.repo.GetJobMentorByID(ctx, mentorID)
	if err != nil {
		logger.Error("[New Mentor] Failed to fetch mentor", zap.String("mentor_id", mentorID), zap.Error(err))
		h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":  mentorID,
			"outcome":    "error",
			"error_type": "db_error",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[New Mentor] Mentor not found", zap.String("mentor_id", mentorID))
		h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id": mentorID,
			"outcome":   "mentor_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentor not found"})
		return
	}

	newStatus := "draft"

	duplicates, err := h.repo.CountActiveMentorsByEmail(ctx, mentor.Email)
	if err != nil {
		logger.Error("[New Mentor] Failed to check duplicates", zap.String("mentor_id", mentorID), zap.Error(err))
		h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":  mentorID,
			"outcome":    "error",
			"error_type": "db_error",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to check duplicates"})
		return
	}
	if duplicates > 0 {
		logger.Info("[New Mentor] Duplicate mentor found", zap.String("mentor_id", mentorID))
		newStatus = "declined"
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
	if newStatus == "draft" {
		token, tokenErr := generateConfirmationToken()
		if tokenErr != nil {
			logger.Error("[New Mentor] Failed to generate confirmation token", zap.String("mentor_id", mentorID), zap.Error(tokenErr))
			h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
				"mentor_id":  mentorID,
				"outcome":    "error",
				"error_type": "token_generation_failed",
			})
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to generate confirmation token"})
			return
		}
		expiresAt := time.Now().Add(confirmationTokenTTL)
		confirmToken = &token
		confirmExpiresAt = &expiresAt
	}

	err = h.repo.FinalizeNewMentor(ctx, FinalizeNewMentorParams{
		MentorID:                   mentor.ID,
		Name:                       mentor.Name,
		PreferredContact:           mentor.PreferredContact,
		Slug:                       mentor.Slug,
		Status:                     newStatus,
		SortOrder:                  rand.IntN(1000), // Math.floor(Math.random() * 1000)
		EmailConfirmationToken:     confirmToken,
		EmailConfirmationExpiresAt: confirmExpiresAt,
	})
	if err != nil {
		logger.Error("[New Mentor] Failed to update mentor", zap.String("mentor_id", mentorID), zap.Error(err))
		h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":  mentorID,
			"outcome":    "error",
			"error_type": "db_error",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to update mentor"})
		return
	}

	// A fresh registration only gets the email confirmation request; the
	// "application received" mentor email and the moderator notification
	// move to the mentor-confirmed job (after the mentor clicks the link).
	var sendErr error
	if newStatus == "draft" {
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
		h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
			"mentor_id":  mentor.ID,
			"outcome":    "error",
			"error_type": "email_send_failed",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to send emails"})
		return
	}

	h.track(ctx, analytics.EventNewMentorWatcherProcessed, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
		"mentor_id":        mentor.ID,
		"status":           newStatus,
		"duplicates_count": duplicates,
		"outcome":          "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "mentorId": mentor.ID, "status": newStatus})
}
