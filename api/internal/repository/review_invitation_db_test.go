package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/reviewtoken"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
)

// H4's single-use guarantee is pure SQL: it comes from Postgres re-evaluating the
// consume UPDATE's WHERE against the winning writer's committed row, and from the
// UNIQUE index on reviews.client_request_id. Neither can be modeled by a fake
// repository, so these tests need a real database and skip without one (see
// api/test/dbtest).

func reviewTestRepo(t *testing.T) (*ReviewRepository, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	return NewReviewRepository(pool), pool
}

// insertDoneRequest creates the row shape a completed mentorship leaves: a mentor
// plus a client_request in status 'done' with no review yet.
func insertDoneRequest(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	ctx := context.Background()

	var mentorID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO mentors (slug, name, status, email, sort_order)
		VALUES ($1, 'Review Test Mentor', 'active', $2, 1)
		RETURNING id
	`, slug, slug+"@example.com").Scan(&mentorID))

	var requestID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO client_requests (mentor_id, email, name, status)
		VALUES ($1, $2, 'Review Test Mentee', 'done')
		RETURNING id
	`, mentorID, slug+"-mentee@example.com").Scan(&requestID))

	t.Cleanup(func() {
		// client_requests, reviews and review_invitations all cascade from here.
		_, _ = pool.Exec(ctx, `DELETE FROM mentors WHERE id = $1`, mentorID)
	})
	return requestID
}

// issueInvitation mints a capability the way the worker does and returns the raw
// token. expiresIn may be negative to produce an already-expired invitation.
func issueInvitation(t *testing.T, repo *ReviewRepository, requestID string, expiresIn time.Duration) string {
	t.Helper()
	raw, hash, err := reviewtoken.New()
	require.NoError(t, err)
	require.NoError(t, repo.CreateReviewInvitation(context.Background(), requestID, hash, time.Now().Add(expiresIn)))
	return raw
}

func reviewContentFixture() ReviewContent {
	return ReviewContent{MentorReview: "a genuinely useful hour", PlatformReview: "", Improvements: ""}
}

// TestReviewInvitationIsSingleUseUnderConcurrency is the H4 acceptance criterion:
// two submissions with ONE token yield one success and one typed conflict. Run
// with real concurrent callers, because a sequential test would also pass against
// a check-then-write implementation that has the race.
func TestReviewInvitationIsSingleUseUnderConcurrency(t *testing.T) {
	repo, pool := reviewTestRepo(t)
	requestID := insertDoneRequest(t, pool, "review-single-use")
	raw := issueInvitation(t, repo, requestID, reviewtoken.TTL)
	hash := reviewtoken.Hash(raw)

	const callers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, callers)
	submissions := make([]*ReviewSubmission, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			submissions[i], results[i] = repo.SubmitReviewWithToken(context.Background(), hash, reviewContentFixture())
		}(i)
	}
	close(start)
	wg.Wait()

	successes, conflicts := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			successes++
			assert.NotEmpty(t, submissions[i].ReviewID)
		case errors.Is(err, ErrReviewTokenInvalid), errors.Is(err, ErrReviewIneligible):
			conflicts++
		default:
			t.Errorf("caller %d got an untyped error: %v", i, err)
		}
	}
	assert.Equal(t, 1, successes, "exactly one caller may spend the token")
	assert.Equal(t, callers-1, conflicts, "every other caller must get a typed conflict")

	// And the review really is single: the UNIQUE index, not the Go code.
	var reviews int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM reviews WHERE client_request_id = $1`, requestID).Scan(&reviews))
	assert.Equal(t, 1, reviews)

	// consumed_at is set, which is what makes a later attempt fail.
	var consumed *time.Time
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT consumed_at FROM review_invitations WHERE token_hash = $1`, hash).Scan(&consumed))
	require.NotNil(t, consumed)
}

