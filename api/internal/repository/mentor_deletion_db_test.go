package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/cache"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/require"
)

// Profile deletion is enforced in SQL — `deleted_at IS NULL` predicates spread
// across the mentor reads, plus one transaction that marks the row and tears
// down everything acting on the mentor's behalf. None of that is reachable by a
// fake repository, so it goes untested until a real database is in front of it.
//
// Requires a database (see the dbtest package docs); skips without one.

const deletionTestToken = "deletion-lockout-test-token"

func deletionTestRepo(t *testing.T, pool *pgxpool.Pool) *MentorRepository {
	t.Helper()
	return NewMentorRepository(pool, cache.NewTagsCache(
		func(context.Context) (map[string]string, error) { return map[string]string{}, nil },
	))
}

// seedLiveMentor inserts an active, logged-in mentor with an email and a live
// magic link — everything deletion is supposed to take away.
func seedLiveMentor(t *testing.T, pool *pgxpool.Pool, suffix string) (id, mentorSlug, email string) {
	t.Helper()
	ctx := context.Background()

	mentorSlug = "deletion-" + suffix
	email = "deletion-" + suffix + "@example.com"
	err := pool.QueryRow(ctx, `
		INSERT INTO mentors (slug, name, status, email, login_token, login_token_expires_at)
		VALUES ($1, 'Deletion Test', 'active', $2, $3, now() + interval '1 hour')
		RETURNING id
	`, mentorSlug, email, HashOneTimeToken(deletionTestToken)).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
	})
	return id, mentorSlug, email
}

// seedDoneRequest inserts a completed request for the mentor and removes it
// afterwards. The mentor cleanup does NOT cover it: client_requests.mentor_id is
// ON DELETE SET NULL, so dropping the mentor orphans the request rather than
// deleting it — the very asymmetry PurgeProfile exists to handle — and its
// review_invitations would survive to collide with the next run's token hashes.
func seedDoneRequest(t *testing.T, pool *pgxpool.Pool, mentorID, email string) string {
	t.Helper()
	var requestID string
	require.NoError(t, pool.QueryRow(context.Background(), `
		INSERT INTO client_requests (mentor_id, name, email, status)
		VALUES ($1, 'Mentee', $2, 'done')
		RETURNING id
	`, mentorID, email).Scan(&requestID))
	t.Cleanup(func() {
		// Cascades to reviews and review_invitations.
		_, _ = pool.Exec(context.Background(), `DELETE FROM client_requests WHERE id = $1`, requestID)
	})
	return requestID
}

func TestSoftDeleteLocksTheProfileOutOfEveryReadPath(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := deletionTestRepo(t, pool)
	ctx := context.Background()

	id, mentorSlug, email := seedLiveMentor(t, pool, "lockout")

	// Precondition: every path can see the live profile.
	_, err := repo.GetByEmail(ctx, email)
	require.NoError(t, err, "the fixture must be reachable before it is deleted")
	_, err = repo.GetBySlug(ctx, mentorSlug, models.FilterOptions{})
	require.NoError(t, err)

	_, err = repo.SoftDeleteMentor(ctx, id)
	require.NoError(t, err)

	t.Run("no login link is ever generated", func(t *testing.T) {
		// GetByEmail returning nothing is what stops RequestLogin minting and
		// mailing a magic link — the mentor is never told the account exists.
		_, err := repo.GetByEmail(ctx, email)
		require.Error(t, err)
		require.True(t, IsNoRows(err), "expected a clean no-rows, got %v", err)
	})

	t.Run("a link issued before the delete is dead", func(t *testing.T) {
		_, err := repo.ConsumeLoginToken(ctx, deletionTestToken)
		require.ErrorIs(t, err, ErrTokenNotConsumable)
	})

	t.Run("the session middleware sees a status no session may hold", func(t *testing.T) {
		status, _, err := repo.MentorSessionState(ctx, id)
		require.NoError(t, err)
		require.False(t, models.MayHoldMentorSession(status),
			"status %q must not be allowed to hold a session", status)
	})

	t.Run("the public page 404s", func(t *testing.T) {
		_, err := repo.GetBySlug(ctx, mentorSlug, models.FilterOptions{})
		require.Error(t, err)
	})

	t.Run("the owner cannot reach it either", func(t *testing.T) {
		// AllowAnyStatus is what lets a draft mentor open their own profile; it
		// must NOT be a way back into a deleted one.
		_, err := repo.GetByMentorId(ctx, id,
			models.FilterOptions{ShowHidden: true, AllowAnyStatus: true})
		require.Error(t, err)
	})

	t.Run("an admin read still sees it", func(t *testing.T) {
		mentor, err := repo.GetByMentorId(ctx, id,
			models.FilterOptions{ShowHidden: true, AllowAnyStatus: true, IncludeDeleted: true})
		require.NoError(t, err)
		require.NotNil(t, mentor.DeletedAt)

		details, err := repo.GetForModerationByID(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, details.DeletedAt)
	})

	t.Run("it leaves the status tabs and appears in the deleted one", func(t *testing.T) {
		approved, err := repo.ListForModeration(ctx, []string{"active", "inactive"})
		require.NoError(t, err)
		require.False(t, containsMentor(approved, id), "a deleted profile must not sit in a status tab")

		deleted, err := repo.ListDeletedForModeration(ctx)
		require.NoError(t, err)
		require.True(t, containsMentor(deleted, id))
	})
}

