package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgUniqueViolation is Postgres' SQLSTATE for unique_violation. It is what makes
// "one review per client request" a database guarantee rather than a
// check-then-write race in Go.
const pgUniqueViolation = "23505"

// reviewTokenHashLen is the length of the hex sha256 this repository stores,
// mirroring the CHECK constraint in migration 000011.
const reviewTokenHashLen = 64

var (
	// ErrReviewTokenInvalid means no spendable invitation matched the hash.
	//
	// ONE error covers absent, already-consumed and expired on purpose: telling
	// those apart would tell a caller whether a token ever existed and whether the
	// request behind it is real, which is exactly the disclosure H4 removes. The
	// service turns this into a single generic message with no mentor name.
	ErrReviewTokenInvalid = errors.New("no spendable review invitation for this token")

	// ErrReviewIneligible means the capability was valid but the request cannot
	// receive a review: it is not in status 'done', or it already has one.
	ErrReviewIneligible = errors.New("request is not eligible for a review")
)

// ReviewRepository handles review data access
type ReviewRepository struct {
	pool *pgxpool.Pool
}

// NewReviewRepository creates a new review repository
func NewReviewRepository(pool *pgxpool.Pool) *ReviewRepository {
	return &ReviewRepository{
		pool: pool,
	}
}

// ReviewCheckResult holds the result of checking if a review can be submitted.
// MentorID is what review telemetry is attributed to; the request id cannot be,
// because it is a bearer capability for the legacy flow (P14/H4).
type ReviewCheckResult struct {
	CanSubmit  bool
	MentorID   string
	MentorName string
}

// ReviewContent is the mentee's free text. One struct so the two entry points
// (token and legacy request id) cannot drift in what they persist.
type ReviewContent struct {
	MentorReview   string
	PlatformReview string
	Improvements   string
}

// ReviewSubmission is the outcome of the transaction that spent a capability and
// wrote the review.
type ReviewSubmission struct {
	ReviewID string
	MentorID string
}

// eligibility is the client_requests row the review flow needs, joined with its
// mentor.
type eligibility struct {
	status        string
	mentorID      string
	mentorName    string
	hasReview     bool
	legacyRevoked bool
}

const eligibilityQuery = `
	SELECT cr.status, m.id AS mentor_id, m.name AS mentor_name,
		cr.review_legacy_link_revoked_at IS NOT NULL AS legacy_revoked,
		EXISTS(SELECT 1 FROM reviews rv WHERE rv.client_request_id = cr.id) AS has_review
	FROM client_requests cr
	JOIN mentors m ON m.id = cr.mentor_id
	WHERE cr.id = $1
`

// queryRower is satisfied by both the pool and a transaction, so the eligibility
// read is identical inside and outside the submission transaction.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// readEligibility returns (nil, nil) when there is no such request.
func readEligibility(ctx context.Context, q queryRower, requestID string) (*eligibility, error) {
	var e eligibility
	err := q.QueryRow(ctx, eligibilityQuery, requestID).Scan(
		&e.status, &e.mentorID, &e.mentorName, &e.legacyRevoked, &e.hasReview,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to check review eligibility: %w", err)
	}
	return &e, nil
}

func (e *eligibility) canSubmit() bool {
	return e.status == "done" && !e.hasReview
}

