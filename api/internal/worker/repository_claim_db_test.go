package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The finalization claim is pure SQL — its exclusivity comes from Postgres
// re-evaluating the UPDATE's WHERE against the winner's committed row, which no
// fake repository can model. These tests therefore need a real database and skip
// without one; see the dbtest package for how to point them at one.
func testRepository(t *testing.T) *Repository {
	t.Helper()
	return NewRepository(dbtest.Pool(t))
}

// insertDraftRegistration creates the row shape the registration INSERT leaves:
// draft, no sort_order, no confirmation token.
func insertDraftRegistration(t *testing.T, repo *Repository, slug string) string {
	t.Helper()

	var id string
	err := repo.pool.QueryRow(context.Background(), `
		INSERT INTO mentors (slug, name, status, email, created_at)
		VALUES ($1, 'Claim Test', 'draft', $2, NOW() - INTERVAL '1 hour')
		RETURNING id
	`, slug, slug+"@example.com").Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
	})
	return id
}

type storedRegistration struct {
	Status    string
	SortOrder *int
	Token     string
	ExpiresAt *time.Time
}

func readRegistration(t *testing.T, repo *Repository, id string) storedRegistration {
	t.Helper()

	var row storedRegistration
	err := repo.pool.QueryRow(context.Background(), `
		SELECT status, sort_order, COALESCE(email_confirmation_token, ''), email_confirmation_expires_at
		FROM mentors WHERE id = $1
	`, id).Scan(&row.Status, &row.SortOrder, &row.Token, &row.ExpiresAt)
	require.NoError(t, err)
	return row
}

func claimParams(mentorID, token, expectedToken string) FinalizeNewMentorParams {
	expiresAt := time.Now().Add(confirmationTokenTTL)
	return FinalizeNewMentorParams{
		MentorID:                       mentorID,
		Name:                           "Claim Test",
		PreferredContact:               "@claim",
		Slug:                           "claim-test",
		Status:                         mentorStatusDraft,
		SortOrder:                      7,
		EmailConfirmationToken:         &token,
		EmailConfirmationExpiresAt:     &expiresAt,
		ExpectedEmailConfirmationToken: expectedToken,
	}
}

// TestFinalizeNewMentorClaimIsExclusive is the race the ten-minute
// finalize-stuck-registrations cron makes reachable: two passes (or a pass and a
// redelivered POST /jobs/new-mentor-watcher) finalizing the same row. Exactly one
// may report applied, because every caller that reports applied emails the token
// it just wrote — two winners means two links, the older one already dead.
func TestFinalizeNewMentorClaimIsExclusive(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertDraftRegistration(t, repo, "claim-exclusive")

	const callers = 8
	var start sync.WaitGroup
	start.Add(1)
	results := make([]bool, callers)
	tokens := make([]string, callers)

	var done sync.WaitGroup
	for i := range callers {
		tokens[i] = "token-exclusive-" + string(rune('a'+i))
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			// Every caller read the row before any of them wrote it, so they all
			// expect the same (empty) token — the state two overlapping cron
			// passes are in.
			applied, err := repo.FinalizeNewMentor(context.Background(), claimParams(mentorID, tokens[i], ""))
			assert.NoError(t, err)
			results[i] = applied
		}(i)
	}
	start.Done()
	done.Wait()

	winners := 0
	winner := -1
	for i, applied := range results {
		if applied {
			winners++
			winner = i
		}
	}
	require.Equal(t, 1, winners, "exactly one concurrent caller may claim the registration")

	stored := readRegistration(t, repo, mentorID)
	assert.Equal(t, tokens[winner], stored.Token,
		"the stored token must be the one the winning caller emails")
}

// TestFinalizeNewMentorKeepsLiveEmailedToken covers the sequential shape of the
// same bug: a caller that reads the row AFTER a successful claim passes the
// compare-and-swap, and must still not replace a confirmation token whose 24h
// window is open — that link is already in the mentor's inbox.
func TestFinalizeNewMentorKeepsLiveEmailedToken(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertDraftRegistration(t, repo, "claim-live-token")

	applied, err := repo.FinalizeNewMentor(context.Background(), claimParams(mentorID, "token-emailed", ""))
	require.NoError(t, err)
	require.True(t, applied)

	applied, err = repo.FinalizeNewMentor(context.Background(),
		claimParams(mentorID, "token-second", "token-emailed"))
	require.NoError(t, err)
	assert.False(t, applied, "a live confirmation token must not be replaced")
	assert.Equal(t, "token-emailed", readRegistration(t, repo, mentorID).Token)
}

