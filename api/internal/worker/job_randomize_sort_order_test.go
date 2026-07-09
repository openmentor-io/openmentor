package worker

import (
	"context"
	"testing"

	"github.com/openmentor-io/openmentor-api/config"
)

func TestRandomizeSortOrderWritesInOneTransaction(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Worker.HighlightedMentors = "star-1,star-2"
	})
	env.repo.activeMentorIDs = []string{"m1", "m2", "m3"}

	summary, err := env.handlers.RandomizeSortOrder(context.Background())
	if err != nil {
		t.Fatalf("RandomizeSortOrder returned error: %v", err)
	}

	if summary.MentorsMatched != 3 || summary.SortOrdersRandomized != 3 || summary.HighlightedPinned != 2 {
		t.Errorf("summary = %+v, want 3 matched / 3 randomized / 2 pinned", summary)
	}
	if summary.WritesSkipped {
		t.Error("writes must not be skipped in production")
	}

	// ONE transaction with all updates: randomized actives first, pins last.
	if len(env.repo.sortOrderTransactions) != 1 {
		t.Fatalf("SetSortOrders called %d times, want exactly 1", len(env.repo.sortOrderTransactions))
	}
	updates := env.repo.sortOrderTransactions[0]
	if len(updates) != 5 {
		t.Fatalf("transaction has %d updates, want 5 (3 active + 2 pins)", len(updates))
	}

	// Random orders sit strictly above the pin range:
	// floor(random()*1000) + highlighted + 1 -> [3, 1002] with 2 pins.
	for i, u := range updates[:3] {
		if u.MentorID != env.repo.activeMentorIDs[i] {
			t.Errorf("update %d mentor = %s, want %s", i, u.MentorID, env.repo.activeMentorIDs[i])
		}
		if u.SortOrder < 3 || u.SortOrder > 1002 {
			t.Errorf("random sort order %d out of func range [3,1002]", u.SortOrder)
		}
	}

	// Highlighted pins come LAST (so they win over the random orders) with
	// sort_order 1..N in HIGHLIGHTED_MENTORS list order.
	if updates[3] != (SortOrderUpdate{MentorID: "star-1", SortOrder: 1}) {
		t.Errorf("pin 1 = %+v, want star-1 -> 1", updates[3])
	}
	if updates[4] != (SortOrderUpdate{MentorID: "star-2", SortOrder: 2}) {
		t.Errorf("pin 2 = %+v, want star-2 -> 2", updates[4])
	}

	// The func tracked no analytics for this job.
	if env.tracker.count() != 0 {
		t.Errorf("tracked %d analytics events, want 0 (func parity)", env.tracker.count())
	}
}

func TestRandomizeSortOrderWithoutHighlightedMentors(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.activeMentorIDs = []string{"m1"}

	summary, err := env.handlers.RandomizeSortOrder(context.Background())
	if err != nil {
		t.Fatalf("RandomizeSortOrder returned error: %v", err)
	}
	if summary.HighlightedPinned != 0 {
		t.Errorf("pinned = %d, want 0", summary.HighlightedPinned)
	}

	updates := env.repo.sortOrderTransactions[0]
	if len(updates) != 1 {
		t.Fatalf("transaction has %d updates, want 1", len(updates))
	}
	// Range with 0 pins: [1, 1000].
	if updates[0].SortOrder < 1 || updates[0].SortOrder > 1000 {
		t.Errorf("random sort order %d out of func range [1,1000]", updates[0].SortOrder)
	}
}

func TestRandomizeSortOrderRunsButSkipsWritesOutsideProduction(t *testing.T) {
	// Special gate (func parity): unlike the email jobs, the job itself is
	// NOT skipped outside production - it runs and reports the matched
	// count - but the DB writes are.
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "development"
	})
	env.repo.activeMentorIDs = []string{"m1", "m2"}

	summary, err := env.handlers.RandomizeSortOrder(context.Background())
	if err != nil {
		t.Fatalf("RandomizeSortOrder returned error: %v", err)
	}
	if summary.Skipped {
		t.Error("randomize-sort-order must not report Skipped (it always runs)")
	}
	if !summary.WritesSkipped {
		t.Error("writes must be skipped outside production")
	}
	if summary.MentorsMatched != 2 {
		t.Errorf("mentors_matched = %d, want 2", summary.MentorsMatched)
	}
	if len(env.repo.sortOrderTransactions) != 0 {
		t.Errorf("SetSortOrders called %d times outside production, want 0", len(env.repo.sortOrderTransactions))
	}
}

func TestRandomizeSortOrderDevEmailOverrideDoesNotUnlockWrites(t *testing.T) {
	// The func's write guard was APP_ENV === 'production' only - no
	// DEV_EMAIL_OVERRIDE unlock (the job sends no email).
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "development"
		cfg.Email.DevEmailOverride = "dev@example.com"
	})
	env.repo.activeMentorIDs = []string{"m1"}

	summary, err := env.handlers.RandomizeSortOrder(context.Background())
	if err != nil {
		t.Fatalf("RandomizeSortOrder returned error: %v", err)
	}
	if !summary.WritesSkipped {
		t.Error("DEV_EMAIL_OVERRIDE must NOT unlock randomize writes outside production")
	}
	if len(env.repo.sortOrderTransactions) != 0 {
		t.Errorf("SetSortOrders called %d times, want 0", len(env.repo.sortOrderTransactions))
	}
}

func TestRandomizeSortOrderTransactionErrorSurfaces(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.activeMentorIDs = []string{"m1"}
	env.repo.setSortOrdersErr = errDBDown

	summary, err := env.handlers.RandomizeSortOrder(context.Background())
	if err == nil {
		t.Fatal("a transaction failure must surface as an error")
	}
	if summary.SortOrdersRandomized != 0 {
		t.Errorf("summary must not report randomized rows on failure: %+v", summary)
	}
}
