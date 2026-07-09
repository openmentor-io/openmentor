package worker

import (
	"context"
	"math/rand/v2"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// RandomizeSortOrder ports openmentor-func/randomize-sort-order/index.ts
// (daily 01:00): shuffle the catalog by giving every active mentor a random
// sort_order, then pin the HIGHLIGHTED_MENTORS ids to the top - all in ONE
// transaction.
//
// GATE (deliberately different from the other jobs): the func ran this
// timer unconditionally - no skipNonProduction() gate and no
// DEV_EMAIL_OVERRIDE unlock (it sends no email) - but guarded the DB
// WRITES on APP_ENV === 'production' only. Replicated exactly: outside
// production the job runs (and reports the matched count) but skips the
// writes, even when DEV_EMAIL_OVERRIDE is set.
//
// Per the func: random orders are floor(random()*1000) + highlighted + 1,
// i.e. always ABOVE the pin range; highlighted mentors get sort_order 1..N
// in HIGHLIGHTED_MENTORS list order, written after the random orders so
// the pins win when a highlighted mentor is also in the active set. The
// func tracked no analytics for this job.
func (h *Handlers) RandomizeSortOrder(ctx context.Context) (JobSummary, error) {
	const job = "randomize-sort-order"
	summary := JobSummary{Job: job}

	mentorIDs, err := h.repo.ListActiveMentorIDs(ctx)
	if err != nil {
		return summary, err
	}
	summary.MentorsMatched = len(mentorIDs)

	if h.appEnv != "production" {
		summary.WritesSkipped = true
		logger.Info("[Randomize Sort Order] Skipping sort order writes outside production",
			zap.Int("mentors_matched", summary.MentorsMatched),
		)
		return summary, nil
	}

	highlighted := h.highlightedMentors
	updates := make([]SortOrderUpdate, 0, len(mentorIDs)+len(highlighted))
	for _, id := range mentorIDs {
		updates = append(updates, SortOrderUpdate{
			MentorID:  id,
			SortOrder: rand.IntN(1000) + len(highlighted) + 1, //nolint:gosec // shuffle, not crypto
		})
	}
	for i, id := range highlighted {
		updates = append(updates, SortOrderUpdate{MentorID: id, SortOrder: i + 1})
	}

	if err := h.repo.SetSortOrders(ctx, updates); err != nil {
		return summary, err
	}
	summary.SortOrdersRandomized = len(mentorIDs)
	summary.HighlightedPinned = len(highlighted)

	logger.Info("[Randomize Sort Order] Successfully updated mentor sort orders",
		zap.Int("sort_orders_randomized", summary.SortOrdersRandomized),
		zap.Int("highlighted_pinned", summary.HighlightedPinned),
	)
	return summary, nil
}
