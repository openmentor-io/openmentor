package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/cache"
	"github.com/openmentor-io/openmentor/api/pkg/tokenhash"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/require"
)

// The exactly-once property of a magic link lives in SQL — an UPDATE whose WHERE
// Postgres re-evaluates against the winning writer's committed row — so no fake
// repository can demonstrate it. These tests need a real database and skip without
// one (see the dbtest package docs); CI / API runs them against its own service
// container.
//
// racers is enough concurrency to lose reliably under the old check-then-act shape
// while keeping the pool comfortable.
const racers = 16

func newTestMentorRepo(pool *pgxpool.Pool) *MentorRepository {
	return NewMentorRepository(pool, cache.NewTagsCache(
		func(context.Context) (map[string]string, error) { return map[string]string{}, nil },
	))
}

// seedMentor inserts a mentor in the given status and returns its id.
func seedMentorWithStatus(t *testing.T, pool *pgxpool.Pool, status, slugSuffix string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO mentors (slug, name, status, email)
		VALUES ($1, 'Session Test', $2, $3)
		RETURNING id
	`, "session-test-"+slugSuffix, status, slugSuffix+"@example.test").Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
	})
	return id
}

func seedModerator(t *testing.T, pool *pgxpool.Pool, role, suffix string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO moderators (name, email, role) VALUES ('Session Test', $1, $2)
		RETURNING id
	`, "mod-"+suffix+"@example.test", role).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM moderators WHERE id = $1`, id)
	})
	return id
}

// TestConsumeLoginTokenHasExactlyOneWinner is the H1 acceptance criterion: two (or
// sixteen) concurrent verifications of one magic link must produce exactly one
// session. Under the old shape — SELECT by token, check the expiry in Go, UPDATE to
// clear — every racer passed the SELECT and every racer minted a 24-hour session.
func TestConsumeLoginTokenHasExactlyOneWinner(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := newTestMentorRepo(pool)
	ctx := context.Background()

	id := seedMentorWithStatus(t, pool, "active", "race")
	const token = "mtk_concurrent_verification_probe"
	require.NoError(t, repo.SetLoginToken(ctx, id, token, time.Now().Add(time.Hour)))

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []*ConsumedMentorLogin
		losses  int
	)
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them together, so they really overlap
			consumed, err := repo.ConsumeLoginToken(ctx, token)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners = append(winners, consumed)
			case errors.Is(err, ErrTokenNotConsumable):
				losses++
			default:
				t.Errorf("ConsumeLoginToken: unexpected error %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Len(t, winners, 1, "%d concurrent verifications minted %d sessions from one link", racers, len(winners))
	require.Equal(t, racers-1, losses)
	require.Equal(t, id, winners[0].MentorID)

	// And the row is genuinely spent, so a later click cannot mint a session either.
	var remaining *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT login_token FROM mentors WHERE id = $1`, id).Scan(&remaining))
	require.Nil(t, remaining, "the winning consumption must clear the token")
}

// TestConsumeModeratorLoginTokenHasExactlyOneWinner is the same property on the
// privileged side, where a duplicated session is a duplicated admin session.
func TestConsumeModeratorLoginTokenHasExactlyOneWinner(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewModeratorRepository(pool)
	ctx := context.Background()

	id := seedModerator(t, pool, "admin", "race")
	const token = "atk_concurrent_verification_probe"
	require.NoError(t, repo.SetLoginToken(ctx, id, token, time.Now().Add(time.Hour)))

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
	)
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			consumed, err := repo.ConsumeLoginToken(ctx, token)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				winners++
				require.Equal(t, id, consumed.ID)
				require.Equal(t, "admin", string(consumed.Role))
			} else if !errors.Is(err, ErrTokenNotConsumable) {
				t.Errorf("ConsumeLoginToken: unexpected error %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, 1, winners, "%d concurrent verifications minted %d admin sessions from one link", racers, winners)
}

// TestConsumeLoginTokenChecksTheExpiryInSQL: the expiry used to be compared in Go
// after the row was already read, which is what made it a check-then-act at all.
func TestConsumeLoginTokenChecksTheExpiryInSQL(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := newTestMentorRepo(pool)
	ctx := context.Background()

	id := seedMentorWithStatus(t, pool, "active", "expired")
	const token = "mtk_already_expired"
	require.NoError(t, repo.SetLoginToken(ctx, id, token, time.Now().Add(-time.Minute)))

	_, err := repo.ConsumeLoginToken(ctx, token)
	require.True(t, errors.Is(err, ErrTokenNotConsumable), "expired token was consumable: %v", err)

	// An expired token is refused, NOT silently cleared: the row is left alone so
	// nothing else about it changes on a failed attempt.
	var stored *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT login_token FROM mentors WHERE id = $1`, id).Scan(&stored))
	require.NotNil(t, stored)
}

// TestLoginTokensAreNeverStoredInPlaintext pins L1 for both identity tables: the
// value on the row must be a digest, so a database dump cannot be replayed as a
// credential.
func TestLoginTokensAreNeverStoredInPlaintext(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	const token = "mtk_plaintext_probe"

	mentorID := seedMentorWithStatus(t, pool, "active", "hashing")
	require.NoError(t, newTestMentorRepo(pool).SetLoginToken(ctx, mentorID, token, time.Now().Add(time.Hour)))

	modID := seedModerator(t, pool, "moderator", "hashing")
	require.NoError(t, NewModeratorRepository(pool).SetLoginToken(ctx, modID, token, time.Now().Add(time.Hour)))

	for _, probe := range []struct{ table, id string }{
		{"mentors", mentorID}, {"moderators", modID},
	} {
		var stored string
		//nolint:gosec // fixed table names from this test's own literal list
		require.NoError(t, pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT login_token FROM %s WHERE id = $1`, probe.table), probe.id).Scan(&stored))
		require.NotEqual(t, token, stored, "%s.login_token holds the plaintext token", probe.table)
		require.Equal(t, tokenhash.Hash(token), stored)
	}
}