// TestReviewInvitationSecondUseIsRefused is the sequential shape of the same
// guarantee, and pins that the second attempt learns nothing.
func TestReviewInvitationSecondUseIsRefused(t *testing.T) {
	repo, pool := reviewTestRepo(t)
	requestID := insertDoneRequest(t, pool, "review-second-use")
	raw := issueInvitation(t, repo, requestID, reviewtoken.TTL)
	hash := reviewtoken.Hash(raw)

	_, err := repo.SubmitReviewWithToken(context.Background(), hash, reviewContentFixture())
	require.NoError(t, err)

	_, err = repo.SubmitReviewWithToken(context.Background(), hash, reviewContentFixture())
	require.ErrorIs(t, err, ErrReviewTokenInvalid)

	// A consumed token also stops resolving on the read path, with no mentor name.
	_, err = repo.CheckCanSubmitReviewByToken(context.Background(), hash)
	require.ErrorIs(t, err, ErrReviewTokenInvalid)
}

// TestExpiredReviewInvitationRevealsNothing covers the other half of "expired or
// consumed tokens reveal no request details".
func TestExpiredReviewInvitationRevealsNothing(t *testing.T) {
	repo, pool := reviewTestRepo(t)
	requestID := insertDoneRequest(t, pool, "review-expired")
	raw := issueInvitation(t, repo, requestID, -time.Minute)
	hash := reviewtoken.Hash(raw)

	result, err := repo.CheckCanSubmitReviewByToken(context.Background(), hash)
	require.ErrorIs(t, err, ErrReviewTokenInvalid)
	assert.Nil(t, result, "an expired token must not resolve to a mentor")

	_, err = repo.SubmitReviewWithToken(context.Background(), hash, reviewContentFixture())
	require.ErrorIs(t, err, ErrReviewTokenInvalid)

	// The expired invitation is left unconsumed: nothing was spent.
	var consumed *time.Time
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT consumed_at FROM review_invitations WHERE token_hash = $1`, hash).Scan(&consumed))
	assert.Nil(t, consumed)
}

// TestUnknownReviewTokenRevealsNothing pins that a token nobody ever issued is
// indistinguishable from a spent one.
func TestUnknownReviewTokenRevealsNothing(t *testing.T) {
	repo, _ := reviewTestRepo(t)
	_, hash, err := reviewtoken.New()
	require.NoError(t, err)

	_, checkErr := repo.CheckCanSubmitReviewByToken(context.Background(), hash)
	require.ErrorIs(t, checkErr, ErrReviewTokenInvalid)
	_, submitErr := repo.SubmitReviewWithToken(context.Background(), hash, reviewContentFixture())
	require.ErrorIs(t, submitErr, ErrReviewTokenInvalid)
}

// TestReviewInvitationClaimRollsBackWhenIneligible is why the claim and the
// INSERT share a transaction: a mentee who submits before the mentor marks the
// session done must keep their link.
func TestReviewInvitationClaimRollsBackWhenIneligible(t *testing.T) {
	repo, pool := reviewTestRepo(t)
	requestID := insertDoneRequest(t, pool, "review-not-done")
	_, err := pool.Exec(context.Background(),
		`UPDATE client_requests SET status = 'working' WHERE id = $1`, requestID)
	require.NoError(t, err)

	raw := issueInvitation(t, repo, requestID, reviewtoken.TTL)
	hash := reviewtoken.Hash(raw)

	_, err = repo.SubmitReviewWithToken(context.Background(), hash, reviewContentFixture())
	require.ErrorIs(t, err, ErrReviewIneligible)

	var consumed *time.Time
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT consumed_at FROM review_invitations WHERE token_hash = $1`, hash).Scan(&consumed))
	assert.Nil(t, consumed, "an ineligible request must not burn the capability")

	// Once the mentor completes the session, the same link works.
	_, err = pool.Exec(context.Background(),
		`UPDATE client_requests SET status = 'done' WHERE id = $1`, requestID)
	require.NoError(t, err)
	submission, err := repo.SubmitReviewWithToken(context.Background(), hash, reviewContentFixture())
	require.NoError(t, err)
	assert.NotEmpty(t, submission.ReviewID)
}

