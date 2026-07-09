package worker

import (
	"context"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// descriptionPreviewLength mirrors DESCRIPTION_PREVIEW_LENGTH in the func's
// PendingRequestsReminderMessage.
const descriptionPreviewLength = 160

// SessionsWatcher ports openmentor-func/sessions-watcher/index.ts (daily
// 08:30): find active mentors with pending requests older than 1 day and
// send each ONE pending-requests-reminder email listing their stale
// requests, with the dashboard link and the 30-day deactivation warning
// (both baked into the template).
//
// Error semantics mirror the func: email sends are isolated per mentor
// (one failure logs, tracks and continues), while a DB failure aborts the
// run - the func's queries ran inside the outer try/catch.
func (h *Handlers) SessionsWatcher(ctx context.Context) (JobSummary, error) {
	const job = "sessions-watcher"
	summary := JobSummary{Job: job}

	if h.skipNonProduction() {
		h.track(ctx, analytics.EventMentorPendingRequestsReminded, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "skipped_non_production",
		})
		summary.Skipped = true
		return summary, nil
	}

	mentors, err := h.repo.ListMentorsWithStalePendingRequests(ctx)
	if err != nil {
		h.trackSessionsWatcherError(ctx)
		return summary, err
	}
	summary.MentorsMatched = len(mentors)

	for _, mentor := range mentors {
		requests, err := h.repo.ListStalePendingRequests(ctx, mentor.ID)
		if err != nil {
			h.trackSessionsWatcherError(ctx)
			return summary, err
		}
		if len(requests) == 0 {
			continue
		}

		if err := h.sendEmail(ctx, job, pendingRequestsReminderMessage(mentor, requests)); err != nil {
			summary.EmailFailures++
			h.track(ctx, analytics.EventMentorPendingRequestsReminded, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
				"mentor_id":              mentor.ID,
				"pending_requests_count": len(requests),
				"outcome":                "error",
				"error_type":             "email_send_failed",
			})
			continue
		}

		summary.EmailsSent++
		h.track(ctx, analytics.EventMentorPendingRequestsReminded, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
			"mentor_id":              mentor.ID,
			"pending_requests_count": len(requests),
			"outcome":                "success",
		})
	}

	logger.Info("[Session Watcher] Run completed",
		zap.Int("mentors_matched", summary.MentorsMatched),
		zap.Int("emails_sent", summary.EmailsSent),
		zap.Int("email_failures", summary.EmailFailures),
	)
	// mentors_reminded_count mirrors the func's run_completed event, which
	// reported mentors.size (all matched mentors, even ones whose send
	// failed or that had no stale requests by send time).
	h.track(ctx, analytics.EventMentorPendingRequestsReminded, analytics.SystemDistinctID("worker"), map[string]interface{}{
		"mentors_reminded_count": len(mentors),
		"outcome":                "run_completed",
	})

	return summary, nil
}

func (h *Handlers) trackSessionsWatcherError(ctx context.Context) {
	h.track(ctx, analytics.EventMentorPendingRequestsReminded, analytics.SystemDistinctID("worker"), map[string]interface{}{
		"outcome":    "error",
		"error_type": "db_error",
	})
}

// pendingRequestsReminderMessage ports PendingRequestsReminderMessage:
// props mentor_name, pending_count (stringified, like String(...) in JS),
// requests_list (HTML) and requests_list_text.
func pendingRequestsReminderMessage(mentor JobMentor, requests []JobReminderRequest) email.Message {
	htmlItems := make([]string, 0, len(requests))
	textItems := make([]string, 0, len(requests))

	for _, req := range requests {
		title, preview := describePendingRequest(req)

		item := `<li style="margin-bottom: 8px;"><strong>` + escapeHTML(title) + `</strong>`
		if preview != "" {
			item += `<br><em>` + escapeHTML(preview) + `</em>`
		}
		item += `</li>`
		htmlItems = append(htmlItems, item)

		text := "- " + title
		if preview != "" {
			text += "\n  " + preview
		}
		textItems = append(textItems, text)
	}

	return email.Message{
		TemplateName: "pending-requests-reminder",
		Recipient:    mentor.Email,
		Props: map[string]interface{}{
			"mentor_name":        mentor.Name,
			"pending_count":      strconv.Itoa(len(requests)),
			"requests_list":      requestsListHTML(htmlItems),
			"requests_list_text": strings.Join(textItems, "\n"),
		},
	}
}

// describePendingRequest mirrors describeRequest() in the func's message
// class: "{name} — waiting for {1 day|N days}" plus a 160-char description
// preview ("..."-suffixed when truncated).
func describePendingRequest(req JobReminderRequest) (title, preview string) {
	title = req.Name + " — waiting for " + daysWording(req.DaysAgo)

	preview = req.Description
	if runes := []rune(preview); len(runes) > descriptionPreviewLength {
		preview = string(runes[:descriptionPreviewLength]) + "..."
	}
	return title, preview
}