// TestConsumeConfirmationTokenHasExactlyOneWinner: a double-clicked confirmation
// link must produce exactly one draft -> pending transition, because the caller
// fires the "application in review" email and the moderator notification off the
// back of it.
func TestConsumeConfirmationTokenHasExactlyOneWinner(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := newTestMentorRepo(pool)
	ctx := context.Background()

	id := seedMentorWithStatus(t, pool, "draft", "confirm-race")
	const token = "mcf_concurrent_confirmation_probe"
	setConfirmationToken(t, pool, id, token, time.Now().Add(time.Hour))

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		applied int
	)
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := repo.ConsumeConfirmationToken(ctx, token)
			mu.Lock()
			defer mu.Unlock()
			require.NoError(t, err)
			if ok {
				applied++
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, 1, applied, "%d concurrent confirmations applied %d times", racers, applied)

	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM mentors WHERE id = $1`, id).Scan(&status))
	require.Equal(t, "pending", status)
}

// TestRotateConfirmationTokenKillsTheOldLink is the third H1 acceptance criterion:
// after a resend the old token must stop working. The rotation and the
// invalidation are the same write, so there is no window where both links work.
func TestRotateConfirmationTokenKillsTheOldLink(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := newTestMentorRepo(pool)
	ctx := context.Background()

	id := seedMentorWithStatus(t, pool, "draft", "rotate")
	const oldToken = "mcf_the_link_they_already_have"
	const newToken = "mcf_the_link_the_resend_sends"
	setConfirmationToken(t, pool, id, oldToken, time.Now().Add(time.Hour))

	applied, err := repo.RotateConfirmationToken(ctx, oldToken, newToken, time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	require.True(t, applied)

	consumedOld, err := repo.ConsumeConfirmationToken(ctx, oldToken)
	require.NoError(t, err)
	require.False(t, consumedOld, "the old confirmation link still worked after a resend")

	consumedNew, err := repo.ConsumeConfirmationToken(ctx, newToken)
	require.NoError(t, err)
	require.True(t, consumedNew, "the resent link does not work")

	// A second rotation keyed on the token we already replaced must not apply — the
	// gate that stops a losing resend from emailing a token it never stored.
	staleRotation, err := repo.RotateConfirmationToken(ctx, oldToken, "mcf_third", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.False(t, staleRotation)
}

// TestConfirmationTokensAreStoredHashed pins D57: the row must hold a digest, and
// a lookup by the plaintext must still find it.
func TestConfirmationTokensAreStoredHashed(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := newTestMentorRepo(pool)
	ctx := context.Background()

	id := seedMentorWithStatus(t, pool, "draft", "confirm-hashing")
	const token = "mcf_hashing_probe"
	// Written the way the resend path writes it, so this covers the real code path
	// rather than a test fixture's idea of the stored form.
	setConfirmationTokenViaRotate(t, repo, pool, id, token)

	var stored string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT email_confirmation_token FROM mentors WHERE id = $1`, id).Scan(&stored))
	require.NotEqual(t, token, stored, "the confirmation token is stored in plaintext")
	require.Equal(t, tokenhash.Hash(token), stored)

	found, err := repo.GetByConfirmationToken(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, found, "a hashed token must still be findable by its plaintext")
	require.Equal(t, id, found.MentorID)
}

// TestALegacyPlaintextConfirmationTokenStillWorks is the one-deploy transition:
// links already in mentors' inboxes were issued before hashing, so their rows hold
// the token verbatim and must keep resolving. Delete with the fallback.
func TestALegacyPlaintextConfirmationTokenStillWorks(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := newTestMentorRepo(pool)
	ctx := context.Background()

	id := seedMentorWithStatus(t, pool, "draft", "legacy")
	const token = "mcf_issued_before_hashing"
	setConfirmationToken(t, pool, id, token, time.Now().Add(time.Hour)) // stored verbatim

	found, err := repo.GetByConfirmationToken(ctx, token)
	require.NoError(t, err)
	require.NotNil(t, found, "a pre-D57 confirmation link stopped working")

	applied, err := repo.ConsumeConfirmationToken(ctx, token)
	require.NoError(t, err)
	require.True(t, applied)
}