// Deletion must be exactly-once: the compare-and-swap is what stops a
// double-submitted dialog revoking a second round of review invitations.
func TestSoftDeleteIsExactlyOnce(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := deletionTestRepo(t, pool)
	ctx := context.Background()

	id, _, _ := seedLiveMentor(t, pool, "exactlyonce")

	_, err := repo.SoftDeleteMentor(ctx, id)
	require.NoError(t, err)

	_, err = repo.SoftDeleteMentor(ctx, id)
	require.ErrorIs(t, err, ErrMentorAlreadyDeleted)

	// A mentor that never existed is a different answer from one already gone.
	_, err = repo.SoftDeleteMentor(ctx, "00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, models.ErrSessionSubjectGone)
}

// Every moderation write carries `deleted_at IS NULL`, so a deleted profile is
// inert at the SQL level and not only behind the service's checks.
func TestDeletedProfileRefusesEveryWriteButRestore(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := deletionTestRepo(t, pool)
	ctx := context.Background()

	id, _, _ := seedLiveMentor(t, pool, "inert")
	_, err := repo.SoftDeleteMentor(ctx, id)
	require.NoError(t, err)

	require.Error(t, repo.SetMentorStatus(ctx, id, "active"))
	require.Error(t, repo.ApproveMentorModeration(ctx, id))
	require.Error(t, repo.ReturnMentorToDraft(ctx, id, "please fix your photo"))
	_, err = repo.ChangeSlug(ctx, id, "deletion-renamed", "admin", 0, time.Now())
	require.Error(t, err, "a rename would mint a history redirect to a page that 404s")

	// Update reports no error (an UPDATE matching nothing is not a failure), but
	// it must not have written anything.
	require.NoError(t, repo.Update(ctx, id, map[string]interface{}{"name": "Renamed By Admin"}))
	var name string
	require.NoError(t, pool.QueryRow(ctx, `SELECT name FROM mentors WHERE id = $1`, id).Scan(&name))
	require.Equal(t, "Deletion Test", name, "a deleted profile must not accept edits")
}

func TestRestoreBringsTheProfileBackInactive(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := deletionTestRepo(t, pool)
	ctx := context.Background()

	id, mentorSlug, email := seedLiveMentor(t, pool, "restore")
	_, err := repo.SoftDeleteMentor(ctx, id)
	require.NoError(t, err)

	require.NoError(t, repo.RestoreMentor(ctx, id))

	mentor, err := repo.GetByEmail(ctx, email)
	require.NoError(t, err, "a restored profile can log in again")
	require.Equal(t, "inactive", mentor.Status,
		"restore must never republish; the profile was off the site while deleted")
	require.Nil(t, mentor.DeletedAt)

	// Inactive, so it is reachable by link but not in the catalog.
	_, err = repo.GetBySlug(ctx, mentorSlug, models.FilterOptions{})
	require.NoError(t, err)

	// Restoring something that is not deleted is a no-op the caller must hear
	// about — it means the admin is acting on stale state.
	require.ErrorIs(t, repo.RestoreMentor(ctx, id), ErrMentorNotDeleted)
}

// The mentor's session must not outlive the deletion by the token's remaining
// 24 hours, and their magic link must not be spendable afterwards.
func TestSoftDeleteRevokesSessionsAndClearsTheLoginToken(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := deletionTestRepo(t, pool)
	ctx := context.Background()

	id, _, _ := seedLiveMentor(t, pool, "revoke")
	_, versionBefore, err := repo.MentorSessionState(ctx, id)
	require.NoError(t, err)

	_, err = repo.SoftDeleteMentor(ctx, id)
	require.NoError(t, err)

	_, versionAfter, err := repo.MentorSessionState(ctx, id)
	require.NoError(t, err)
	require.Greater(t, versionAfter, versionBefore,
		"session_version must advance or every already-issued cookie stays valid")

	var token *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT login_token FROM mentors WHERE id = $1`, id).Scan(&token))
	require.Nil(t, token, "a magic link already in the mentor's inbox must be dead")
}

// "Any review invitations should invalidate." Revoked, not consumed: consumed_at
// means a review was submitted through the link, and the H4 cutover is gated on
// counting exactly that (D4).
func TestSoftDeleteRevokesOutstandingReviewInvitations(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := deletionTestRepo(t, pool)
	reviews := NewReviewRepository(pool)
	ctx := context.Background()

	id, _, _ := seedLiveMentor(t, pool, "invitations")

	requestID := seedDoneRequest(t, pool, id, "mentee-invitations@example.com")

	// Hashes derived from the request id: valid 64-char hex, and unique per run
	// so a leaked row from an interrupted run cannot collide with this one.
	liveHash := HashOneTimeToken("live-" + requestID)
	spentHash := HashOneTimeToken("spent-" + requestID)
	require.NoError(t, reviews.CreateReviewInvitation(ctx, requestID, liveHash, time.Now().Add(time.Hour)))
	require.NoError(t, reviews.CreateReviewInvitation(ctx, requestID, spentHash, time.Now().Add(time.Hour)))
	_, err := pool.Exec(ctx,
		`UPDATE review_invitations SET consumed_at = now() WHERE token_hash = $1`, spentHash)
	require.NoError(t, err)

	revoked, err := repo.SoftDeleteMentor(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 1, revoked, "only the outstanding invitation is revoked, not the spent one")

	// The live one no longer resolves...
	_, err = reviews.CheckCanSubmitReviewByToken(ctx, liveHash)
	require.ErrorIs(t, err, ErrReviewTokenInvalid)

	// ...and the already-spent one keeps its meaning, so the outstanding count
	// the cutover reads stays honest.
	var consumedAt, revokedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT consumed_at, revoked_at FROM review_invitations WHERE token_hash = $1`, spentHash).
		Scan(&consumedAt, &revokedAt))
	require.NotNil(t, consumedAt)
	require.Nil(t, revokedAt, "a spent invitation must not be relabelled as revoked")
}

// The legacy request-id review path has no invitation row to revoke, so its
// guard is the mentor join in the eligibility query.
func TestDeletedMentorKillsTheLegacyReviewPath(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := deletionTestRepo(t, pool)
	reviews := NewReviewRepository(pool)
	ctx := context.Background()

	id, _, _ := seedLiveMentor(t, pool, "legacyreview")

	requestID := seedDoneRequest(t, pool, id, "mentee-legacy@example.com")

	before, err := reviews.CheckCanSubmitReview(ctx, requestID)
	require.NoError(t, err)
	require.True(t, before.CanSubmit)

	_, err = repo.SoftDeleteMentor(ctx, id)
	require.NoError(t, err)

	after, err := reviews.CheckCanSubmitReview(ctx, requestID)
	require.NoError(t, err)
	require.False(t, after.CanSubmit)
	require.Empty(t, after.MentorName, "a dead capability must not leak the mentor's name")
}

func containsMentor(items []models.AdminMentorListItem, id string) bool {
	for _, item := range items {
		if item.MentorID == id {
			return true
		}
	}
	return false
}
