package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/reviewtoken"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
)

// The mentee dashboard's guarantees (D95) are SQL properties — the EXISTS gate on
// mentee creation, single-use login tokens, the email join that decides whose
// requests are listed, and one review per request — so they are proved against a
// real Postgres. Skips without one (see api/test/dbtest).

// seedMenteeRequest creates a mentor and one request from email in the given
// status. The mentor is returned so a test can delete or alter it.
func seedMenteeRequest(t *testing.T, pool *pgxpool.Pool, slug, email, status string) (mentorID, requestID string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO mentors (slug, name, status, email, sort_order, calendar_url)
		VALUES ($1, 'Mentee Test Mentor', 'active', $2, 1, 'https://cal.example.com/'||$1)
		RETURNING id
	`, slug, slug+"@example.com").Scan(&mentorID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO client_requests (mentor_id, email, name, description, status)
		VALUES ($1, $2, 'Jane Mentee', 'help me grow', $3)
		RETURNING id
	`, mentorID, email, status).Scan(&requestID))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM client_requests WHERE id = $1`, requestID)
		_, _ = pool.Exec(ctx, `DELETE FROM mentors WHERE id = $1`, mentorID)
		_, _ = pool.Exec(ctx, `DELETE FROM mentees WHERE email = $1::citext`, email)
	})
	return mentorID, requestID
}

func loginMentee(t *testing.T, repo *MenteeRepository, email string) string {
	t.Helper()
	id, err := repo.SetLoginToken(context.Background(), email, "etk_"+email, time.Now().Add(15*time.Minute))
	require.NoError(t, err)
	return id
}

func TestMenteeLoginNeedsARequest(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewMenteeRepository(pool)
	ctx := context.Background()

	_, err := repo.SetLoginToken(ctx, "nobody-mentee@example.com", "etk_x", time.Now().Add(time.Minute))
	require.ErrorIs(t, err, ErrMenteeHasNoRequests)
	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM mentees WHERE email = 'nobody-mentee@example.com'`).Scan(&rows))
	assert.Zero(t, rows, "no mentee row may be created for an address that sent no request")

	seedMenteeRequest(t, pool, "mentee-login-gate", "Gate.Mentee@Example.com", "pending")

	// citext: the address is matched case-insensitively, and a second request
	// reuses the row rather than creating another.
	first := loginMentee(t, repo, "gate.mentee@example.com")
	second := loginMentee(t, repo, "GATE.MENTEE@EXAMPLE.COM")
	assert.Equal(t, first, second)

	var stored string
	require.NoError(t, pool.QueryRow(ctx, `SELECT login_token FROM mentees WHERE id = $1`, first).Scan(&stored))
	assert.Equal(t, HashOneTimeToken("etk_GATE.MENTEE@EXAMPLE.COM"), stored, "only the hash of the latest token is stored")
}

func TestMenteeLoginTokenIsSingleUseUnderConcurrency(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewMenteeRepository(pool)
	seedMenteeRequest(t, pool, "mentee-login-race", "race-mentee@example.com", "pending")
	menteeID := loginMentee(t, repo, "race-mentee@example.com")

	const callers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			var consumed *ConsumedMenteeLogin
			consumed, errs[i] = repo.ConsumeLoginToken(context.Background(), "etk_race-mentee@example.com")
			if errs[i] == nil && consumed.ID != menteeID {
				errs[i] = fmt.Errorf("consumed the wrong mentee %s", consumed.ID)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for i, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrTokenNotConsumable):
		default:
			t.Errorf("caller %d: %v", i, err)
		}
	}
	assert.Equal(t, 1, wins, "one sign-in link mints exactly one session")
}

func TestMenteeLoginTokenExpires(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewMenteeRepository(pool)
	seedMenteeRequest(t, pool, "mentee-login-expiry", "expiry-mentee@example.com", "pending")

	_, err := repo.SetLoginToken(context.Background(), "expiry-mentee@example.com", "etk_old", time.Now().Add(-time.Second))
	require.NoError(t, err)
	_, err = repo.ConsumeLoginToken(context.Background(), "etk_old")
	require.ErrorIs(t, err, ErrTokenNotConsumable)
}

func TestMenteeSessionRevocation(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewMenteeRepository(pool)
	seedMenteeRequest(t, pool, "mentee-revoke", "revoke-mentee@example.com", "pending")
	menteeID := loginMentee(t, repo, "revoke-mentee@example.com")
	ctx := context.Background()

	before, err := repo.MenteeSessionState(ctx, menteeID)
	require.NoError(t, err)
	require.NoError(t, repo.BumpMenteeSessionVersion(ctx, menteeID))
	after, err := repo.MenteeSessionState(ctx, menteeID)
	require.NoError(t, err)
	assert.Equal(t, before+1, after)

	_, err = repo.MenteeSessionState(ctx, "00000000-0000-4000-8000-000000000000")
	require.ErrorIs(t, err, ErrSessionSubjectGone)
}

