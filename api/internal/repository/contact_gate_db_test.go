package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
)

// C3's guarantee is that no client_requests row can exist for a mentor who is not
// accepting requests, and that such a mentor's calendar link is never returned.
// Both come from the INSERT ... SELECT itself — the row is created only if the
// gating SELECT matches — so they can only be tested against a real Postgres, with
// concurrent callers. Skips without a database (see api/test/dbtest).

// seedContactMentor inserts a mentor in a given status, optionally with a
// calendar link, and returns its id.
func seedContactMentor(t *testing.T, pool *pgxpool.Pool, slug, status, calendarURL string) string {
	t.Helper()

	var id string
	require.NoError(t, pool.QueryRow(context.Background(), `
		INSERT INTO mentors (slug, name, status, calendar_url)
		VALUES ($1, 'Contact Gate Mentor', $2, NULLIF($3, ''))
		RETURNING id
	`, slug, status, calendarURL).Scan(&id))
	t.Cleanup(func() {
		// client_requests cascade from the mentor.
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
	})
	return id
}

func contactFixture(mentorID string) *models.ClientRequest {
	return &models.ClientRequest{
		MentorID:         mentorID,
		Email:            "mentee@example.test",
		Name:             "Mentee",
		Level:            "junior",
		Description:      "I would like an hour of your time",
		PreferredContact: "telegram",
	}
}

func countRequests(t *testing.T, pool *pgxpool.Pool, mentorID string) int {
	t.Helper()
	var rows int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM client_requests WHERE mentor_id = $1`, mentorID).Scan(&rows))
	return rows
}

// TestCreateOnlyAcceptsAnActiveMentor is the C3 defect in test form: the request
// row used to be inserted BEFORE the mentor was fetched, so a draft, pending or
// declined mentor could be contacted through the API even though no page offers
// the form — and the mentee got a confirmation for a request nobody would answer.
func TestCreateOnlyAcceptsAnActiveMentor(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	ctx := context.Background()

	for _, status := range []string{"draft", "pending", "declined", "inactive"} {
		mentorID := seedContactMentor(t, pool,
			"contact-gate-"+status, status, "https://cal.example/private")

		created, err := repo.Create(ctx, contactFixture(mentorID))
		require.ErrorIs(t, err, ErrMentorNotContactable, "status %q must be refused", status)
		assert.Nil(t, created)
		assert.Equal(t, 0, countRequests(t, pool, mentorID),
			"status %q: a refused submission must leave no row", status)
	}

	active := seedContactMentor(t, pool,
		"contact-gate-active", "active", "https://cal.example/public")
	created, err := repo.Create(ctx, contactFixture(active))
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "https://cal.example/public", created.CalendarURL,
		"an active mentor's calendar link is the whole point of the successful path")
	assert.Equal(t, 1, countRequests(t, pool, active))
}

// TestCreateRefusesADeletedMentor: deletion (D70) leaves status 'inactive' today,
// so the status check happens to cover it — but that is a consequence of how
// deletion is implemented, not a rule about deletion. State the rule.
func TestCreateRefusesADeletedMentor(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	mentors := NewMentorRepository(pool, nil)
	ctx := context.Background()

	mentorID := seedContactMentor(t, pool,
		"contact-gate-deleted", "active", "https://cal.example/gone")
	_, err := mentors.SoftDeleteMentor(ctx, mentorID)
	require.NoError(t, err)
	// Deletion also sets status; put it back to prove deleted_at alone refuses.
	_, err = pool.Exec(ctx, `UPDATE mentors SET status = 'active' WHERE id = $1`, mentorID)
	require.NoError(t, err)

	created, err := repo.Create(ctx, contactFixture(mentorID))
	require.ErrorIs(t, err, ErrMentorNotContactable)
	assert.Nil(t, created)
	assert.Equal(t, 0, countRequests(t, pool, mentorID))
}

// TestCreateGateHoldsUnderConcurrentDeactivation runs submissions against the
// mentor hiding themselves. The invariant a sequential test cannot show: the gate
// is INSIDE the write, so the row count matches the number of successes exactly,
// and nothing is created once the deactivation has committed — where the pre-C3
// code inserted first and asked afterwards, which created rows either way.
func TestCreateGateHoldsUnderConcurrentDeactivation(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	ctx := context.Background()
	mentorID := seedContactMentor(t, pool, "contact-gate-race", "active", "")

	const callers = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]*CreatedClientRequest, callers)
	errs := make([]error, callers)
	var deactivatedAt time.Time

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		if _, err := pool.Exec(ctx,
			`UPDATE mentors SET status = 'inactive' WHERE id = $1`, mentorID); err != nil {
			t.Errorf("deactivate: %v", err)
			return
		}
		// Read the clock AFTER the UPDATE committed: every row created by a
		// statement that still saw 'active' carries an earlier created_at.
		if err := pool.QueryRow(ctx, `SELECT now()`).Scan(&deactivatedAt); err != nil {
			t.Errorf("read clock: %v", err)
		}
	}()

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = repo.Create(ctx, contactFixture(mentorID))
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for i, err := range errs {
		switch {
		case err == nil:
			successes++
			require.NotNil(t, results[i])
			assert.NotEmpty(t, results[i].ID)
		case assert.ErrorIs(t, err, ErrMentorNotContactable, "caller %d", i):
			assert.Nil(t, results[i], "a refused caller must get no request back")
		}
	}

	assert.Equal(t, successes, countRequests(t, pool, mentorID),
		"every row in the table must be accounted for by a successful caller")

	require.False(t, deactivatedAt.IsZero())
	var createdAfter int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM client_requests
		WHERE mentor_id = $1 AND created_at > $2
	`, mentorID, deactivatedAt).Scan(&createdAfter))
	assert.Equal(t, 0, createdAfter,
		"no request may be created after the mentor stopped accepting them")

	// And deterministically, now that the deactivation is committed:
	created, err := repo.Create(ctx, contactFixture(mentorID))
	require.ErrorIs(t, err, ErrMentorNotContactable)
	assert.Nil(t, created)
}

