package worker

import (
	"context"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// FinalizeStuckRegistrations is the safety net under the new-mentor-watcher
// trigger. The API fires that trigger from a detached goroutine with no
// persistence, no retry and no shutdown drain, so one lost HTTP call leaves a
// registration finalized by nobody: status 'draft', sort_order NULL, no
// confirmation token — the mentor never receives a link and can never get in.
// This job finds those rows (see ListStuckDraftRegistrations) and replays the
// SAME idempotent finalization the callback performs, so a dropped trigger
// converges within one cron interval instead of never.
func (h *Handlers) FinalizeStuckRegistrations(ctx context.Context) (JobSummary, error) {
	const job = "finalize-stuck-registrations"
	summary := JobSummary{Job: job}

	// Same gate as the other email jobs: the replay sends the confirmation
	// email the lost trigger owed the mentor.
	if h.skipNonProduction() {
		summary.Skipped = true
		return summary, nil
	}

	mentors, err := h.repo.ListStuckDraftRegistrations(ctx)
	if err != nil {
		return summary, err
	}
	summary.MentorsMatched = len(mentors)

	for _, mentor := range mentors {
		res, finalizeErr := h.finalizeNewMentor(ctx, mentor.ID)
		if finalizeErr != nil {
			// One bad row must not abort the rest — the next run retries it.
			logger.Error("[Finalize Stuck Registrations] Finalization failed",
				zap.String("mentor_id", mentor.ID),
				zap.String("error_type", res.ErrorType),
				zap.Error(finalizeErr),
			)
			if res.ErrorType == "email_send_failed" {
				// The DB write landed; only the email is owed.
				summary.MentorsFinalized++
				summary.EmailFailures++
			}
			continue
		}
		summary.MentorsFinalized++
		summary.EmailsSent++
		logger.Info("[Finalize Stuck Registrations] Replayed lost finalization",
			zap.String("mentor_id", mentor.ID),
			zap.String("status", res.Status),
		)
	}

	logger.Info("[Finalize Stuck Registrations] Run completed",
		zap.Int("mentors_matched", summary.MentorsMatched),
		zap.Int("mentors_finalized", summary.MentorsFinalized),
		zap.Int("emails_sent", summary.EmailsSent),
		zap.Int("email_failures", summary.EmailFailures),
	)
	return summary, nil
}