// TestAStoredConfirmationHashIsNotItselfAToken is why the legacy fallback is scoped
// to the "mcf_" prefix. An unscoped "submitted == stored" fallback would let anyone
// holding a database dump submit the stored DIGEST as their token, which undoes the
// hashing completely.
func TestAStoredConfirmationHashIsNotItselfAToken(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := newTestMentorRepo(pool)
	ctx := context.Background()

	id := seedMentorWithStatus(t, pool, "draft", "digest-replay")
	const token = "mcf_digest_replay_probe"
	setConfirmationTokenViaRotate(t, repo, pool, id, token)

	digest := tokenhash.Hash(token)
	found, err := repo.GetByConfirmationToken(ctx, digest)
	require.NoError(t, err)
	require.Nil(t, found, "the stored digest was accepted as a token")

	applied, err := repo.ConsumeConfirmationToken(ctx, digest)
	require.NoError(t, err)
	require.False(t, applied, "the stored digest confirmed the mentor")
}

func TestMentorSessionStateAndRevocation(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := newTestMentorRepo(pool)
	ctx := context.Background()

	id := seedMentorWithStatus(t, pool, "active", "session-state")

	status, version, err := repo.MentorSessionState(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "active", status)
	require.Equal(t, 1, version, "the migration's default is what pre-existing rows get")

	require.NoError(t, repo.BumpMentorSessionVersion(ctx, id))
	_, bumped, err := repo.MentorSessionState(ctx, id)
	require.NoError(t, err)
	require.Equal(t, version+1, bumped, "a logout that does not move the counter revokes nothing")

	_, _, err = repo.MentorSessionState(ctx, "00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, ErrSessionSubjectGone)
	require.ErrorIs(t, repo.BumpMentorSessionVersion(ctx, "00000000-0000-0000-0000-000000000000"), ErrSessionSubjectGone)
}

func TestModeratorSessionStateAndRevocation(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewModeratorRepository(pool)
	ctx := context.Background()

	id := seedModerator(t, pool, "admin", "session-state")

	role, version, err := repo.ModeratorSessionState(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "admin", role)
	require.Equal(t, 1, version)

	require.NoError(t, repo.BumpModeratorSessionVersion(ctx, id))
	_, bumped, err := repo.ModeratorSessionState(ctx, id)
	require.NoError(t, err)
	require.Equal(t, version+1, bumped)

	// A demotion done straight in SQL — how it actually happens — is visible to the
	// next request, which is what the admin middleware relies on.
	_, err = pool.Exec(ctx, `UPDATE moderators SET role = 'moderator' WHERE id = $1`, id)
	require.NoError(t, err)
	role, _, err = repo.ModeratorSessionState(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "moderator", role)
}

// TestTheLoginTokenIndexIsUnique pins the schema guarantee behind the
// single-UPDATE consumption: at most one row can ever match a token.
func TestTheLoginTokenIndexIsUnique(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	first := seedMentorWithStatus(t, pool, "active", "uniq-a")
	second := seedMentorWithStatus(t, pool, "active", "uniq-b")
	repo := newTestMentorRepo(pool)

	require.NoError(t, repo.SetLoginToken(ctx, first, "mtk_shared", time.Now().Add(time.Hour)))
	require.Error(t, repo.SetLoginToken(ctx, second, "mtk_shared", time.Now().Add(time.Hour)),
		"two mentors were allowed to hold the same login token")
}

// setConfirmationToken writes the token VERBATIM, which is what a pre-D57 row
// looks like. Use setConfirmationTokenViaRotate for the current shape.
func setConfirmationToken(t *testing.T, pool *pgxpool.Pool, mentorID, token string, expiresAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		UPDATE mentors SET email_confirmation_token = $1, email_confirmation_expires_at = $2
		WHERE id = $3
	`, token, expiresAt, mentorID)
	require.NoError(t, err)
}

// setConfirmationTokenViaRotate seeds a token through the production write path, so
// the stored form is whatever the code really stores.
func setConfirmationTokenViaRotate(t *testing.T, repo *MentorRepository, pool *pgxpool.Pool, mentorID, token string) {
	t.Helper()
	// Rotation is a compare-and-swap, so give it a known predecessor to swap out.
	const seed = "mcf_seed"
	setConfirmationToken(t, pool, mentorID, seed, time.Now().Add(time.Hour))
	applied, err := repo.RotateConfirmationToken(context.Background(), seed, token, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.True(t, applied)
}