// TestFinalizeNewMentorReplaysExpiredToken pins the other side of that guard: the
// replay's second leg (token issued, 24h window gone) must still be claimable,
// otherwise a mentor whose link never arrived is locked out for good.
func TestFinalizeNewMentorReplaysExpiredToken(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertDraftRegistration(t, repo, "claim-expired-token")

	_, err := repo.pool.Exec(context.Background(), `
		UPDATE mentors SET email_confirmation_token = 'token-expired',
			email_confirmation_expires_at = NOW() - INTERVAL '1 hour', sort_order = 3
		WHERE id = $1
	`, mentorID)
	require.NoError(t, err)

	applied, err := repo.FinalizeNewMentor(context.Background(),
		claimParams(mentorID, "token-reissued", "token-expired"))
	require.NoError(t, err)
	require.True(t, applied, "an expired token must be re-issued")
	assert.Equal(t, "token-reissued", readRegistration(t, repo, mentorID).Token)
}

// TestFinalizeNewMentorClaimRejectsStaleRead is the compare-and-swap on its own:
// a caller whose read predates someone else's write must lose, even once the
// winner's token has expired and the row is claimable again.
func TestFinalizeNewMentorClaimRejectsStaleRead(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertDraftRegistration(t, repo, "claim-stale-read")

	applied, err := repo.FinalizeNewMentor(context.Background(), claimParams(mentorID, "token-winner", ""))
	require.NoError(t, err)
	require.True(t, applied)
	_, err = repo.pool.Exec(context.Background(),
		`UPDATE mentors SET email_confirmation_expires_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, mentorID)
	require.NoError(t, err)

	applied, err = repo.FinalizeNewMentor(context.Background(), claimParams(mentorID, "token-loser", ""))
	require.NoError(t, err)
	assert.False(t, applied, "a claim built on a stale read must not apply")
	assert.Equal(t, "token-winner", readRegistration(t, repo, mentorID).Token)
}

// TestReleaseNewMentorFinalizationIgnoresForeignClaim: a release must only ever
// undo the claim its own caller made. If a send failure could hand the row back
// while it carries someone else's finalization, that mentor's confirmation token
// would be wiped after their link was emailed.
func TestReleaseNewMentorFinalizationIgnoresForeignClaim(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertDraftRegistration(t, repo, "release-foreign-claim")

	applied, err := repo.FinalizeNewMentor(context.Background(), claimParams(mentorID, "token-b", ""))
	require.NoError(t, err)
	require.True(t, applied)

	// Caller A never won the row (its claim was rejected), so its release must
	// find nothing to undo — including when both callers happened to draw the
	// same sort_order.
	foreign := claimParams(mentorID, "token-a", "")
	require.NoError(t, repo.ReleaseNewMentorFinalization(context.Background(), foreign))

	stored := readRegistration(t, repo, mentorID)
	assert.Equal(t, "token-b", stored.Token, "another caller's finalization must survive")
	require.NotNil(t, stored.SortOrder)
	assert.Equal(t, 7, *stored.SortOrder)
}

// TestReleaseNewMentorFinalizationUndoesOwnClaim: the send failed, so the row
// must go back to the shape the INSERT left — that is what puts it back into
// finalize-stuck-registrations' replay set instead of waiting out the 24h TTL.
func TestReleaseNewMentorFinalizationUndoesOwnClaim(t *testing.T) {
	repo := testRepository(t)
	mentorID := insertDraftRegistration(t, repo, "release-own-claim")

	params := claimParams(mentorID, "token-own", "")
	applied, err := repo.FinalizeNewMentor(context.Background(), params)
	require.NoError(t, err)
	require.True(t, applied)

	require.NoError(t, repo.ReleaseNewMentorFinalization(context.Background(), params))

	stored := readRegistration(t, repo, mentorID)
	assert.Equal(t, mentorStatusDraft, stored.Status)
	assert.Empty(t, stored.Token)
	assert.Nil(t, stored.SortOrder)
	assert.Nil(t, stored.ExpiresAt)

	// ...and the released row is claimable again.
	applied, err = repo.FinalizeNewMentor(context.Background(), claimParams(mentorID, "token-next", ""))
	require.NoError(t, err)
	assert.True(t, applied)
}
