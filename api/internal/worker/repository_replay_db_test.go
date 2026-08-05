package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The replay guards on the worker's writes are pure SQL: their exclusivity comes
// from Postgres re-evaluating each UPDATE's WHERE against the winning writer's
// committed row, and a fake repository cannot model that. These tests therefore
// need a real database and skip without one (see the dbtest package, and
// testRepository in repository_claim_db_test.go).

// insertMentor creates a mentor row in the given status. Slug is unique per test
// so the tests can run against a shared database.
func insertMentor(t *testing.T, repo *Repository, slug, status string) string {
	t.Helper()

	var id string
	err := repo.pool.QueryRow(context.Background(), `
		INSERT INTO mentors (slug, name, status, email)
		VALUES ($1, 'Replay Test', $2, $3)
		RETURNING id
	`, slug, status, slug+"@example.com").Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
	})
	return id
}

// insertClientRequest creates a client request in the shape the API's INSERT
// leaves it: status 'pending', status_changed_at NULL.
func insertClientRequest(t *testing.T, repo *Repository, mentorID string) string {
	t.Helper()

	var id string
	err := repo.pool.QueryRow(context.Background(), `
		INSERT INTO client_requests (mentor_id, email, name, preferred_contact, description, level, status)
		VALUES ($1, 'mentee@example.com', 'Jane Mentee', ' @jane ', 'help me', 'Junior', 'pending')
		RETURNING id
	`, mentorID).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(context.Background(), `DELETE FROM client_requests WHERE id = $1`, id)
	})
	return id
}

type storedRequest struct {
	Status          string
	Contact         string
	StatusChangedAt *time.Time
	UpdatedAt       time.Time
}

func readRequest(t *testing.T, repo *Repository, id string) storedRequest {
	t.Helper()

	var row storedRequest
	err := repo.pool.QueryRow(context.Background(), `
		SELECT status, COALESCE(preferred_contact, ''), status_changed_at, updated_at
		FROM client_requests WHERE id = $1
	`, id).Scan(&row.Status, &row.Contact, &row.StatusChangedAt, &row.UpdatedAt)
	require.NoError(t, err)
	return row
}

// TestSetRequestContactPendingClaimIsExclusive: the API dispatches
// new-request-watcher through a detached goroutine, and the endpoint is also
// manually triggerable, so the same request can be processed twice at once.
// Exactly one caller may report applied, because every caller that does emails the
// mentee, the mentor and the moderators about a brand-new request.
func TestSetRequestContactPendingClaimIsExclusive(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertMentor(t, repo, "replay-request-exclusive", mentorStatusActive)
	requestID := insertClientRequest(t, repo, mentorID)

	const callers = 8
	var start sync.WaitGroup
	start.Add(1)
	results := make([]bool, callers)

	var done sync.WaitGroup
	for i := range callers {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			applied, err := repo.SetRequestContactPending(context.Background(), requestID, "@jane")
			assert.NoError(t, err)
			results[i] = applied
		}(i)
	}
	start.Done()
	done.Wait()

	winners := 0
	for _, applied := range results {
		if applied {
			winners++
		}
	}
	require.Equal(t, 1, winners, "exactly one concurrent caller may claim the request")

	stored := readRequest(t, repo, requestID)
	assert.Equal(t, "pending", stored.Status)
	assert.Equal(t, "@jane", stored.Contact)
	require.NotNil(t, stored.StatusChangedAt, "the claim must leave a timestamp trace")
}

// TestSetRequestContactPendingRefusesAdvancedRequest is the H3 defect itself: the
// unguarded UPDATE set status = 'pending' unconditionally, so a replayed callback
// silently REOPENED a request the mentor had already declined or completed, while
// leaving status_changed_at on the older transition — so the reopened request also
// read as long-stale to the reminder jobs. updated_at is asserted to stand still
// too: trg_client_requests_updated_at bumps it on any UPDATE that matches the row,
// which is the one trace the old write did leave.
func TestSetRequestContactPendingRefusesAdvancedRequest(t *testing.T) {
	for _, status := range []string{"declined", "done", "contacted"} {
		t.Run(status, func(t *testing.T) {
			repo := testRepository(t)
			mentorID := insertMentor(t, repo, "replay-request-advanced-"+status, mentorStatusActive)
			requestID := insertClientRequest(t, repo, mentorID)

			// The mentor moved the request on, the way the API's UpdateStatus /
			// UpdateDecline do (both always stamp status_changed_at).
			_, err := repo.pool.Exec(context.Background(), `
				UPDATE client_requests SET status = $1, status_changed_at = NOW() - INTERVAL '2 days'
				WHERE id = $2
			`, status, requestID)
			require.NoError(t, err)
			before := readRequest(t, repo, requestID)

			applied, err := repo.SetRequestContactPending(context.Background(), requestID, "@replayed")
			require.NoError(t, err)
			assert.False(t, applied, "a request that has advanced must not be reclaimed")

			after := readRequest(t, repo, requestID)
			assert.Equal(t, status, after.Status, "a replayed callback must not reopen the request")
			assert.Equal(t, before.Contact, after.Contact)
			assert.Equal(t, before.StatusChangedAt.UTC(), after.StatusChangedAt.UTC())
			assert.Equal(t, before.UpdatedAt.UTC(), after.UpdatedAt.UTC())
		})
	}
}

