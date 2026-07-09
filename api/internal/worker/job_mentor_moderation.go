package worker

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/openmentor-io/openmentor-api/pkg/analytics"
	"github.com/openmentor-io/openmentor-api/pkg/email"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
)

// MentorModerationAction ports openmentor-func/mentor-moderation-action/index.ts:
// notify a mentor about the outcome of moderation (approve/decline).
//
// Division of labor with the API (documented per stage-2 spec):
//   - The API's admin service (internal/services/admin_mentors_service.go)
//     sets the mentor status itself (approve -> active, decline -> declined)
//     BEFORE firing MENTOR_MODERATION_TRIGGER_URL. The func app's handler
//     also contained an updateStatus() helper, but it was dead code - the
//     shipped handler never wrote status, it only sent the email.
//   - The worker therefore does NOT normally write status. To be idempotent
//     against races/replays it verifies the mentor's status matches the
//     action's expected outcome and only writes it (with a warning log)
//     when it does not.
func (h *Handlers) MentorModerationAction(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "mentor-moderation-action"

	var payload models.AdminModerationTriggerPayload
	bindErr := c.ShouldBindJSON(&payload)

	trackOutcome := func(outcome string, includeRole bool) {
		props := map[string]interface{}{"outcome": outcome}
		if payload.MentorID != "" {
			props["target_mentor_id"] = payload.MentorID
		}
		if payload.Action != "" {
			props["action"] = payload.Action
		}
		if includeRole && payload.Role != "" {
			props["moderator_role"] = payload.Role
		}
		h.track(ctx, analytics.EventAdminMentorModerationAction, analytics.ModeratorDistinctID(payload.ModeratorID), props)
	}

	if bindErr != nil || payload.Type != "mentor_moderation" {
		trackOutcome("invalid_payload_type", false)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: type must be 'mentor_moderation'"})
		return
	}
	if payload.MentorID == "" {
		trackOutcome("missing_mentor_id", false)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: missing mentor_id"})
		return
	}
	if payload.ModeratorID == "" {
		trackOutcome("missing_moderator_id", false)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: missing moderator_id"})
		return
	}
	if payload.Action != "approve" && payload.Action != "decline" {
		trackOutcome("invalid_action", false)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: action must be approve or decline"})
		return
	}

	moderator, err := h.repo.GetJobModeratorByID(ctx, payload.ModeratorID)
	if err != nil {
		logger.Error("[Mentor Moderation Action] Failed to fetch moderator",
			zap.String("moderator_id", payload.ModeratorID), zap.Error(err))
		trackOutcome("error", false)
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Failed to fetch moderator"})
		return
	}
	if moderator == nil {
		logger.Warn("[Mentor Moderation Action] Moderator not found", zap.String("moderator_id", payload.ModeratorID))
		trackOutcome("moderator_not_found", false)
		c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "Moderator not found"})
		return
	}

	mentor, err := h.repo.GetJobMentorByID(ctx, payload.MentorID)
	if err != nil {
		logger.Error("[Mentor Moderation Action] Failed to fetch mentor",
			zap.String("mentor_id", payload.MentorID), zap.Error(err))
		trackOutcome("error", true)
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[Mentor Moderation Action] Mentor not found", zap.String("mentor_id", payload.MentorID))
		trackOutcome("mentor_not_found", true)
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Mentor not found"})
		return
	}

	// Idempotency check: the API already wrote the status before firing the
	// trigger. Repair it (and warn) only if it doesn't match the action.
	expectedStatus := "active"
	if payload.Action == "decline" {
		expectedStatus = "declined"
	}
	if mentor.Status != expectedStatus {
		logger.Warn("[Mentor Moderation Action] Mentor status does not match moderation action; updating",
			zap.String("mentor_id", mentor.ID),
			zap.String("action", payload.Action),
			zap.String("current_status", mentor.Status),
			zap.String("expected_status", expectedStatus),
		)
		if err := h.repo.SetMentorStatus(ctx, mentor.ID, expectedStatus); err != nil {
			logger.Error("[Mentor Moderation Action] Failed to update mentor status",
				zap.String("mentor_id", mentor.ID), zap.Error(err))
			trackOutcome("error", true)
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Failed to update mentor status"})
			return
		}
		mentor.Status = expectedStatus
	}

	// Moderation results are visible in the admin portal; the mentor is
	// notified by email. The approved email links to the mentor's public
	// profile (with login-link guidance baked into the template copy).
	var message email.Message
	if payload.Action == "approve" {
		message = email.Message{
			TemplateName: "new-mentor-approved",
			Recipient:    mentor.Email,
			Props: map[string]interface{}{
				"first_name":         mentor.Name,
				"mentor_profile_url": h.mentorProfileURL(mentor.Slug),
			},
		}
	} else {
		message = email.Message{
			TemplateName: "new-mentor-declined",
			Recipient:    mentor.Email,
			Props:        map[string]interface{}{"first_name": mentor.Name},
		}
	}
	if sendErr := h.sendEmail(ctx, job, message); sendErr != nil {
		trackOutcome("error", true)
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Failed to send email"})
		return
	}

	logger.Info("[Mentor Moderation Action] Completed",
		zap.String("mentor_id", payload.MentorID),
		zap.String("action", payload.Action),
		zap.String("moderator_id", payload.ModeratorID),
		zap.String("role", payload.Role),
	)
	trackOutcome("success", true)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
