package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The client-request repository owns the mentee-facing state machine: what the
// mentor's inbox shows, what the admin history shows, and whether a review can
// still be submitted. Its behavior lives in SQL — a status filter passed as a
// text array, a LEFT JOIN onto reviews, an idempotent ON CONFLICT insert — so it
// gets database tests rather than a fake.
//
// Requires a database (see the dbtest package docs); skips without one.

func seedRequest(t *testing.T, pool *pgxpool.Pool, mentorID, status string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO client_requests (mentor_id, email, name, description, level, status)
		VALUES ($1, 'mentee@example.test', 'Mentee', 'help please', 'junior', $2)
		RETURNING id
	`, mentorID, status).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM client_requests WHERE id = $1`, id)
	})
	return id
}

func TestClientRequestCreateAndRead(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	ctx := context.Background()
	mentorID := seedMentor(t, pool, "requests-create")

	id, err := repo.Create(ctx, &models.ClientRequest{
		MentorID:         mentorID,
		Email:            "mentee@example.test",
		Name:             "Mentee",
		PreferredContact: "telegram",
		Description:      "I would like help with Go",
		Level:            "middle",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM client_requests WHERE id = $1`, id)
	})

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, mentorID, got.MentorID)
	assert.Equal(t, "I would like help with Go", got.Details)
	// Create hardcodes 'pending': a new request must never arrive already
	// accepted, whatever the caller passes.
	assert.Equal(t, models.RequestStatus("pending"), got.Status)
	assert.Nil(t, got.Review, "a fresh request has no review to join onto")
}

// TestGetByMentorFiltersByStatus: the mentor's inbox and "past" page are the same
// query with different status sets, and the statuses cross the boundary as a text
// array rather than as SQL.
func TestGetByMentorFiltersByStatus(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	ctx := context.Background()
	mentorID := seedMentor(t, pool, "requests-filter")

	pending := seedRequest(t, pool, mentorID, "pending")
	working := seedRequest(t, pool, mentorID, "working")
	done := seedRequest(t, pool, mentorID, "done")

	// Another mentor's request must never appear in this mentor's inbox.
	otherMentor := seedMentor(t, pool, "requests-filter-other")
	seedRequest(t, pool, otherMentor, "pending")

	active, err := repo.GetByMentor(ctx, mentorID, []models.RequestStatus{"pending", "working"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{pending, working}, requestIDs(active))

	finished, err := repo.GetByMentor(ctx, mentorID, []models.RequestStatus{"done"})
	require.NoError(t, err)
	assert.Equal(t, []string{done}, requestIDs(finished))

	// An empty status set means literally nothing here, NOT everything — the
	// admin list is the method with the "no filter" behavior.
	none, err := repo.GetByMentor(ctx, mentorID, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}

// TestListByMentorFilteredShowsEverythingUnfiltered is the admin counterpart, and
// the two disagree on purpose: admins see the full history newest-first.
func TestListByMentorFilteredShowsEverythingUnfiltered(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	ctx := context.Background()
	mentorID := seedMentor(t, pool, "requests-admin-list")

	seedRequest(t, pool, mentorID, "pending")
	seedRequest(t, pool, mentorID, "declined")
	seedRequest(t, pool, mentorID, "done")

	all, err := repo.ListByMentorFiltered(ctx, mentorID, nil)
	require.NoError(t, err)
	require.Len(t, all, 3)
	for i := 1; i < len(all); i++ {
		assert.False(t, all[i].CreatedAt.After(all[i-1].CreatedAt),
			"the admin list is newest-first")
	}

	declined, err := repo.ListByMentorFiltered(ctx, mentorID, []models.RequestStatus{"declined"})
	require.NoError(t, err)
	assert.Len(t, declined, 1)
}

// TestUpdateStatusStampsStatusChangedAt: status_changed_at is what the inbox
// sorts and ages requests by, so a transition that leaves it behind makes a
// request look stale forever.
func TestUpdateStatusStampsStatusChangedAt(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	ctx := context.Background()
	mentorID := seedMentor(t, pool, "requests-status")
	id := seedRequest(t, pool, mentorID, "pending")

	before, err := repo.GetByID(ctx, id)
	require.NoError(t, err)

	require.NoError(t, repo.UpdateStatus(ctx, id, "working"))

	after, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, models.RequestStatus("working"), after.Status)
	require.NotNil(t, after.StatusChangedAt)
	if before.StatusChangedAt != nil {
		assert.True(t, after.StatusChangedAt.After(*before.StatusChangedAt))
	}
	assert.True(t, after.ModifiedAt.After(before.ModifiedAt) ||
		after.ModifiedAt.Equal(before.ModifiedAt))
}

// TestUpdateDeclineRecordsReasonAndComment. Declining is the one transition that
// carries a payload, and it sets the status itself rather than trusting a caller.
func TestUpdateDeclineRecordsReasonAndComment(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	ctx := context.Background()
	mentorID := seedMentor(t, pool, "requests-decline")
	id := seedRequest(t, pool, mentorID, "pending")

	require.NoError(t, repo.UpdateDecline(ctx, id, models.DeclineTopicMismatch, "try a backend mentor"))

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, models.RequestStatus("declined"), got.Status)
	assert.Equal(t, string(models.DeclineTopicMismatch), got.DeclineReason)
	require.NotNil(t, got.DeclineComment)
	assert.Equal(t, "try a backend mentor", *got.DeclineComment)
}

// TestReviewEligibilityFollowsTheRequestState pins the three-way answer the
// public review link depends on: unknown request, not finished, already reviewed.
// CheckCanSubmitReview returns (false, nil) for a missing request rather than an
// error, so a wrong link is "you cannot review this", not a 500.
func TestReviewEligibilityFollowsTheRequestState(t *testing.T) {
	pool := dbtest.Pool(t)
	requests := NewClientRequestRepository(pool)
	reviews := NewReviewRepository(pool)
	ctx := context.Background()
	mentorID := seedMentor(t, pool, "reviews-eligibility")

	missing, err := reviews.CheckCanSubmitReview(ctx, "00000000-0000-0000-0000-0000000000fd")
	require.NoError(t, err)
	assert.False(t, missing.CanSubmit)
	assert.Empty(t, missing.MentorID)

	id := seedRequest(t, pool, mentorID, "working")
	notDone, err := reviews.CheckCanSubmitReview(ctx, id)
	require.NoError(t, err)
	assert.False(t, notDone.CanSubmit)
	// The mentor is still identified, because the check page names them.
	assert.Equal(t, mentorID, notDone.MentorID)
	assert.Equal(t, "Update Test", notDone.MentorName)

	require.NoError(t, requests.UpdateStatus(ctx, id, "done"))
	eligible, err := reviews.CheckCanSubmitReview(ctx, id)
	require.NoError(t, err)
	assert.True(t, eligible.CanSubmit)

	submission, err := reviews.SubmitReviewForRequest(ctx, id, ReviewContent{
		MentorReview: "great session", PlatformReview: "good platform", Improvements: "none",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, submission.ReviewID)

	alreadyReviewed, err := reviews.CheckCanSubmitReview(ctx, id)
	require.NoError(t, err)
	assert.False(t, alreadyReviewed.CanSubmit,
		"the review link is single-use: the LEFT JOIN must see the new row")

	// And the second submission is refused. Since H4, SubmitReviewForRequest
	// re-checks eligibility inside its transaction, so a sequential resubmit is
	// refused there (ErrReviewIneligible) before the UNIQUE constraint can fire;
	// the constraint still backs the concurrent check-then-write race, which
	// review_invitation_db_test.go exercises with 8 simultaneous callers.
	_, err = reviews.SubmitReviewForRequest(ctx, id, ReviewContent{
		MentorReview: "again", PlatformReview: "again", Improvements: "again",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReviewIneligible)
}

// TestReviewJoinsOntoTheRequestRead: the mentor's inbox shows the review text via
// a LEFT JOIN, so the row must stay readable both before and after a review lands.
func TestReviewJoinsOntoTheRequestRead(t *testing.T) {
	pool := dbtest.Pool(t)
	requests := NewClientRequestRepository(pool)
	reviews := NewReviewRepository(pool)
	ctx := context.Background()
	mentorID := seedMentor(t, pool, "reviews-join")
	id := seedRequest(t, pool, mentorID, "done")

	got, err := requests.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, got.Review)

	_, err = reviews.SubmitReviewForRequest(ctx, id, ReviewContent{MentorReview: "clear and practical"})
	require.NoError(t, err)

	got, err = requests.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got.Review)
	assert.Equal(t, "clear and practical", *got.Review)
}

// TestMigrationIntentCreateIsIdempotent: the public opt-in endpoint is
// unauthenticated and retryable, so a resubmission must keep the original row
// and report created=false rather than 500 on the unique index.
func TestMigrationIntentCreateIsIdempotent(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewMigrationIntentRepository(pool)
	ctx := context.Background()
	const slug = "migration-intent-idempotent"

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM migration_intents WHERE slug = $1`, slug)
	})

	created, err := repo.Create(ctx, slug)
	require.NoError(t, err)
	assert.True(t, created)

	var firstSeen time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT created_at FROM migration_intents WHERE slug = $1`, slug).Scan(&firstSeen))

	created, err = repo.Create(ctx, slug)
	require.NoError(t, err)
	assert.False(t, created, "a resubmission is not a new intent")

	var stillSeen time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT created_at FROM migration_intents WHERE slug = $1`, slug).Scan(&stillSeen))
	assert.Equal(t, firstSeen, stillSeen, "the original row must survive untouched")
}

func requestIDs(requests []*models.MentorClientRequest) []string {
	ids := make([]string, 0, len(requests))
	for _, r := range requests {
		ids = append(ids, r.ID)
	}
	return ids
}
