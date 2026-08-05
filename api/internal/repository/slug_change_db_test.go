package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ChangeSlug is one transaction that takes a row lock and two advisory locks and
// then does six ordered things (cooldown, availability, reclaim, retire, trim,
// update). Every property worth having is a property of Postgres under
// concurrency — a lock that serializes, a UNIQUE that backstops a race, an
// ORDER BY over timestamps two transactions wrote — so none of it can be tested
// against a fake repository.
//
// Requires a database (see the dbtest package docs); skips without one.

const changeCooldown = 14 * 24 * time.Hour

func slugHistory(t *testing.T, pool *pgxpool.Pool, mentorID string) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(),
		`SELECT slug FROM mentor_slug_history WHERE mentor_id = $1 ORDER BY created_at DESC`, mentorID)
	require.NoError(t, err)
	defer rows.Close()

	var slugs []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		slugs = append(slugs, s)
	}
	require.NoError(t, rows.Err())
	return slugs
}

func currentSlug(t *testing.T, pool *pgxpool.Pool, mentorID string) string {
	t.Helper()
	var s string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT slug FROM mentors WHERE id = $1`, mentorID).Scan(&s))
	return s
}

// TestChangeSlugRetiresTheOldSlugAsARedirect is the base case, checked through
// the read path that matters: the retired slug must still resolve to the mentor,
// carrying their CURRENT slug so the handler can 301.
func TestChangeSlugRetiresTheOldSlugAsARedirect(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "slug-retire-old")

	old, err := repo.ChangeSlug(ctx, id, "slug-retire-new", "mentor", changeCooldown, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "slug-retire-old", old)
	assert.Equal(t, "slug-retire-new", currentSlug(t, pool, id))
	assert.Equal(t, []string{"slug-retire-old"}, slugHistory(t, pool, id))

	// The redirect resolves, and reports the new slug.
	mentor, err := repo.GetBySlug(ctx, "slug-retire-old", models.FilterOptions{})
	require.NoError(t, err)
	assert.Equal(t, id, mentor.MentorID)
	assert.Equal(t, "slug-retire-new", mentor.Slug)
}

// TestChangeSlugToTheSameSlugIsANoOp: no history row, no cooldown consumed.
func TestChangeSlugToTheSameSlugIsANoOp(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "slug-noop")

	old, err := repo.ChangeSlug(ctx, id, "slug-noop", "mentor", changeCooldown, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "slug-noop", old)
	assert.Empty(t, slugHistory(t, pool, id))

	at, err := repo.LatestMentorSlugChange(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, at, "a no-op must not start the 14-day cooldown")
}

// TestChangeSlugRejectsAnotherMentorsCurrentSlug.
func TestChangeSlugRejectsATakenSlug(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	mine := seedMentor(t, pool, "slug-taken-mine")
	seedMentor(t, pool, "slug-taken-theirs")

	_, err := repo.ChangeSlug(ctx, mine, "slug-taken-theirs", "mentor", changeCooldown, time.Now())
	assert.ErrorIs(t, err, ErrSlugTaken)
	assert.Equal(t, "slug-taken-mine", currentSlug(t, pool, mine))
	assert.Empty(t, slugHistory(t, pool, mine), "a rejected change must roll the whole tx back")
}

// TestChangeSlugRejectsAnotherMentorsRetiredSlug: a redirect someone else owns is
// as unavailable as a live profile — taking it would break their old links.
func TestChangeSlugRejectsAnotherMentorsRetiredSlug(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	theirs := seedMentor(t, pool, "slug-theirs-old")
	mine := seedMentor(t, pool, "slug-mine-current")

	_, err := repo.ChangeSlug(ctx, theirs, "slug-theirs-new", "admin", 0, time.Now())
	require.NoError(t, err)

	_, err = repo.ChangeSlug(ctx, mine, "slug-theirs-old", "mentor", changeCooldown, time.Now())
	assert.ErrorIs(t, err, ErrSlugTaken)
}

// TestChangeSlugReclaimsOwnRetiredSlug: switching back to your own old name must
// consume the history row, or mentors.slug and mentor_slug_history would both
// claim the same string — a profile that is simultaneously live and a redirect.
func TestChangeSlugReclaimsOwnRetiredSlug(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "slug-reclaim-a")

	_, err := repo.ChangeSlug(ctx, id, "slug-reclaim-b", "admin", 0, time.Now())
	require.NoError(t, err)
	require.Equal(t, []string{"slug-reclaim-a"}, slugHistory(t, pool, id))

	_, err = repo.ChangeSlug(ctx, id, "slug-reclaim-a", "admin", 0, time.Now())
	require.NoError(t, err)

	assert.Equal(t, "slug-reclaim-a", currentSlug(t, pool, id))
	assert.Equal(t, []string{"slug-reclaim-b"}, slugHistory(t, pool, id),
		"the reclaimed slug must be gone from history and the previous one retired")
}

// TestChangeSlugEnforcesTheCooldownInsideTheTransaction. The cooldown is read
// under the mentor row lock, which is what closes the TOCTOU gap: a pre-check
// outside the transaction would let two concurrent POSTs both pass and rename.
func TestChangeSlugEnforcesTheCooldown(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "slug-cooldown-a")

	start := time.Now()
	_, err := repo.ChangeSlug(ctx, id, "slug-cooldown-b", "mentor", changeCooldown, start)
	require.NoError(t, err)

	_, err = repo.ChangeSlug(ctx, id, "slug-cooldown-c", "mentor", changeCooldown, start.Add(time.Hour))
	var cooldown *CooldownError
	require.ErrorAs(t, err, &cooldown)
	assert.WithinDuration(t, time.Now().Add(changeCooldown), cooldown.NextChangeAt, time.Minute)
	assert.Equal(t, "slug-cooldown-b", currentSlug(t, pool, id))

	// Past the window it goes through again.
	_, err = repo.ChangeSlug(ctx, id, "slug-cooldown-c", "mentor",
		changeCooldown, time.Now().Add(changeCooldown+time.Minute))
	require.NoError(t, err)
	assert.Equal(t, "slug-cooldown-c", currentSlug(t, pool, id))
}

// TestAdminChangeNeitherConsumesNorResetsTheCooldown: admin renames go through
// the same machinery but must not spend the mentor's own 14-day allowance, and
// must not reset it either.
func TestAdminChangeNeitherConsumesNorResetsTheCooldown(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "slug-admin-a")

	// An admin rename with no cooldown, before the mentor has ever renamed.
	_, err := repo.ChangeSlug(ctx, id, "slug-admin-b", "admin", 0, time.Now())
	require.NoError(t, err)
	at, err := repo.LatestMentorSlugChange(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, at, "an admin rename must not start the mentor's cooldown")

	// The mentor still has their change available.
	_, err = repo.ChangeSlug(ctx, id, "slug-admin-c", "mentor", changeCooldown, time.Now())
	require.NoError(t, err)
	at, err = repo.LatestMentorSlugChange(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, at)
	mentorChangedAt := *at

	// A later admin rename must not push the mentor's timestamp forward, which
	// would silently hand them a fresh 14 days.
	_, err = repo.ChangeSlug(ctx, id, "slug-admin-d", "admin", 0, time.Now())
	require.NoError(t, err)
	at, err = repo.LatestMentorSlugChange(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, at)
	assert.WithinDuration(t, mentorChangedAt, *at, time.Millisecond)
}

// TestChangeSlugTrimsHistoryToTheNewestTwo. The trim orders by created_at, which
// ChangeSlug stamps with clock_timestamp() rather than the column default NOW():
// NOW() is the TRANSACTION start time, so under overlapping renames a
// later-committing transaction could stamp an earlier time and the trim would
// then evict the wrong — still-current — redirect.
func TestChangeSlugTrimsHistoryToTheNewestTwo(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "slug-trim-1")

	for _, slug := range []string{"slug-trim-2", "slug-trim-3", "slug-trim-4"} {
		_, err := repo.ChangeSlug(ctx, id, slug, "admin", 0, time.Now())
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"slug-trim-3", "slug-trim-2"}, slugHistory(t, pool, id))

	// The evicted slug is genuinely freed: another mentor may claim it.
	other := seedMentor(t, pool, "slug-trim-other")
	_, err := repo.ChangeSlug(ctx, other, "slug-trim-1", "admin", 0, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "slug-trim-1", currentSlug(t, pool, other))
}

// TestChangeSlugOfMissingMentor: a rename of a row that is not there fails on the
// FOR UPDATE read, before any advisory lock is taken, and says which mentor.
func TestChangeSlugOfMissingMentor(t *testing.T) {
	repo, _ := updateTestRepo(t)
	_, err := repo.ChangeSlug(context.Background(),
		"00000000-0000-0000-0000-0000000000fe", "slug-nobody", "mentor", changeCooldown, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestConcurrentChangesToTheSameSlugProduceOneWinner is the reason the advisory
// locks exist. Two mentors race for one free slug; the row lock does not help
// (different rows), so serialization comes from lockSlugs plus the UNIQUE
// constraint. Exactly one must win, and the loser must see ErrSlugTaken rather
// than a raw constraint violation.
func TestConcurrentChangesToTheSameSlugProduceOneWinner(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	const contested = "slug-race-contested"

	first := seedMentor(t, pool, "slug-race-first")
	second := seedMentor(t, pool, "slug-race-second")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, id := range []string{first, second} {
		wg.Add(1)
		go func(slot int, mentorID string) {
			defer wg.Done()
			<-start
			_, err := repo.ChangeSlug(ctx, mentorID, contested, "mentor", changeCooldown, time.Now())
			errs[slot] = err
		}(i, id)
	}
	close(start)
	wg.Wait()

	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
			continue
		}
		assert.True(t, errors.Is(err, ErrSlugTaken),
			"the loser must lose with ErrSlugTaken, got %v", err)
	}
	require.Equal(t, 1, winners, "exactly one mentor may hold the slug")

	// And the winner really holds it, with the loser untouched.
	holders := 0
	for _, id := range []string{first, second} {
		if currentSlug(t, pool, id) == contested {
			holders++
		}
	}
	assert.Equal(t, 1, holders)
}

// TestConcurrentRenamesOfOneMentorSerialize: the FOR UPDATE row lock makes the
// second rename read the first one's committed slug_changed_at, so the cooldown
// cannot be raced. Without that, both would pass a check made before the lock.
func TestConcurrentRenamesOfOneMentorSerialize(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "slug-serial-a")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, slug := range []string{"slug-serial-b", "slug-serial-c"} {
		wg.Add(1)
		go func(slot int, newSlug string) {
			defer wg.Done()
			<-start
			_, err := repo.ChangeSlug(ctx, id, newSlug, "mentor", changeCooldown, time.Now())
			errs[slot] = err
		}(i, slug)
	}
	close(start)
	wg.Wait()

	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
			continue
		}
		var cooldown *CooldownError
		assert.True(t, errors.As(err, &cooldown),
			"the losing rename must be refused by the cooldown, got %v", err)
	}
	assert.Equal(t, 1, winners, "the cooldown must let exactly one of two concurrent renames land")
	assert.Len(t, slugHistory(t, pool, id), 1)
}

// TestIsSlugTakenMatchesWhatChangeSlugAccepts keeps the cheap pre-check (used by
// the live availability endpoint) in step with the authoritative check inside the
// transaction. A pre-check that is more permissive shows a green tick and then
// fails the save.
func TestIsSlugTakenMatchesWhatChangeSlugAccepts(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	mine := seedMentor(t, pool, "slug-check-mine")
	theirs := seedMentor(t, pool, "slug-check-theirs")

	_, err := repo.ChangeSlug(ctx, theirs, "slug-check-theirs-new", "admin", 0, time.Now())
	require.NoError(t, err)
	_, err = repo.ChangeSlug(ctx, mine, "slug-check-mine-new", "admin", 0, time.Now())
	require.NoError(t, err)

	cases := []struct {
		slug  string
		taken bool
		why   string
	}{
		{"slug-check-free", false, "nobody has it"},
		{"slug-check-theirs-new", true, "another mentor's current slug"},
		{"slug-check-theirs", true, "another mentor's redirect"},
		{"slug-check-mine", false, "my own redirect: reclaimable"},
		{"slug-check-mine-new", true, "my own current slug"},
	}
	for _, tc := range cases {
		taken, checkErr := repo.IsSlugTaken(ctx, tc.slug, mine)
		require.NoError(t, checkErr)
		assert.Equal(t, tc.taken, taken, "%s (%s)", tc.slug, tc.why)

		// The transaction must agree, except for the mentor's own current slug —
		// which ChangeSlug short-circuits as a no-op rather than rejecting.
		if tc.slug == "slug-check-mine-new" {
			continue
		}
		_, changeErr := repo.ChangeSlug(ctx, mine, tc.slug, "admin", 0, time.Now())
		if tc.taken {
			assert.ErrorIs(t, changeErr, ErrSlugTaken, "%s (%s)", tc.slug, tc.why)
			continue
		}
		require.NoError(t, changeErr, "%s (%s)", tc.slug, tc.why)
		// Put it back so the next case starts from a known slug.
		_, err = repo.ChangeSlug(ctx, mine, "slug-check-mine-new", "admin", 0, time.Now())
		require.NoError(t, err)
	}
}

// TestRegistrationCannotClaimASlugBeingRetired pins what lockSlugs is for: the
// slug a rename is retiring into history must not become another mentor's current
// slug in the meantime, which would leave one string both a live profile and a
// redirect.
func TestRegistrationCannotClaimASlugBeingRetired(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "slug-lock-old")

	_, err := repo.ChangeSlug(ctx, id, "slug-lock-new", "admin", 0, time.Now())
	require.NoError(t, err)

	// The retired slug is now a redirect, so a registration must not take it.
	taken, err := repo.IsSlugTaken(ctx, "slug-lock-old", "")
	require.NoError(t, err)
	assert.True(t, taken)

	entry, err := repo.GetSlugHistoryBySlug(ctx, "slug-lock-old")
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, id, entry.MentorID)
	assert.Equal(t, "admin", entry.ChangedBy)

	// And an unknown slug resolves to nothing rather than an error.
	entry, err = repo.GetSlugHistoryBySlug(ctx, "slug-lock-never-existed")
	require.NoError(t, err)
	assert.Nil(t, entry)
}
