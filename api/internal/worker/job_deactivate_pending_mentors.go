package worker

import (
	"context"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor-api/pkg/email"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
)

// DeactivatePendingMentors ports
// openmentor-func/deactivate-pending-mentors/index.ts (Wednesdays 10:00):
// mentors with status='active' that have requests pending for more than
// 30 days are set to status='inactive' and notified with a
// profile-deactivated email (the reactivation steps - magic-link login,
// resolve requests, re-activate from profile edit - are baked into the
// template; the only prop is mentor_name).
//
// Error semantics mirror the func: the status UPDATE ran outside the
// per-mentor try/catch, so a DB failure aborts the run; the email send is
// isolated per mentor. The func tracked no analytics for this job.
//
// Public visibility note: deactivation removes the mentor from the public
// catalog, which the API serves from its in-memory mentors cache. The
// worker is a separate process and cannot invalidate that cache; like the
// func app, we rely on the cache's TTL refresh (MENTOR_CACHE_TTL, default
// 10 minutes) to pick the change up. Cross-process invalidation is
// deliberately NOT built.
func (h *Handlers) DeactivatePendingMentors(ctx context.Context) (JobSummary, error) {
	const job = "deactivate-pending-mentors"
	summary := JobSummary{Job: job}

	// Same gate as the other email jobs; the func returned silently here
	// (no skip analytics, unlike the two reminder jobs).
	if h.skipNonProduction() {
		summary.Skipped = true
		return summary, nil
	}

	mentors, err := h.repo.ListMentorsToDeactivate(ctx)
	if err != nil {
		return summary, err
	}
	summary.MentorsMatched = len(mentors)

	for _, mentor := range mentors {
		if err := h.repo.DeactivateMentor(ctx, mentor.ID); err != nil {
			return summary, err
		}
		summary.MentorsDeactivated++

		if err := h.sendEmail(ctx, job, email.Message{
			TemplateName: "profile-deactivated",
			Recipient:    mentor.Email,
			Props: map[string]interface{}{
				"mentor_name": mentor.Name,
			},
		}); err != nil {
			// Mirrors the func's per-mentor try/catch: the mentor stays
			// deactivated and the loop continues.
			summary.EmailFailures++
			continue
		}
		summary.EmailsSent++
	}

	logger.Info("[Deactivate Pending Mentors] Run completed",
		zap.Int("mentors_matched", summary.MentorsMatched),
		zap.Int("mentors_deactivated", summary.MentorsDeactivated),
		zap.Int("emails_sent", summary.EmailsSent),
		zap.Int("email_failures", summary.EmailFailures),
	)
	return summary, nil
}
