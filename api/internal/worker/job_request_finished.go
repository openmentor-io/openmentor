package worker

import (
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor-api/pkg/analytics"
	"github.com/openmentor-io/openmentor-api/pkg/email"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
)

// defaultDeclineReasonText mirrors DEFAULT_DECLINE_REASON_TEXT in the func's
// SessionDeclinedMessage.
const defaultDeclineReasonText = "They may be short on time right now, or unsure they can help with this particular topic."

// RequestProcessFinished ports openmentor-func/request-process-finished/index.ts:
// notify the mentee after the mentor finalized a request. Status 'done'
// sends session-complete, 'declined' sends session-declined (with the
// decline reason mapping from P2.6); any other status is a tracked no-op.
func (h *Handlers) RequestProcessFinished(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "request-process-finished"

	requestID := c.Query("requestId")
	if requestID == "" {
		h.track(ctx, analytics.EventRequestProcessFinishedNotified, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "missing_request_id",
		})
		c.JSON(http.StatusBadRequest, gin.H{"error": "requestId is required"})
		return
	}

	request, err := h.repo.GetJobRequestWithMentorName(ctx, requestID)
	if err != nil {
		logger.Error("[Request Process Finished] Failed to fetch request", zap.String("request_id", requestID), zap.Error(err))
		h.track(ctx, analytics.EventRequestProcessFinishedNotified, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id": requestID,
			"outcome":    "error",
			"error_type": "db_error",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Internal error"})
		return
	}
	if request == nil {
		logger.Warn("[Request Process Finished] Request not found", zap.String("request_id", requestID))
		h.track(ctx, analytics.EventRequestProcessFinishedNotified, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id": requestID,
			"outcome":    "request_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
		return
	}

	var message *email.Message
	switch request.Status {
	case "done":
		message = &email.Message{
			TemplateName: "session-complete",
			Recipient:    request.Email,
			Props: map[string]interface{}{
				"first_name":  request.Name,
				"mentor_name": request.MentorName,
				"request_id":  request.ID,
			},
		}
	case "declined":
		message = &email.Message{
			TemplateName: "session-declined",
			Recipient:    request.Email,
			Props: map[string]interface{}{
				"first_name":        request.Name,
				"mentor_name":       request.MentorName,
				"decline_info":      buildDeclineInfoHTML(request.DeclineReason, request.DeclineComment),
				"decline_info_text": buildDeclineInfoText(request.DeclineReason, request.DeclineComment),
			},
		}
	default:
		h.track(ctx, analytics.EventRequestProcessFinishedNotified, analytics.RequestDistinctID(request.ID), map[string]interface{}{
			"request_id": request.ID,
			"mentor_id":  request.MentorID,
			"status":     request.Status,
			"outcome":    "status_not_actionable",
		})
		c.JSON(http.StatusOK, gin.H{"success": true, "requestId": requestID})
		return
	}

	if sendErr := h.sendEmail(ctx, job, *message); sendErr != nil {
		h.track(ctx, analytics.EventRequestProcessFinishedNotified, analytics.RequestDistinctID(requestID), map[string]interface{}{
			"request_id": requestID,
			"outcome":    "error",
			"error_type": "email_send_failed",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Internal error"})
		return
	}

	logger.Info("[Request Process Finished] Notification sent",
		zap.String("request_id", request.ID),
		zap.String("status", request.Status),
	)
	h.track(ctx, analytics.EventRequestProcessFinishedNotified, analytics.RequestDistinctID(request.ID), map[string]interface{}{
		"request_id": request.ID,
		"mentor_id":  request.MentorID,
		"status":     request.Status,
		"outcome":    "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "requestId": requestID})
}

// mapDeclineReason mirrors SessionDeclinedMessage.mapDeclineReason (P2.6):
// known decline reason codes map to friendly copy; unknown codes pass through.
func mapDeclineReason(reason string) string {
	switch reason {
	case "no_time":
		return "No time at the moment"
	case "topic_mismatch":
		return "The topic isn't a good fit"
	case "helping_others":
		return "Already helping other mentees"
	case "on_break":
		return "Taking a break from mentoring"
	case "other":
		return "Other"
	default:
		return reason
	}
}

// buildDeclineInfoHTML mirrors SessionDeclinedMessage.buildDeclineInfoHtml.
func buildDeclineInfoHTML(reason, comment string) string {
	hasReason := strings.TrimSpace(reason) != ""
	hasComment := strings.TrimSpace(comment) != ""

	if !hasReason && !hasComment {
		return defaultDeclineReasonText
	}

	var b strings.Builder
	b.WriteString("<br><br>")
	if hasReason {
		b.WriteString(`<div style="font-family: inherit; text-align: inherit"><strong>Reason:</strong> `)
		b.WriteString(html.EscapeString(mapDeclineReason(reason)))
		b.WriteString(`<br></div>`)
	}
	if hasComment {
		b.WriteString(`<div style="font-family: inherit; text-align: inherit"><strong>Comment:</strong> `)
		b.WriteString(html.EscapeString(comment))
		b.WriteString(`</div>`)
	}
	return b.String()
}

// buildDeclineInfoText mirrors SessionDeclinedMessage.buildDeclineInfoText.
func buildDeclineInfoText(reason, comment string) string {
	hasReason := strings.TrimSpace(reason) != ""
	hasComment := strings.TrimSpace(comment) != ""

	if !hasReason && !hasComment {
		return defaultDeclineReasonText
	}

	var b strings.Builder
	b.WriteString("\n\n")
	if hasReason {
		b.WriteString("Reason: ")
		b.WriteString(mapDeclineReason(reason))
		b.WriteString("\n")
	}
	if hasComment {
		b.WriteString("Comment: ")
		b.WriteString(comment)
	}
	return b.String()
}
