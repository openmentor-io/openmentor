package worker

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// PurgeDeletedProfiles is the irreversible half of profile deletion (D70): it
// erases deleted profiles, and everything attached to them, once their
// retention window has closed.
//
// Schedule and window are both config (WORKER_PROFILE_PURGE_CRON,
// WORKER_PROFILE_PURGE_RETENTION_DAYS); nightly / 30 days by default. Running
// nightly rather than monthly is what keeps the two knobs independent: the
// window alone decides how long a deletion stays undoable, so it can be tuned
// without anybody reasoning about where in the month a profile happened to land.
//
// GATE: none, deliberately — and this is the one job where that needs saying.
// The email jobs skip outside production because the alternative is mailing
// real people from a staging box (skipNonProduction). This job sends nothing;
// gating it would instead mean staging never exercises the destructive path,
// which is precisely the path worth exercising somewhere other than production.
// randomize-sort-order makes the same call for its writes, on weaker grounds.
//
// Per-profile transactions, not one for the batch: each erasure is independent,
// and a single failure — a lock timeout, a row an admin is editing — should cost
// that one profile, not abort a backlog of them. The run reports the failures
// and the next night retries them.
func (h *Handlers) PurgeDeletedProfiles(ctx context.Context) (JobSummary, error) {
	const job = "purge-deleted-profiles"
	summary := JobSummary{Job: job}

	profiles, err := h.repo.ListPurgeableProfiles(ctx, h.profilePurgeRetentionDays)
	if err != nil {
		return summary, err
	}
	summary.MentorsMatched = len(profiles)

	if len(profiles) == 0 {
		logger.Info("[Purge Deleted Profiles] Nothing past the retention window",
			zap.Int("retention_days", h.profilePurgeRetentionDays),
		)
		return summary, nil
	}

	logger.Info("[Purge Deleted Profiles] Starting retention sweep",
		zap.Int("mentors_matched", summary.MentorsMatched),
		zap.Int("retention_days", h.profilePurgeRetentionDays),
	)

	for _, profile := range profiles {
		counts, purgeErr := h.repo.PurgeProfile(ctx, profile.MentorID, h.profilePurgeRetentionDays)
		if purgeErr != nil {
			// Restored between the listing and the write: the guard worked, so
			// this is an expected outcome and not a failure of the run.
			if errors.Is(purgeErr, ErrProfileNotPurgeable) {
				summary.ProfilesSkipped++
				logger.Info("[Purge Deleted Profiles] Profile no longer eligible, skipping",
					zap.String("mentor_id", profile.MentorID),
				)
				continue
			}
			summary.PurgeFailures++
			logger.Error("[Purge Deleted Profiles] Failed to purge profile",
				zap.String("mentor_id", profile.MentorID),
				logger.RedactedError(purgeErr),
			)
			continue
		}

		summary.ProfilesPurged++
		summary.RequestsPurged += counts.Requests
		summary.ReviewsPurged += counts.Reviews
		summary.InvitationsPurged += counts.Invitations

		// Emitted per profile, AFTER the commit: this event is the only record
		// that the profile ever existed once the rows are gone, so it must not
		// claim an erasure that rolled back. The mentor id is retained
		// deliberately — with the row deleted it identifies nobody, and without
		// it the event cannot be correlated with the deletion that preceded it.
		h.track(ctx, analytics.EventMentorProfilePurged, analytics.MentorDistinctID(profile.MentorID), map[string]interface{}{
			"mentor_id":          profile.MentorID,
			"deleted_at":         profile.DeletedAt,
			"retention_days":     h.profilePurgeRetentionDays,
			"requests_purged":    counts.Requests,
			"reviews_purged":     counts.Reviews,
			"invitations_purged": counts.Invitations,
			"outcome":            "success",
		})

		logger.Info("[Purge Deleted Profiles] Profile permanently erased",
			zap.String("mentor_id", profile.MentorID),
			zap.Int("requests_purged", counts.Requests),
			zap.Int("reviews_purged", counts.Reviews),
			zap.Int("invitations_purged", counts.Invitations),
		)
	}

	logger.Info("[Purge Deleted Profiles] Retention sweep finished",
		zap.Int("profiles_purged", summary.ProfilesPurged),
		zap.Int("profiles_skipped", summary.ProfilesSkipped),
		zap.Int("purge_failures", summary.PurgeFailures),
		zap.Int("requests_purged", summary.RequestsPurged),
		zap.Int("reviews_purged", summary.ReviewsPurged),
		zap.Int("invitations_purged", summary.InvitationsPurged),
	)

	// A failed erasure does NOT fail the run: the successful ones are committed
	// and the failures are retried on the next pass. Returning an error here
	// would mark the whole sweep failed in the metrics and tell an operator
	// nothing about which profile was the problem — the per-profile log line
	// above does that. The count is in the summary the manual trigger returns.
	return summary, nil
}
