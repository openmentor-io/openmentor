package worker

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// mentorLoginPayload mirrors the JSON the API's mentor auth service POSTs
// via MENTOR_LOGIN_EMAIL_TRIGGER_URL (see internal/services/mentor_auth_service.go).
type mentorLoginPayload struct {
	Type     string `json:"type"`
	MentorID string `json:"mentor_id"`
	LoginURL string `json:"login_url"`
}

// moderatorLoginPayload mirrors the JSON the API's admin auth service POSTs
// via MODERATOR_LOGIN_EMAIL_TRIGGER_URL (see internal/services/admin_auth_service.go).
type moderatorLoginPayload struct {
	Type           string `json:"type"`
	ModeratorID    string `json:"moderator_id"`
	ModeratorEmail string `json:"moderator_email"`
	ModeratorName  string `json:"moderator_name"`
	LoginURL       string `json:"login_url"`
}

// MentorLoginEmail ports openmentor-func/mentor-login-email/index.ts:
// send the magic-link login email to a mentor.
func (h *Handlers) MentorLoginEmail(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "mentor-login-email"

	var payload mentorLoginPayload
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Type != "mentor_login" {
		h.track(ctx, analytics.EventMentorAuthLoginEmailSent, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "invalid_payload_type",
		})
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: type must be 'mentor_login'"})
		return
	}

	if payload.MentorID == "" {
		h.track(ctx, analytics.EventMentorAuthLoginEmailSent, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "missing_mentor_id",
		})
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: missing mentor_id"})
		return
	}

	if payload.LoginURL == "" {
		h.track(ctx, analytics.EventMentorAuthLoginEmailSent, analytics.MentorDistinctID(payload.MentorID), map[string]interface{}{
			"mentor_id": payload.MentorID,
			"outcome":   "missing_login_url",
		})
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: missing login_url"})
		return
	}

	mentor, err := h.repo.GetJobMentorByID(ctx, payload.MentorID)
	if err != nil {
		logger.Error("[Mentor Login Email] Failed to fetch mentor", zap.String("mentor_id", payload.MentorID), logger.RedactedError(err))
		h.track(ctx, analytics.EventMentorAuthLoginEmailSent, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome":    "error",
			"error_type": errTypeDBError,
		})
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[Mentor Login Email] Mentor not found", zap.String("mentor_id", payload.MentorID))
		h.track(ctx, analytics.EventMentorAuthLoginEmailSent, analytics.MentorDistinctID(payload.MentorID), map[string]interface{}{
			"mentor_id": payload.MentorID,
			"outcome":   "mentor_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Mentor not found"})
		return
	}

	sendErr := h.sendEmail(ctx, job, email.Message{
		TemplateName: "mentor-login",
		Recipient:    mentor.Email,
		Props: map[string]interface{}{
			"mentor_name": mentor.Name,
			"login_url":   payload.LoginURL,
		},
	})
	if sendErr != nil {
		h.track(ctx, analytics.EventMentorAuthLoginEmailSent, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome":    "error",
			"error_type": errTypeEmailSendFailed,
		})
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to send email"})
		return
	}

	logger.Info("[Mentor Login Email] Sent", zap.String("mentor_id", mentor.ID))
	h.track(ctx, analytics.EventMentorAuthLoginEmailSent, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
		"mentor_id":         mentor.ID,
		"delivery_channels": 1,
		"outcome":           "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ModeratorLoginEmail ports openmentor-func/moderator-login-email/index.ts:
// send the magic-link login email to a moderator/admin. The recipient comes
// from the payload or - when moderator_id is set - from the moderators
// table; the name defaults to "moderator". As in the func, the message
// reuses the mentor-login template.
func (h *Handlers) ModeratorLoginEmail(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "moderator-login-email"

	var payload moderatorLoginPayload
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Type != "admin_login" {
		h.track(ctx, analytics.EventAdminAuthLoginEmailSent, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "invalid_payload_type",
		})
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: type must be 'admin_login'"})
		return
	}

	moderatorProps := func(outcome string) map[string]interface{} {
		props := map[string]interface{}{"outcome": outcome}
		if payload.ModeratorID != "" {
			props["moderator_id"] = payload.ModeratorID
		}
		return props
	}

	if payload.LoginURL == "" {
		h.track(ctx, analytics.EventAdminAuthLoginEmailSent, analytics.ModeratorDistinctID(payload.ModeratorID), moderatorProps("missing_login_url"))
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: missing login_url"})
		return
	}

	recipientEmail := payload.ModeratorEmail
	recipientName := payload.ModeratorName
	if recipientName == "" {
		recipientName = "moderator"
	}

	if payload.ModeratorID != "" {
		moderator, err := h.repo.GetJobModeratorByID(ctx, payload.ModeratorID)
		if err != nil {
			logger.Error("[Moderator Login Email] Failed to fetch moderator",
				zap.String("moderator_id", payload.ModeratorID), logger.RedactedError(err))
			h.track(ctx, analytics.EventAdminAuthLoginEmailSent, analytics.SystemDistinctID("worker"), map[string]interface{}{
				"outcome":    "error",
				"error_type": errTypeDBError,
			})
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to fetch moderator"})
			return
		}
		if moderator == nil {
			logger.Warn("[Moderator Login Email] Moderator not found", zap.String("moderator_id", payload.ModeratorID))
			h.track(ctx, analytics.EventAdminAuthLoginEmailSent, analytics.ModeratorDistinctID(payload.ModeratorID), moderatorProps("moderator_not_found"))
			c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Moderator not found"})
			return
		}
		if moderator.Email != "" {
			recipientEmail = moderator.Email
		}
		if moderator.Name != "" {
			recipientName = moderator.Name
		}
	}

	if recipientEmail == "" {
		h.track(ctx, analytics.EventAdminAuthLoginEmailSent, analytics.ModeratorDistinctID(payload.ModeratorID), moderatorProps("missing_moderator_email"))
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: missing moderator email"})
		return
	}

	sendErr := h.sendEmail(ctx, job, email.Message{
		TemplateName: "mentor-login",
		Recipient:    recipientEmail,
		Props: map[string]interface{}{
			"mentor_name": recipientName,
			"login_url":   payload.LoginURL,
		},
	})
	if sendErr != nil {
		h.track(ctx, analytics.EventAdminAuthLoginEmailSent, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome":    "error",
			"error_type": errTypeEmailSendFailed,
		})
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to send email"})
		return
	}

	logger.Info("[Moderator Login Email] Sent", zap.String("moderator_id", payload.ModeratorID))
	h.track(ctx, analytics.EventAdminAuthLoginEmailSent, analytics.ModeratorDistinctID(payload.ModeratorID), moderatorProps("success"))
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// menteeLoginPayload mirrors the JSON the API's mentee service POSTs to the URL
// derived from MENTOR_LOGIN_EMAIL_TRIGGER_URL (see
// config.EventTriggerFunctionsConfig.MenteeLoginEmailTriggerURL).
type menteeLoginPayload struct {
	Type     string `json:"type"`
	MenteeID string `json:"mentee_id"`
	LoginURL string `json:"login_url"`
}

