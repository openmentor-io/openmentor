package worker

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// MentorModerationAction ports openmentor-func/mentor-moderation-action/index.ts:
// notify a mentor about the outcome of moderation (approve/decline).
//
// Division of labor with the API (documented per stage-2 spec):
//   - The API's admin service (internal/services/admin_mentors_service.go)
//     sets the mentor status itself (approve -> active, decline -> declined)
//     BEFORE firing MENTOR_MODERATION_TRIGGER_URL, and returns WITHOUT firing
//     when that write fails. The func app's handler also contained an
//     updateStatus() helper, but it was dead code - the shipped handler never
//     wrote status, it only sent the email.
//   - The worker therefore never writes status at all (JobsRepository carries no
//     method for it). The mentor's CURRENT status is the replay guard instead:
//     see the check around expectedStatus below.
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

	if outcome, msg, ok := validateModerationPayload(bindErr, payload); !ok {
		trackOutcome(outcome, false)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": msg})
		return
	}

	moderator, err := h.repo.GetJobModeratorByID(ctx, payload.ModeratorID)
	if err != nil {
		logger.Error("[Mentor Moderation Action] Failed to fetch moderator",
			zap.String("moderator_id", payload.ModeratorID), logger.RedactedError(err))
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
			zap.String("mentor_id", payload.MentorID), logger.RedactedError(err))
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

	// REPLAY GUARD. The row, not the payload, decides. The API committed the
	// status before it fired this trigger, so a mentor whose status no longer
	// matches this action's outcome has been moved on by something newer — a
	// second moderator, the mentor's own profile toggle, or this very callback
	// arriving twice with the older of two decisions. The email that belongs to
	// this payload would then describe a decision that is no longer in force, so
	// it is dropped and nothing is written.
	//
	// This used to be the opposite: labeled "idempotency", it reconciled the row
	// TOWARD the payload, so a redelivered decline landing after an approve
	// declined a live mentor and emailed them a rejection. The trade for dropping
	// the repair is that a status change racing a legitimate decision by
	// milliseconds costs that mentor their notification email, which is a warning
	// log and a resend — not a mentor removed from the catalog.
	expectedStatus := mentorStatusActive
	switch payload.Action {
	case moderationActionDecline:
		expectedStatus = mentorStatusDeclined
	case moderationActionReturn:
		expectedStatus = mentorStatusDraft
	}
	if mentor.Status != expectedStatus {
		logger.Warn("[Mentor Moderation Action] Mentor status no longer matches this moderation action; skipping notification",
			zap.String("mentor_id", mentor.ID),
			zap.String("action", payload.Action),
			zap.String("current_status", mentor.Status),
			zap.String("expected_status", expectedStatus),
		)
		// 200: a superseded callback is a no-op, and a retry would find the same
		// state. Answering an error would invite exactly the redelivery that
		// caused this.
		trackOutcome("superseded", true)
		c.JSON(http.StatusOK, gin.H{"success": true, "superseded": true})
		return
	}

	if sendErr := h.sendEmail(ctx, job, h.moderationEmail(payload.Action, mentor)); sendErr != nil {
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

// validateModerationPayload checks the trigger payload's shape. It returns the
// analytics outcome label and the client-facing message for the first problem
// found, or ok=true when the payload is usable.
func validateModerationPayload(bindErr error, payload models.AdminModerationTriggerPayload) (outcome, msg string, ok bool) {
	switch {
	case bindErr != nil || payload.Type != "mentor_moderation":
		return "invalid_payload_type", "Invalid payload: type must be 'mentor_moderation'", false
	case payload.MentorID == "":
		return "missing_mentor_id", "Invalid payload: missing mentor_id", false
	case payload.ModeratorID == "":
		return "missing_moderator_id", "Invalid payload: missing moderator_id", false
	case payload.Action != moderationActionApprove &&
		payload.Action != moderationActionDecline &&
		payload.Action != moderationActionReturn:
		return "invalid_action", "Invalid payload: action must be approve, decline or return", false
	}
	return "", "", true
}

// moderationEmail builds the outcome notification for the mentor. The approved
// email links to their public profile (login-link guidance is baked into the
// template copy); the returned email carries the reviewer's note (written to
// mentors.moderation_note by the API before the trigger fired) plus a link to
// the profile editor; anything else is a decline.
func (h *Handlers) moderationEmail(action string, mentor *JobMentor) email.Message {
	switch action {
	case moderationActionApprove:
		return email.Message{
			TemplateName: "new-mentor-approved",
			Recipient:    mentor.Email,
			Props: map[string]interface{}{
				"first_name":               mentor.Name,
				"mentor_profile_url":       h.mentorProfileURL(mentor.Slug),
				"mentor_profile_share_url": h.mentorProfileShareURL(mentor.Slug),
				"discord_invite_link":      h.discordInviteLink,
			},
		}
	case moderationActionReturn:
		return email.Message{
			TemplateName: "new-mentor-returned",
			Recipient:    mentor.Email,
			Props: map[string]interface{}{
				"first_name":    mentor.Name,
				"reviewer_note": mentor.ModerationNote,
				"edit_url":      h.baseURL + "/mentor/profile/edit",
			},
		}
	default:
		return email.Message{
			TemplateName: "new-mentor-declined",
			Recipient:    mentor.Email,
			Props:        map[string]interface{}{"first_name": mentor.Name},
		}
	}
}
