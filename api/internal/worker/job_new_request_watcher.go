package worker

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// NewRequestWatcher ports openmentor-func/new-request-watcher/index.ts:
// normalize a fresh mentorship request, move it to 'pending' and notify the
// mentee (confirmation), the mentor and the moderators mailbox.
//
// Behavioral delta vs the func: when the request's mentor row is missing the
// func threw (tracking mentor_not_found AND a second "error" event, then
// answering 503); the worker answers a meaningful 404 with a single
// mentor_not_found event.
func (h *Handlers) NewRequestWatcher(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "new-request-watcher"

	requestID := c.Query("requestId")
	if requestID == "" {
		h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "missing_request_id",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "requestId is required"})
		return
	}

	trackError := func(errorType string) {
		h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id": requestID,
			"outcome":    "error",
			"error_type": errorType,
		})
	}

	request, err := h.repo.GetJobRequestByID(ctx, requestID)
	if err != nil {
		logger.Error("[New Client Request] Failed to fetch request", zap.String("request_id", requestID), zap.Error(err))
		trackError("db_error")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch request"})
		return
	}
	if request == nil {
		logger.Warn("[New Client Request] Request not found", zap.String("request_id", requestID))
		h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id": requestID,
			"outcome":    "request_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "request not found"})
		return
	}

	request.PreferredContact = strings.TrimSpace(request.PreferredContact)

	if err := h.repo.SetRequestContactPending(ctx, request.ID, request.PreferredContact); err != nil {
		logger.Error("[New Client Request] Failed to update request", zap.String("request_id", requestID), zap.Error(err))
		trackError("db_error")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to update request"})
		return
	}

	mentor, err := h.repo.GetJobMentorByID(ctx, request.MentorID)
	if err != nil {
		logger.Error("[New Client Request] Failed to fetch mentor",
			zap.String("request_id", requestID), zap.String("mentor_id", request.MentorID), zap.Error(err))
		trackError("db_error")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[New Client Request] Mentor not found",
			zap.String("request_id", requestID), zap.String("mentor_id", request.MentorID))
		h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.RequestDistinctID(request.ID), map[string]interface{}{
			"request_id": request.ID,
			"mentor_id":  request.MentorID,
			"outcome":    "mentor_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentor not found"})
		return
	}

	// Mentee confirmation: mentors with a calendar link get the calendly
	// variant (mirrors `mentor.calendly ? ... : ...` in the func).
	menteeMessage := email.Message{
		TemplateName: "new-request",
		Recipient:    request.Email,
		Props: map[string]interface{}{
			"first_name":      request.Name,
			"mentor_name":     mentor.Name,
			"request_details": request.Description,
			"request_price":   mentor.Price,
		},
	}
	if mentor.CalendarURL != "" {
		menteeMessage.TemplateName = "new-request-calendly"
		menteeMessage.Props["calendly_url"] = mentor.CalendarURL
	}

	// The contact details are optional free text - friendly fallback per the
	// func's NewRequestMentorEmailMessage (P2.6).
	menteeContact := "not provided"
	if request.PreferredContact != "" {
		menteeContact = request.PreferredContact
	}

	sendErr := h.sendEmails(ctx, job,
		menteeMessage,
		email.Message{
			TemplateName: "new-request-mentor",
			Recipient:    mentor.Email,
			Props: map[string]interface{}{
				"mentor_name":    mentor.Name,
				"mentee_name":    request.Name,
				"mentee_email":   request.Email,
				"mentee_contact": menteeContact,
				"mentee_request": request.Description,
			},
		},
		email.Message{
			TemplateName: "new-request-moderator",
			Recipient:    h.moderatorsEmail,
			Props: map[string]interface{}{
				"mentee_name":  request.Name,
				"mentee_level": request.Level,
				"mentor_name":  mentor.Name,
			},
		},
	)
	if sendErr != nil {
		trackError("email_send_failed")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to send emails"})
		return
	}

	h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.RequestDistinctID(request.ID), map[string]interface{}{
		"request_id":              request.ID,
		"mentor_id":               mentor.ID,
		"mentor_calendar_enabled": mentor.CalendarURL != "",
		"outcome":                 "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "requestId": request.ID})
}