// MenteeLoginEmail sends a mentee their dashboard sign-in link (D95). The
// recipient is read from the mentees row, never taken from the payload, so the
// trigger cannot be turned into "mail this link to any address".
func (h *Handlers) MenteeLoginEmail(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "mentee-login-email"

	track := func(menteeID, outcome string, extra map[string]interface{}) {
		props := map[string]interface{}{"outcome": outcome}
		for k, v := range extra {
			props[k] = v
		}
		distinctID := analytics.SystemDistinctID("worker")
		if menteeID != "" {
			props["mentee_id"] = menteeID
			distinctID = analytics.MenteeDistinctID(menteeID)
		}
		h.track(ctx, analytics.EventMenteeAuthLoginEmailSent, distinctID, props)
	}

	var payload menteeLoginPayload
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Type != "mentee_login" {
		track("", "invalid_payload_type", nil)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: type must be 'mentee_login'"})
		return
	}
	if payload.MenteeID == "" {
		track("", "missing_mentee_id", nil)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: missing mentee_id"})
		return
	}
	if payload.LoginURL == "" {
		track(payload.MenteeID, "missing_login_url", nil)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid payload: missing login_url"})
		return
	}

	mentee, err := h.repo.GetJobMenteeByID(ctx, payload.MenteeID)
	if err != nil {
		logger.Error("[Mentee Login Email] Failed to fetch mentee", zap.String("mentee_id", payload.MenteeID), logger.RedactedError(err))
		track("", "error", map[string]interface{}{"error_type": errTypeDBError})
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to fetch mentee"})
		return
	}
	if mentee == nil {
		logger.Warn("[Mentee Login Email] Mentee not found", zap.String("mentee_id", payload.MenteeID))
		track(payload.MenteeID, "mentee_not_found", nil)
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Mentee not found"})
		return
	}

	name := mentee.Name
	if name == "" {
		name = "there"
	}
	sendErr := h.sendEmail(ctx, job, email.Message{
		TemplateName: "mentee-login",
		Recipient:    mentee.Email,
		Props: map[string]interface{}{
			"mentee_name": name,
			"login_url":   payload.LoginURL,
		},
	})
	if sendErr != nil {
		track("", "error", map[string]interface{}{"error_type": errTypeEmailSendFailed})
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to send email"})
		return
	}

	logger.Info("[Mentee Login Email] Sent", zap.String("mentee_id", mentee.ID))
	track(mentee.ID, "success", map[string]interface{}{"delivery_channels": 1})
	c.JSON(http.StatusOK, gin.H{"success": true})
}
