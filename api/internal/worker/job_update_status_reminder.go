package worker

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor-api/pkg/analytics"
	"github.com/openmentor-io/openmentor-api/pkg/email"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
)

// requestStatusLabels mirrors STATUS_LABELS in the func's
// StatusUpdateReminderMessage (unknown statuses fall back to the raw value).
var requestStatusLabels = map[string]string{
	"contacted": "Contacted",
	"working":   "In progress",
}

// UpdateStatusReminder ports openmentor-func/update-status-reminder/index.ts
// (Wednesdays 10:00): find non-declined mentors with requests stuck in
// 'contacted'/'working' for more than 120 hours (by status_changed_at) and
// send each ONE status-update-reminder email listing the stale sessions.
//
// Error semantics mirror the func: email sends are isolated per mentor,
// DB failures abort the run.
func (h *Handlers) UpdateStatusReminder(ctx context.Context) (JobSummary, error) {
	const job = "update-status-reminder"
	summary := JobSummary{Job: job}

	if h.skipNonProduction() {
		h.track(ctx, analytics.EventMentorStatusUpdateReminded, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "skipped_non_production",
		})
		summary.Skipped = true
		return summary, nil
	}

	mentors, err := h.repo.ListMentorsWithStaleInProgressRequests(ctx)
	if err != nil {
		h.trackUpdateStatusReminderError(ctx)
		return summary, err
	}
	summary.MentorsMatched = len(mentors)

	for _, mentor := range mentors {
		requests, err := h.repo.ListStaleInProgressRequests(ctx, mentor.ID)
		if err != nil {
			h.trackUpdateStatusReminderError(ctx)
			return summary, err
		}
		if len(requests) == 0 {
			continue
		}

		if err := h.sendEmail(ctx, job, statusUpdateReminderMessage(mentor, requests)); err != nil {
			summary.EmailFailures++
			h.track(ctx, analytics.EventMentorStatusUpdateReminded, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
				"mentor_id":            mentor.ID,
				"stale_requests_count": len(requests),
				"outcome":              "error",
				"error_type":           "email_send_failed",
			})
			continue
		}

		summary.EmailsSent++
		h.track(ctx, analytics.EventMentorStatusUpdateReminded, analytics.MentorDistinctID(mentor.ID), map[string]interface{}{
			"mentor_id":            mentor.ID,
			"stale_requests_count": len(requests),
			"outcome":              "success",
		})
	}

	logger.Info("[Update Status Reminder] Run completed",
		zap.Int("mentors_matched", summary.MentorsMatched),
		zap.Int("emails_sent", summary.EmailsSent),
		zap.Int("email_failures", summary.EmailFailures),
	)
	// mentors_reminded_count mirrors the func's run_completed event
	// (mentors.length: all matched mentors).
	h.track(ctx, analytics.EventMentorStatusUpdateReminded, analytics.SystemDistinctID("worker"), map[string]interface{}{
		"mentors_reminded_count": len(mentors),
		"outcome":                "run_completed",
	})

	return summary, nil
}

func (h *Handlers) trackUpdateStatusReminderError(ctx context.Context) {
	h.track(ctx, analytics.EventMentorStatusUpdateReminded, analytics.SystemDistinctID("worker"), map[string]interface{}{
		"outcome":    "error",
		"error_type": "db_error",
	})
}

// statusUpdateReminderMessage ports StatusUpdateReminderMessage: props
// mentor_name, requests_list (HTML) and requests_list_text.
func statusUpdateReminderMessage(mentor JobMentor, requests []JobReminderRequest) email.Message {
	htmlItems := make([]string, 0, len(requests))
	textItems := make([]string, 0, len(requests))

	for _, req := range requests {
		line := describeStaleRequest(req)
		htmlItems = append(htmlItems, `<li style="margin-bottom: 8px;">`+escapeHTML(line)+`</li>`)
		textItems = append(textItems, "- "+line)
	}

	return email.Message{
		TemplateName: "status-update-reminder",
		Recipient:    mentor.Email,
		Props: map[string]interface{}{
			"mentor_name":        mentor.Name,
			"requests_list":      requestsListHTML(htmlItems),
			"requests_list_text": strings.Join(textItems, "\n"),
		},
	}
}

// describeStaleRequest mirrors describeRequest() in the func's message
// class: `{name} — "{status label}" for {1 day|N days}` where the days
// value is the time since the last status change.
func describeStaleRequest(req JobReminderRequest) string {
	label, ok := requestStatusLabels[req.Status]
	if !ok {
		label = req.Status
	}
	return req.Name + ` — "` + label + `" for ` + daysWording(req.DaysAgo)
}
