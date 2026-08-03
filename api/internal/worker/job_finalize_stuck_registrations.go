package worker

import (
	"context"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// FinalizeStuckRegistrations is the safety net under the new-mentor-watcher
// trigger, which the API fires from a detached goroutine with no persistence, no
// retry and no shutdown drain. It finds draft registrations left holding no
// usable confirmation link (ListStuckDraftRegistrations documents the two shapes
// that qualify) and replays the SAME idempotent finalization the callback
// performs, so a dropped trigger or an undelivered link converges within one
// cron interval instead of never.
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
				logger.RedactedError(finalizeErr),
			)
			if res.ErrorType == errTypeEmailSendFailed {
				// finalizeNewMentor released the claim it made before sending, so
				// the row is back in the replay set for the next pass.
				summary.EmailFailures++
			}
			continue
		}
		if res.Superseded {
			// The mentor confirmed, or a moderator acted, between the listing and
			// the write: no email is owed and the row has left the replay set.
			logger.Info("[Finalize Stuck Registrations] Registration already left draft",
				zap.String("mentor_id", mentor.ID))
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
