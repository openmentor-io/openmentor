package worker

import (
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor-api/pkg/analytics"
	"github.com/openmentor-io/openmentor-api/pkg/email"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"github.com/openmentor-io/openmentor-api/pkg/slug"
)

// loginTokenTTLDays mirrors getExpiryDate() in new-mentor-watcher/index.ts
// (now + 100 days). The func stored the value date-truncated
// (Date.toDateString()); the worker stores the full timestamp, which only
// makes the expiry more precise.
const loginTokenTTLDays = 100

// NewMentorWatcher ports openmentor-func/new-mentor-watcher/index.ts:
// finalize a fresh mentor registration.
//
// Division of labor with the API (documented per stage-2 spec):
//   - The API's registration flow (internal/services/registration_service.go)
//     already trims the name, normalizes telegram, sets status=pending and
//     ALWAYS generates the slug at INSERT time (repository.CreateMentor via
//     pkg/slug). The func app generated a slug only when the record had none.
//   - This handler therefore keeps the existing slug and only generates one
//     (same pkg/slug algorithm, same {name}-{legacy_id} format as the func's
//     getAlias) as a defensive fallback for records that somehow miss it.
//   - What only THIS handler does: duplicate-email check, login_token
//     generation (+100 day expiry), sort_order randomization, the final
//     status write (pending, or declined for duplicates) and the emails.
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

	newStatus := "pending"

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

	mentor.Telegram = trimTelegramHandle(mentor.Telegram)
	mentor.Name = trimMentorName(mentor.Name)

	// The slug is normally already set at registration; generate only as a
	// fallback (mirrors the func's "if alias is empty" branch).
	if mentor.Slug == "" {
		mentor.Slug = slug.GenerateMentorSlug(mentor.Name, mentor.LegacyID)
	}

	err = h.repo.FinalizeNewMentor(ctx, FinalizeNewMentorParams{
		MentorID:            mentor.ID,
		Name:                mentor.Name,
		Telegram:            mentor.Telegram,
		LoginToken:          uuid.NewString(),
		LoginTokenExpiresAt: time.Now().AddDate(0, 0, loginTokenTTLDays),
		Slug:                mentor.Slug,
		Status:              newStatus,
		SortOrder:           rand.IntN(1000), // Math.floor(Math.random() * 1000)
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

	var sendErr error
	if newStatus == "pending" {
		sendErr = h.sendEmails(ctx, job,
			email.Message{
				TemplateName: "new-mentor-moderator",
				Recipient:    h.moderatorsEmail,
				Props: map[string]interface{}{
					"mentor_name":  mentor.Name,
					"mentor_email": mentor.Email,
					"mentor_job":   valueOrDash(mentor.JobTitle) + " @ " + valueOrDash(mentor.Workplace),
				},
			},
			email.Message{
				TemplateName: "new-mentor",
				Recipient:    mentor.Email,
				Props:        map[string]interface{}{"first_name": mentor.Name},
			},
		)
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