func TestMenteeSeesOnlyTheirOwnRequests(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewMenteeRepository(pool)
	ctx := context.Background()

	_, mine := seedMenteeRequest(t, pool, "mentee-own-a", "own-mentee@example.com", "pending")
	deletedMentor, minePast := seedMenteeRequest(t, pool, "mentee-own-b", "OWN-mentee@example.com", "done")
	_, theirs := seedMenteeRequest(t, pool, "mentee-own-c", "other-mentee@example.com", "pending")

	// A request whose mentor deleted their profile (D70) is still the mentee's
	// history, but must not expose the mentor.
	_, err := pool.Exec(ctx, `UPDATE mentors SET deleted_at = now() WHERE id = $1`, deletedMentor)
	require.NoError(t, err)

	menteeID := loginMentee(t, repo, "own-mentee@example.com")
	rows, err := repo.ListRequests(ctx, menteeID)
	require.NoError(t, err)

	ids := map[string]MenteeRequestRow{}
	for _, row := range rows {
		ids[row.ID] = row
	}
	require.Len(t, ids, 2)
	assert.Contains(t, ids, mine)
	assert.Contains(t, ids, minePast)
	assert.NotContains(t, ids, theirs, "another email's request must never be listed")

	assert.True(t, ids[mine].MentorAvailable)
	assert.Equal(t, "https://cal.example.com/mentee-own-a", ids[mine].MentorCalendarURL)
	assert.False(t, ids[minePast].MentorAvailable)
}

// TestEveryNullableClientRequestColumnStaysReadableForMentee is the D50 rule for
// ListRequests: a NULL in any client_requests column must not fail the scan and
// empty the mentee's whole dashboard.
func TestEveryNullableClientRequestColumnStaysReadableForMentee(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewMenteeRepository(pool)
	columns := dbtest.NullableColumns(t, pool, "client_requests")
	ctx := context.Background()
	const email = "nullscan-mentee@example.com"

	for i, column := range columns {
		t.Run(column.Name, func(t *testing.T) {
			suffix := fmt.Sprintf("mentee%s-%d", strings.ReplaceAll(column.Name, "_", ""), i)
			_, requestID := seedFullClientRequest(t, pool, columns, suffix)
			_, err := pool.Exec(ctx, `UPDATE client_requests SET email = $1 WHERE id = $2`, email, requestID)
			require.NoError(t, err)
			menteeID := loginMentee(t, repo, email)
			t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM mentees WHERE id = $1`, menteeID) })

			dbtest.SetNull(t, pool, "client_requests", column.Name, requestID)

			rows, err := repo.ListRequests(ctx, menteeID)
			require.NoError(t, err, "ListRequests with %s NULL", column.Name)
			if column.Name == "email" {
				return // a request with no email belongs to no mentee
			}
			found := false
			for _, row := range rows {
				found = found || row.ID == requestID
			}
			assert.True(t, found, "ListRequests dropped the request with %s NULL", column.Name)
		})
	}
}

func TestMenteeReviewIsOwnedAndSingle(t *testing.T) {
	pool := dbtest.Pool(t)
	menteeRepo := NewMenteeRepository(pool)
	reviews := NewReviewRepository(pool)
	ctx := context.Background()

	_, requestID := seedMenteeRequest(t, pool, "mentee-review", "review-mentee@example.com", "done")
	seedMenteeRequest(t, pool, "mentee-review-other", "intruder-mentee@example.com", "pending")
	menteeID := loginMentee(t, menteeRepo, "review-mentee@example.com")
	intruderID := loginMentee(t, menteeRepo, "intruder-mentee@example.com")

	// An emailed invitation is outstanding; a dashboard review must spend it.
	raw := issueInvitation(t, reviews, requestID, reviewtoken.TTL)

	_, err := reviews.SubmitReviewAsMentee(ctx, intruderID, requestID, reviewContentFixture())
	require.ErrorIs(t, err, ErrReviewRequestNotOwned, "a mentee may not review a request sent from another email")

	const callers = 6
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = reviews.SubmitReviewAsMentee(context.Background(), menteeID, requestID, reviewContentFixture())
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for i, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrReviewIneligible):
		default:
			t.Errorf("caller %d got an untyped error: %v", i, err)
		}
	}
	assert.Equal(t, 1, wins, "exactly one review per request")

	_, err = reviews.SubmitReviewWithToken(ctx, reviewtoken.Hash(raw), reviewContentFixture())
	require.ErrorIs(t, err, ErrReviewTokenInvalid, "the emailed link is spent by the dashboard review")
}

func TestMenteeCannotReviewBeforeDone(t *testing.T) {
	pool := dbtest.Pool(t)
	menteeRepo := NewMenteeRepository(pool)
	reviews := NewReviewRepository(pool)

	_, requestID := seedMenteeRequest(t, pool, "mentee-review-early", "early-mentee@example.com", "working")
	menteeID := loginMentee(t, menteeRepo, "early-mentee@example.com")

	_, err := reviews.SubmitReviewAsMentee(context.Background(), menteeID, requestID, reviewContentFixture())
	require.ErrorIs(t, err, ErrReviewIneligible)
}