// TestSetRequestContactPendingRefusesSecondDelivery: a redelivered callback on a
// request that is STILL pending (nobody moved it) must also write nothing, or the
// three announcement emails go out twice.
func TestSetRequestContactPendingRefusesSecondDelivery(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertMentor(t, repo, "replay-request-second", mentorStatusActive)
	requestID := insertClientRequest(t, repo, mentorID)

	applied, err := repo.SetRequestContactPending(context.Background(), requestID, "@first")
	require.NoError(t, err)
	require.True(t, applied)
	first := readRequest(t, repo, requestID)

	applied, err = repo.SetRequestContactPending(context.Background(), requestID, "@second")
	require.NoError(t, err)
	assert.False(t, applied, "the request is already claimed")

	after := readRequest(t, repo, requestID)
	assert.Equal(t, "@first", after.Contact)
	assert.Equal(t, first.StatusChangedAt.UTC(), after.StatusChangedAt.UTC())
}

func readMentorStatus(t *testing.T, repo *Repository, id string) (status string, updatedAt time.Time) {
	t.Helper()

	err := repo.pool.QueryRow(context.Background(),
		`SELECT status, updated_at FROM mentors WHERE id = $1`, id).Scan(&status, &updatedAt)
	require.NoError(t, err)
	return status, updatedAt
}

// TestDeactivateMentorClaimIsExclusive: two overlapping deactivate-pending-mentors
// passes (or a pass racing the manual POST /jobs/cron/deactivate-pending-mentors)
// must deactivate a mentor once, because the winner emails them about it.
func TestDeactivateMentorClaimIsExclusive(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertMentor(t, repo, "replay-deactivate-exclusive", mentorStatusActive)

	const callers = 8
	var start sync.WaitGroup
	start.Add(1)
	results := make([]bool, callers)

	var done sync.WaitGroup
	for i := range callers {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			applied, err := repo.DeactivateMentor(context.Background(), mentorID)
			assert.NoError(t, err)
			results[i] = applied
		}(i)
	}
	start.Done()
	done.Wait()

	winners := 0
	for _, applied := range results {
		if applied {
			winners++
		}
	}
	require.Equal(t, 1, winners, "exactly one concurrent caller may deactivate the mentor")

	status, _ := readMentorStatus(t, repo, mentorID)
	assert.Equal(t, "inactive", status)
}

// TestDeactivateMentorRefusesMentorThatLeftActive: the write used to be
// unconditional, so a mentor who was declined (or returned to draft) between
// ListMentorsToDeactivate and the write was pulled sideways into 'inactive' — a
// declined mentor silently becoming a deactivated one, and emailed as much.
//
// updated_at is asserted to stand still because trg_mentors_updated_at bumps it on
// any UPDATE that touches the row: before the guard, a no-op replay still moved
// the mentor's modification timestamp.
func TestDeactivateMentorRefusesMentorThatLeftActive(t *testing.T) {
	for _, status := range []string{mentorStatusDeclined, mentorStatusDraft, "inactive"} {
		t.Run(status, func(t *testing.T) {
			repo := testRepository(t)
			mentorID := insertMentor(t, repo, "replay-deactivate-"+status, status)
			_, before := readMentorStatus(t, repo, mentorID)

			applied, err := repo.DeactivateMentor(context.Background(), mentorID)
			require.NoError(t, err)
			assert.False(t, applied, "only an active mentor may be deactivated")

			after, afterUpdated := readMentorStatus(t, repo, mentorID)
			assert.Equal(t, status, after)
			assert.Equal(t, before.UTC(), afterUpdated.UTC(), "a no-op must not bump updated_at")
		})
	}
}

// TestCountActiveMentorsByEmailExcludesTheRowBeingProcessed: replaying
// new-mentor-watcher on an already-active mentor used to count that mentor as
// their own duplicate, which is how the job concluded "duplicate" and headed for
// 'declined' against a live profile.
func TestCountActiveMentorsByEmailExcludesTheRowBeingProcessed(t *testing.T) {
	repo := testRepository(t)
	activeID := insertMentor(t, repo, "replay-duplicate-self", mentorStatusActive)

	var email string
	require.NoError(t, repo.pool.QueryRow(context.Background(),
		`SELECT email::text FROM mentors WHERE id = $1`, activeID).Scan(&email))

	count, err := repo.CountActiveMentorsByEmail(context.Background(), email, activeID)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "a mentor is not their own duplicate")

	// A genuine second registration on the same email still counts the live one.
	otherID := insertMentor(t, repo, "replay-duplicate-other", mentorStatusDraft)
	_, err = repo.pool.Exec(context.Background(),
		`UPDATE mentors SET email = $1 WHERE id = $2`, email, otherID)
	require.NoError(t, err)

	count, err = repo.CountActiveMentorsByEmail(context.Background(), email, otherID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "the live profile is a duplicate of the new registration")
}
