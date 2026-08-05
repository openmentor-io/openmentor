package worker

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
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
		h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome":    "error",
			"error_type": errorType,
		})
	}

	request, err := h.repo.GetJobRequestByID(ctx, requestID)
	if err != nil {
		logger.Error("[New Client Request] Failed to fetch request", zap.String("request_ref", redact.ID(requestID)), logger.RedactedError(err))
		trackError(errTypeDBError)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch request"})
		return
	}
	if request == nil {
		logger.Warn("[New Client Request] Request not found", zap.String("request_ref", redact.ID(requestID)))
		h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "request_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "request not found"})
		return
	}

	request.PreferredContact = strings.TrimSpace(request.PreferredContact)

	// Every fallible step that is only a READ happens before the claim, so that
	// failing one leaves the request claimable. The mentor fetch used to sit
	// after the claim: a transient DB error there answered 503 with the claim
	// already burned, and every later replay landed in the superseded branch
	// below — the three announcement emails lost until an operator hand-repaired
	// status_changed_at.
	mentor, err := h.repo.GetJobMentorByID(ctx, request.MentorID)
	if err != nil {
		logger.Error("[New Client Request] Failed to fetch mentor",
			zap.String("request_ref", redact.ID(requestID)), zap.String("mentor_id", request.MentorID), logger.RedactedError(err))
		trackError(errTypeDBError)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[New Client Request] Mentor not found",
			zap.String("request_ref", redact.ID(requestID)), zap.String("mentor_id", request.MentorID))
		h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.MentorDistinctID(request.MentorID), map[string]interface{}{
			"mentor_id": request.MentorID,
			"outcome":   "mentor_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentor not found"})
		return
	}

	// The request is CLAIMED directly against the sends below: all three emails
	// announce a brand-new request waiting for its mentor, so none of them may go
	// out for a request this call did not just move into that state. A replay after
	// the request advanced — or a second delivery of the same callback — loses the
	// claim and stops here.
	//
	// A send failure after this point still consumes the claim. That window is
	// inherent, not an oversight: sendEmails attempts every message even when an
	// earlier one fails, so releasing the claim on send error (the way
	// finalizeNewMentor does) would let a replay re-send whatever already went out.
	applied, err := h.repo.SetRequestContactPending(ctx, request.ID, request.PreferredContact)
	if err != nil {
		logger.Error("[New Client Request] Failed to update request", zap.String("request_ref", redact.ID(requestID)), logger.RedactedError(err))
		trackError(errTypeDBError)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to update request"})
		return
	}
	if !applied {
		logger.Info("[New Client Request] Request already processed or no longer new, skipping",
			zap.String("request_ref", redact.ID(requestID)))
		h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "superseded",
		})
		// Idempotent no-op, so 200: a retry would find the same state.
		c.JSON(http.StatusOK, gin.H{"success": true, "requestId": request.ID, "superseded": true})
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
				"request_url":    h.mentorRequestURL(request.ID),
			},
		},
		email.Message{
			TemplateName: "new-request-moderator",
			Recipient:    h.moderatorsEmail,
			Props: map[string]interface{}{
				"mentee_name":  request.Name,
				"mentee_level": request.Level,
				"mentor_name":  mentor.Name,
				"request_url":  h.adminRequestURL(mentor.ID, request.ID),
				"admin_url":    h.adminPortalURL(),
			},
		},
	)
	if sendErr != nil {
		trackError(errTypeEmailSendFailed)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to send emails"})
		return
	}

	h.track(ctx, analytics.EventNewRequestWatcherProcessed, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
		"mentor_id":               mentor.ID,
		"mentor_calendar_enabled": mentor.CalendarURL != "",
		"outcome":                 "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "requestId": request.ID})
}
