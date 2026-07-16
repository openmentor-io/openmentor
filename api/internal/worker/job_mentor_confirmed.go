package worker

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// MentorConfirmed handles /jobs/mentor-confirmed?mentorId= — fired by the
// API after a mentor confirms their email (draft -> pending) or resubmits
// a returned draft from the portal. It sends the emails that used to go
// out at registration time: the "application in review" message to the
// mentor and the new-mentor notification to the moderators.
func (h *Handlers) MentorConfirmed(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "mentor-confirmed"

	mentorID := c.Query("mentorId")
	if mentorID == "" {
		h.track(ctx, analytics.EventMentorConfirmedProcessed, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "missing_mentor_id",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentorId is required"})
		return
	}

	mentor, err := h.repo.GetJobMentorByID(ctx, mentorID)
	if err != nil {
		logger.Error("[Mentor Confirmed] Failed to fetch mentor", zap.String("mentor_id", mentorID), zap.Error(err))
		h.track(ctx, analytics.EventMentorConfirmedProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":  mentorID,
			"outcome":    "error",
			"error_type": "db_error",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[Mentor Confirmed] Mentor not found", zap.String("mentor_id", mentorID))
		h.track(ctx, analytics.EventMentorConfirmedProcessed, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id": mentorID,
			"outcome":   "mentor_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentor not found"})
		return
	}

	sendErr := h.sendEmails(ctx, job,
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
	if sendErr != nil {
		h.track(ctx, analytics.EventMentorConfirmedProcessed, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
			"mentor_id":  mentor.ID,
			"outcome":    "error",
			"error_type": "email_send_failed",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to send emails"})
		return
	}

	h.track(ctx, analytics.EventMentorConfirmedProcessed, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
		"mentor_id": mentor.ID,
		"status":    mentor.Status,
		"outcome":   "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "mentorId": mentor.ID})
}

// MentorConfirmEmail handles /jobs/mentor-confirm-email?mentorId= — fired
// by the API's confirmation resend endpoint after it stored a fresh token
// on the mentor row. It (re)sends the mentor-confirm-email message with
// the confirmation link.
func (h *Handlers) MentorConfirmEmail(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "mentor-confirm-email"

	mentorID := c.Query("mentorId")
	if mentorID == "" {
		h.track(ctx, analytics.EventMentorConfirmEmailSent, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "missing_mentor_id",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentorId is required"})
		return
	}

	mentor, err := h.repo.GetJobMentorByID(ctx, mentorID)
	if err != nil {
		logger.Error("[Mentor Confirm Email] Failed to fetch mentor", zap.String("mentor_id", mentorID), zap.Error(err))
		h.track(ctx, analytics.EventMentorConfirmEmailSent, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":  mentorID,
			"outcome":    "error",
			"error_type": "db_error",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[Mentor Confirm Email] Mentor not found", zap.String("mentor_id", mentorID))
		h.track(ctx, analytics.EventMentorConfirmEmailSent, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id": mentorID,
			"outcome":   "mentor_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentor not found"})
		return
	}
	if mentor.EmailConfirmationToken == "" {
		logger.Warn("[Mentor Confirm Email] Mentor has no confirmation token", zap.String("mentor_id", mentorID))
		h.track(ctx, analytics.EventMentorConfirmEmailSent, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id": mentorID,
			"outcome":   "no_confirmation_token",
		})
		c.JSON(http.StatusConflict, gin.H{"error": "mentor has no confirmation token"})
		return
	}

	sendErr := h.sendEmail(ctx, job, email.Message{
		TemplateName: "mentor-confirm-email",
		Recipient:    mentor.Email,
		Props: map[string]interface{}{
			"first_name":  mentor.Name,
			"confirm_url": h.baseURL + "/mentor/confirm?token=" + mentor.EmailConfirmationToken,
		},
	})
	if sendErr != nil {
		h.track(ctx, analytics.EventMentorConfirmEmailSent, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
			"mentor_id":  mentor.ID,
			"outcome":    "error",
			"error_type": "email_send_failed",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to send email"})
		return
	}

	h.track(ctx, analytics.EventMentorConfirmEmailSent, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
		"mentor_id": mentor.ID,
		"outcome":   "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "mentorId": mentor.ID})
}