// TestUpdateStatusAppliesExactlyOnce: two mentor tabs, or a mentor and an admin,
// each validate a transition against the 'pending' they read. Only the first write
// may land — the second would overwrite a transition it never saw and re-stamp
// status_changed_at, which is what the reminder jobs age requests by.
func TestUpdateStatusAppliesExactlyOnce(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	mentorID := seedMentor(t, pool, "requests-cas-status")
	id := seedRequest(t, pool, mentorID, "pending")

	const callers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	applied := make([]bool, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			var err error
			applied[i], err = repo.UpdateStatus(context.Background(), id, "pending", "working")
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for _, ok := range applied {
		if ok {
			winners++
		}
	}
	assert.Equal(t, 1, winners, "exactly one caller may make the transition")

	got, err := repo.GetByID(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, models.RequestStatus("working"), got.Status)
}

// TestUpdateDeclineLosesToAConcurrentTransition: the decline and the "mark as
// working" both looked at the same 'pending' row. Whichever writes first wins, and
// the loser must report that it changed nothing rather than clobbering the winner.
func TestUpdateDeclineLosesToAConcurrentTransition(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	ctx := context.Background()

	// Repeated so both orderings get exercised; the assertion is the same either
	// way, which is the point.
	for round := 0; round < 8; round++ {
		mentorID := seedMentor(t, pool, fmt.Sprintf("requests-cas-decline-%d", round))
		id := seedRequest(t, pool, mentorID, "pending")

		var wg sync.WaitGroup
		start := make(chan struct{})
		var declineApplied, statusApplied bool
		var declineErr, statusErr error

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			declineApplied, declineErr = repo.UpdateDecline(ctx, id, "pending",
				models.DeclineTopicMismatch, "not my field")
		}()
		go func() {
			defer wg.Done()
			<-start
			statusApplied, statusErr = repo.UpdateStatus(ctx, id, "pending", "working")
		}()
		close(start)
		wg.Wait()

		require.NoError(t, declineErr)
		require.NoError(t, statusErr)
		assert.False(t, declineApplied && statusApplied,
			"round %d: both writers cannot have moved the same pending request", round)
		require.True(t, declineApplied || statusApplied, "round %d: one must win", round)

		got, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		if declineApplied {
			assert.Equal(t, models.RequestStatus("declined"), got.Status, "round %d", round)
		} else {
			assert.Equal(t, models.RequestStatus("working"), got.Status, "round %d", round)
			assert.Empty(t, got.DeclineReason,
				"round %d: a decline that did not apply must not have written its reason", round)
		}
	}
}