// CreateReviewInvitation stores a freshly minted capability. Called by the worker
// BEFORE it sends the email that carries the raw token — a token that was not
// persisted must never be mailed (the ordering FinalizeNewMentor documents).
//
// Rows are appended, never updated: only the hash is stored, so a resend cannot
// re-emit the token it already mailed and has to mint a new one. Leaving the
// previous row spendable is what keeps a link already sitting in the mentee's
// inbox working. Each row is independently single-use and `reviews` still permits
// exactly one review per request, so extra live invitations widen nothing.
func (r *ReviewRepository) CreateReviewInvitation(ctx context.Context, requestID, tokenHash string, expiresAt time.Time) error {
	if len(tokenHash) != reviewTokenHashLen {
		// Belt to the CHECK constraint's braces: catching a raw token passed where
		// a digest belongs means it never reaches the database at all.
		return fmt.Errorf("review invitation token hash has the wrong length")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO review_invitations (client_request_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, requestID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to create review invitation: %w", err)
	}
	return nil
}

// CheckCanSubmitReviewByToken resolves a capability to its request WITHOUT
// spending it, so the review page can render the mentor's name.
//
// Returns ErrReviewTokenInvalid for an absent, consumed or expired token, and
// nothing else: no mentor, no request, no distinction between the three.
func (r *ReviewRepository) CheckCanSubmitReviewByToken(ctx context.Context, tokenHash string) (*ReviewCheckResult, error) {
	var requestID string
	err := r.pool.QueryRow(ctx, `
		SELECT client_request_id FROM review_invitations
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
	`, tokenHash).Scan(&requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrReviewTokenInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up review invitation: %w", err)
	}

	e, err := readEligibility(ctx, r.pool, requestID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		// The foreign key makes this unreachable short of a concurrent delete;
		// report a dead capability rather than leaking that the request vanished.
		return nil, ErrReviewTokenInvalid
	}
	return &ReviewCheckResult{
		CanSubmit:  e.canSubmit(),
		MentorID:   e.mentorID,
		MentorName: e.mentorName,
	}, nil
}

// CheckCanSubmitReview is the LEGACY check: it authorizes on client_requests.id
// itself. Kept for the dual-read window so invitations already in mentees'
// inboxes keep working; removed at the cutover (see the H4 runbook).
//
// A request whose legacy link has been revoked reads exactly like a request that
// does not exist — that is the point of the revocation, so it must not answer
// with the mentor's name.
func (r *ReviewRepository) CheckCanSubmitReview(ctx context.Context, requestID string) (*ReviewCheckResult, error) {
	e, err := readEligibility(ctx, r.pool, requestID)
	if err != nil {
		return nil, err
	}
	if e == nil || e.legacyRevoked {
		return &ReviewCheckResult{CanSubmit: false}, nil
	}
	return &ReviewCheckResult{
		CanSubmit:  e.canSubmit(),
		MentorID:   e.mentorID,
		MentorName: e.mentorName,
	}, nil
}

// SubmitReviewWithToken spends a capability and writes the review in ONE
// transaction.
//
// Single-use is a SQL guarantee, not a Go one. The UPDATE is a compare-and-swap
// on consumed_at: concurrent callers holding the same token serialize on the
// invitation row, and Postgres re-evaluates the WHERE against the winner's
// committed row, so every later caller matches zero rows and gets
// ErrReviewTokenInvalid. (Same shape as FinalizeNewMentor's exclusive claim.)
//
// The claim and the INSERT share the transaction so a token is only spent when a
// review was actually stored: if the request turns out to be ineligible the
// rollback un-burns it, and a mentee who submits before the mentor marks the
// session done can still use their link afterwards.
func (r *ReviewRepository) SubmitReviewWithToken(ctx context.Context, tokenHash string, content ReviewContent) (*ReviewSubmission, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin review transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // a no-op once Commit succeeded

	var requestID string
	err = tx.QueryRow(ctx, `
		UPDATE review_invitations SET consumed_at = now()
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
		RETURNING client_request_id
	`, tokenHash).Scan(&requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrReviewTokenInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("failed to claim review invitation: %w", err)
	}

	submission, err := insertReview(ctx, tx, requestID, content, false)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit review: %w", err)
	}
	return submission, nil
}

// SubmitReviewForRequest is the LEGACY submission path, authorizing on
// client_requests.id. It is single-use for the same reason the token path is:
// `reviews.client_request_id` is UNIQUE, so two concurrent submissions serialize
// in the index and the loser gets ErrReviewIneligible instead of the untyped 500
// the previous check-then-insert produced.
func (r *ReviewRepository) SubmitReviewForRequest(ctx context.Context, requestID string, content ReviewContent) (*ReviewSubmission, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin review transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // a no-op once Commit succeeded

	submission, err := insertReview(ctx, tx, requestID, content, true)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit review: %w", err)
	}
	return submission, nil
}

// insertReview re-checks eligibility inside the transaction and writes the row.
// honourLegacyRevocation is set only for the legacy path: a per-request
// revocation kills the request-id link, never a properly issued token.
func insertReview(ctx context.Context, tx pgx.Tx, requestID string, content ReviewContent, honourLegacyRevocation bool) (*ReviewSubmission, error) {
	e, err := readEligibility(ctx, tx, requestID)
	if err != nil {
		return nil, err
	}
	if e == nil || (honourLegacyRevocation && e.legacyRevoked) {
		return nil, ErrReviewTokenInvalid
	}
	if !e.canSubmit() {
		return nil, fmt.Errorf("%w: request status %s", ErrReviewIneligible, e.status)
	}

	var reviewID string
	err = tx.QueryRow(ctx, `
		INSERT INTO reviews (client_request_id, mentor_review, platform_review, improvements)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, requestID, content.MentorReview, content.PlatformReview, content.Improvements).Scan(&reviewID)
	if err != nil {
		// The NOT EXISTS above ran against this transaction's snapshot, so a
		// concurrent submission that committed in between is caught here and only
		// here.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return nil, fmt.Errorf("%w: a review already exists", ErrReviewIneligible)
		}
		return nil, fmt.Errorf("failed to create review: %w", err)
	}

	return &ReviewSubmission{ReviewID: reviewID, MentorID: e.mentorID}, nil
}
