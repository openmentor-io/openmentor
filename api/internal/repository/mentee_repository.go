package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/models"
)

// ErrMenteeHasNoRequests reports that no client request was ever sent from the
// address, so there is no mentee to log in. The handler answers it exactly like
// success (account enumeration, M3).
var ErrMenteeHasNoRequests = errors.New("no client requests for this email")

// MenteeRepository is the mentee identity (D95): one row per email that has
// sent at least one request, holding only the magic-link token and the
// session_version revocation counter.
type MenteeRepository struct {
	pool *pgxpool.Pool
}

func NewMenteeRepository(pool *pgxpool.Pool) *MenteeRepository {
	return &MenteeRepository{pool: pool}
}

// SetLoginToken stores a fresh magic-link token for the mentee behind email,
// creating the mentee row on first use, and returns its id.
//
// The EXISTS gate and the write are one statement, so a mentee row is never
// created for an address that has no request. A second request before the first
// link is used replaces the token — the mentor and admin flows behave the same.
func (r *MenteeRepository) SetLoginToken(ctx context.Context, email, token string, exp time.Time) (string, error) {
	const query = `
		INSERT INTO mentees (email, login_token, login_token_expires_at)
		SELECT $1::citext, $2, $3
		WHERE EXISTS (SELECT 1 FROM client_requests WHERE email = $1::citext)
		ON CONFLICT (email) DO UPDATE
			SET login_token = EXCLUDED.login_token,
				login_token_expires_at = EXCLUDED.login_token_expires_at,
				updated_at = NOW()
		RETURNING id
	`
	var id string
	// SECURITY: store the hash, never the plaintext token (L1).
	err := r.pool.QueryRow(ctx, query, email, HashOneTimeToken(token), exp).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMenteeHasNoRequests
	}
	if err != nil {
		return "", fmt.Errorf("failed to store mentee login token: %w", err)
	}
	return id, nil
}

// ConsumedMenteeLogin is the mentee row returned by a successful magic-link
// consumption.
type ConsumedMenteeLogin struct {
	ID             string
	Email          string
	SessionVersion int
}

// ConsumeLoginToken atomically spends a mentee's magic-link token. Same
// single-UPDATE shape as MentorRepository.ConsumeLoginToken, for the same H1
// reasons: two concurrent clicks get one session, and no session is minted
// unless the clear succeeded.
func (r *MenteeRepository) ConsumeLoginToken(ctx context.Context, token string) (*ConsumedMenteeLogin, error) {
	const query = `
		UPDATE mentees
		SET login_token = NULL, login_token_expires_at = NULL, updated_at = NOW()
		WHERE login_token = $1
			AND login_token_expires_at >= NOW()
		RETURNING id, email::text, session_version
	`
	var consumed ConsumedMenteeLogin
	err := r.pool.QueryRow(ctx, query, HashOneTimeToken(token)).Scan(
		&consumed.ID, &consumed.Email, &consumed.SessionVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotConsumable
	}
	if err != nil {
		return nil, fmt.Errorf("failed to consume mentee login token: %w", err)
	}
	return &consumed, nil
}

// MenteeSessionState reads the revocation counter a live mentee session is
// checked against.
func (r *MenteeRepository) MenteeSessionState(ctx context.Context, menteeID string) (int, error) {
	var version int
	err := r.pool.QueryRow(ctx,
		`SELECT session_version FROM mentees WHERE id = $1`, menteeID,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrSessionSubjectGone
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read mentee session state: %w", err)
	}
	return version, nil
}

// BumpMenteeSessionVersion invalidates every session issued for this mentee
// (D58). Called on logout.
func (r *MenteeRepository) BumpMenteeSessionVersion(ctx context.Context, menteeID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE mentees SET session_version = session_version + 1, updated_at = NOW() WHERE id = $1`,
		menteeID,
	)
	if err != nil {
		return fmt.Errorf("failed to bump mentee session version: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionSubjectGone
	}
	return nil
}

// MenteeRequestRow is one request joined with what the dashboard may show of
// its mentor and review. Every nullable column is COALESCEd or scanned into a
// pointer (the D50 rule): client_requests is nullable nearly everywhere, and the
// mentor join is a LEFT JOIN because mentor_id is ON DELETE SET NULL.
type MenteeRequestRow struct {
	ID              string
	Status          models.RequestStatus
	Details         string
	CreatedAt       time.Time
	StatusChangedAt *time.Time
	DeclineReason   string
	DeclineComment  string

	MentorName        string
	MentorSlug        string
	MentorStatus      string
	MentorCalendarURL string
	// MentorAvailable is false when the request has no mentor row or the
	// mentor deleted their profile (D70).
	MentorAvailable bool

	ReviewCreatedAt *time.Time
	MentorReview    string
	PlatformReview  string
	Improvements    string
}

// ListRequests returns every request sent from the mentee's email, newest first.
// The mentee is resolved from its id, so the email in a session token is never
// what decides whose requests are read.
func (r *MenteeRepository) ListRequests(ctx context.Context, menteeID string) ([]MenteeRequestRow, error) {
	const query = `
		SELECT cr.id, cr.status, COALESCE(cr.description, ''), cr.created_at,
			cr.status_changed_at, COALESCE(cr.decline_reason, ''),
			COALESCE(cr.decline_comment, ''),
			COALESCE(m.name, ''), COALESCE(m.slug, ''), COALESCE(m.status, ''),
			COALESCE(m.calendar_url, ''),
			(m.id IS NOT NULL AND m.deleted_at IS NULL),
			rv.created_at, COALESCE(rv.mentor_review, ''),
			COALESCE(rv.platform_review, ''), COALESCE(rv.improvements, '')
		FROM mentees me
		JOIN client_requests cr ON cr.email = me.email
		LEFT JOIN mentors m ON m.id = cr.mentor_id
		LEFT JOIN reviews rv ON rv.client_request_id = cr.id
		WHERE me.id = $1
		ORDER BY cr.created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, menteeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list mentee requests: %w", err)
	}
	defer rows.Close()

	var out []MenteeRequestRow
	for rows.Next() {
		var row MenteeRequestRow
		var status string
		if err := rows.Scan(
			&row.ID, &status, &row.Details, &row.CreatedAt,
			&row.StatusChangedAt, &row.DeclineReason, &row.DeclineComment,
			&row.MentorName, &row.MentorSlug, &row.MentorStatus,
			&row.MentorCalendarURL, &row.MentorAvailable,
			&row.ReviewCreatedAt, &row.MentorReview, &row.PlatformReview, &row.Improvements,
		); err != nil {
			return nil, fmt.Errorf("failed to scan mentee request: %w", err)
		}
		row.Status = models.RequestStatus(status)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list mentee requests: %w", err)
	}
	return out, nil
}