// TestTwoLiveInvitationsShareOneReview pins the append semantics: reissuing does
// not invalidate the link already in the inbox, and the second one still cannot
// produce a second review.
func TestTwoLiveInvitationsShareOneReview(t *testing.T) {
	repo, pool := reviewTestRepo(t)
	requestID := insertDoneRequest(t, pool, "review-reissued")
	first := reviewtoken.Hash(issueInvitation(t, repo, requestID, reviewtoken.TTL))
	second := reviewtoken.Hash(issueInvitation(t, repo, requestID, reviewtoken.TTL))

	_, err := repo.SubmitReviewWithToken(context.Background(), first, reviewContentFixture())
	require.NoError(t, err)

	_, err = repo.SubmitReviewWithToken(context.Background(), second, reviewContentFixture())
	require.ErrorIs(t, err, ErrReviewIneligible)

	var reviews int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM reviews WHERE client_request_id = $1`, requestID).Scan(&reviews))
	assert.Equal(t, 1, reviews)
}

// TestLegacyReviewPathIsSingleUseUnderConcurrency covers the dual-read
// requirement that BOTH paths be single-use. Before H4 this was a check-then-
// insert race whose loser surfaced as an untyped 500.
func TestLegacyReviewPathIsSingleUseUnderConcurrency(t *testing.T) {
	repo, pool := reviewTestRepo(t)
	requestID := insertDoneRequest(t, pool, "review-legacy-single-use")

	const callers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = repo.SubmitReviewForRequest(context.Background(), requestID, reviewContentFixture())
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for i, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrReviewIneligible):
		default:
			t.Errorf("caller %d got an untyped error: %v", i, err)
		}
	}
	assert.Equal(t, 1, successes, "exactly one legacy submission may win")
}

// TestLegacyLinkRevocationHidesTheRequest is the remedy for the request ids that
// already leaked into telemetry: an operator marks them revoked and the legacy
// link stops working AND stops disclosing the mentor, while a properly issued
// token for the same request still works.
func TestLegacyLinkRevocationHidesTheRequest(t *testing.T) {
	repo, pool := reviewTestRepo(t)
	requestID := insertDoneRequest(t, pool, "review-legacy-revoked")

	before, err := repo.CheckCanSubmitReview(context.Background(), requestID)
	require.NoError(t, err)
	require.True(t, before.CanSubmit)
	require.NotEmpty(t, before.MentorName)

	_, err = pool.Exec(context.Background(),
		`UPDATE client_requests SET review_legacy_link_revoked_at = now() WHERE id = $1`, requestID)
	require.NoError(t, err)

	after, err := repo.CheckCanSubmitReview(context.Background(), requestID)
	require.NoError(t, err)
	assert.False(t, after.CanSubmit)
	assert.Empty(t, after.MentorName, "a revoked legacy link must not disclose the mentor")

	_, err = repo.SubmitReviewForRequest(context.Background(), requestID, reviewContentFixture())
	require.ErrorIs(t, err, ErrReviewTokenInvalid)

	// The revocation targets the legacy link only — the reissued token still works.
	hash := reviewtoken.Hash(issueInvitation(t, repo, requestID, reviewtoken.TTL))
	result, err := repo.CheckCanSubmitReviewByToken(context.Background(), hash)
	require.NoError(t, err)
	assert.True(t, result.CanSubmit)
	submission, err := repo.SubmitReviewWithToken(context.Background(), hash, reviewContentFixture())
	require.NoError(t, err)
	assert.NotEmpty(t, submission.ReviewID)
}

// TestRawTokenIsNeverPersisted is the storage-side sentinel: nothing in
// review_invitations is the raw value, so a database leak yields no capability.
func TestRawTokenIsNeverPersisted(t *testing.T) {
	repo, pool := reviewTestRepo(t)
	requestID := insertDoneRequest(t, pool, "review-hash-at-rest")
	raw := issueInvitation(t, repo, requestID, reviewtoken.TTL)

	var stored string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT token_hash FROM review_invitations WHERE client_request_id = $1`, requestID).Scan(&stored))
	assert.NotContains(t, stored, raw)
	assert.Len(t, stored, 64, "the column holds a hex sha256")
	assert.Equal(t, reviewtoken.Hash(raw), stored)

	// Looking the row up by the RAW value must find nothing — the CHECK
	// constraint also refuses to store one.
	var found int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM review_invitations WHERE token_hash = $1`, raw).Scan(&found))
	assert.Zero(t, found)
	require.Error(t, repo.CreateReviewInvitation(context.Background(), requestID, raw, time.Now().Add(time.Hour)))
}
