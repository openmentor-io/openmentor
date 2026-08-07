package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/models"
)

// clientRequestSelect is the column list every read in this file shares — the
// column order ScanClientRequest expects.
//
// Same rule as mentorSelect: client_requests is nullable nearly everywhere and
// pgx fails the WHOLE row scan on a NULL in a non-pointer destination, so every
// nullable column is either COALESCEd here or scanned into a pointer field of
// MentorClientRequest. mentor_id is nullable because the FK is ON DELETE SET
// NULL; the empty string is the right substitute rather than an invented id,
// because the ownership checks that compare it against a session's mentor id can
// never match it — one orphaned request is then refused, instead of failing the
// read for every request the mentor has.
const clientRequestSelect = `
	SELECT cr.id, COALESCE(cr.mentor_id::text, ''), COALESCE(cr.email::text, ''),
		COALESCE(cr.name, ''), COALESCE(cr.preferred_contact, ''),
		COALESCE(cr.description, ''),
		cr.level, cr.status, cr.created_at, cr.updated_at, cr.status_changed_at,
		cr.scheduled_at, cr.decline_reason, cr.decline_comment,
		r.mentor_review
	FROM client_requests cr
	LEFT JOIN reviews r ON r.client_request_id = cr.id
`

// ClientRequestRepository handles client request data access
type ClientRequestRepository struct {
	pool *pgxpool.Pool
}

// NewClientRequestRepository creates a new client request repository
func NewClientRequestRepository(pool *pgxpool.Pool) *ClientRequestRepository {
	return &ClientRequestRepository{
		pool: pool,
	}
}

// ErrMentorNotContactable reports that the mentor named by a contact request is
// not accepting requests — no active, non-deleted mentor row matched — so NO
// client request was created. It is the SQL gate's answer, not a Go check that
// preceded the write.
var ErrMentorNotContactable = errors.New("mentor is not accepting requests")

// CreatedClientRequest is what a successful contact submission produces: the new
// request id, plus the mentor's calendar link read in the SAME statement that
// created the row.
type CreatedClientRequest struct {
	ID string
	// CalendarURL is empty when the mentor has not published one.
	CalendarURL string
}

// Create records a contact request, gated in SQL on the mentor being contactable
// (C3).
//
// The gate is the INSERT ... SELECT, not a preceding read: the service used to
// insert the row FIRST and fetch the mentor afterwards, so a draft, pending or
// declined mentor — one who has never been approved, or was refused — could be
// contacted through the API even though the profile page offers no form, and the
// mentee got a confirmation for a request that mentor would never see.
// 'active' is exactly the state the profile page shows a contact form for
// (models.Mentor.IsVisible), so a mentor who has hidden themselves is refused
// here too, matching what the UI already tells a visitor.
//
// The calendar_url comes out of the same statement for the same reason. Reading
// it separately is what let an inactive mentor's calendar link be handed to a
// mentee, and a CTE cannot return a row for a mentor the INSERT did not accept.
//
// status_changed_at is left NULL on purpose: the worker's SetRequestContactPending
// claims a fresh request by filling it, and treats NULL as "nobody has moved this
// yet".
func (r *ClientRequestRepository) Create(ctx context.Context, req *models.ClientRequest) (*CreatedClientRequest, error) {
	query := `
		WITH contactable AS (
			SELECT m.id, COALESCE(m.calendar_url, '') AS calendar_url
			FROM mentors m
			WHERE m.id = $1::uuid
				AND m.status = 'active'
				AND m.deleted_at IS NULL
		), created AS (
			INSERT INTO client_requests (mentor_id, email, name, preferred_contact, description, level, status)
			SELECT contactable.id, $2::citext, $3::text, $4::text, $5::text, $6::text, 'pending'
			FROM contactable
			RETURNING id
		)
		SELECT created.id, contactable.calendar_url
		FROM created, contactable
	`

	var created CreatedClientRequest
	err := r.pool.QueryRow(ctx, query,
		req.MentorID,
		req.Email,
		req.Name,
		req.PreferredContact,
		req.Description,
		req.Level,
	).Scan(&created.ID, &created.CalendarURL)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMentorNotContactable
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create client request: %w", err)
	}

	return &created, nil
}

// GetByMentor retrieves all client requests for a mentor filtered by statuses
func (r *ClientRequestRepository) GetByMentor(ctx context.Context, mentorId string, statuses []models.RequestStatus) ([]*models.MentorClientRequest, error) {
	query := clientRequestSelect + `
		WHERE cr.mentor_id = $1 AND cr.status = ANY($2)
		ORDER BY cr.created_at ASC
	`

	// Convert statuses to strings for PostgreSQL array
	statusStrs := make([]string, len(statuses))
	for i, s := range statuses {
		statusStrs[i] = string(s)
	}

	rows, err := r.pool.Query(ctx, query, mentorId, statusStrs)
	if err != nil {
		return nil, fmt.Errorf("failed to get client requests: %w", err)
	}

	return models.ScanClientRequests(rows)
}

// ListByMentorFiltered retrieves a mentor's client requests for the admin
// moderation portal, newest first. An empty statuses slice means "every
// status" — unlike the mentor-facing inbox, admins see the full history in
// one list and filter it themselves.
func (r *ClientRequestRepository) ListByMentorFiltered(ctx context.Context, mentorID string, statuses []models.RequestStatus) ([]*models.MentorClientRequest, error) {
	query := clientRequestSelect + ` WHERE cr.mentor_id = $1`
	args := []interface{}{mentorID}

	if len(statuses) > 0 {
		statusStrs := make([]string, len(statuses))
		for i, s := range statuses {
			statusStrs[i] = string(s)
		}
		query += ` AND cr.status = ANY($2)`
		args = append(args, statusStrs)
	}
	query += ` ORDER BY cr.created_at DESC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list client requests for mentor: %w", err)
	}

	return models.ScanClientRequests(rows)
}

// GetByID retrieves a single client request by ID
func (r *ClientRequestRepository) GetByID(ctx context.Context, id string) (*models.MentorClientRequest, error) {
	query := clientRequestSelect + ` WHERE cr.id = $1`

	row := r.pool.QueryRow(ctx, query, id)
	return models.ScanClientRequest(row)
}

// UpdateStatus moves a client request from the status the caller READ to a new
// one, and reports whether this caller's write is the one that did it.
//
// The expected status is in the WHERE for the same reason the worker's claims are
// (H3, internal/worker/repository.go): every caller validates the transition
// against a row it read a moment earlier, and a blind UPDATE applies that
// verdict to whatever the row says NOW. Two mentor tabs, or a mentor and an
// admin, could each read 'pending' and both write — the second overwriting a
// transition it never saw, and re-stamping status_changed_at so the reminder
// jobs date the request from the wrong event.
//
// applied is false when the row moved (or vanished) in between. Callers must not
// send mail describing a transition this returned false for.
func (r *ClientRequestRepository) UpdateStatus(
	ctx context.Context,
	id string,
	expected models.RequestStatus,
	status models.RequestStatus,
) (applied bool, err error) {

	query := `
		UPDATE client_requests
		SET status = $1, status_changed_at = NOW(), updated_at = NOW()
		WHERE id = $2 AND status = $3
	`

	tag, err := r.pool.Exec(ctx, query, status, id, expected)
	if err != nil {
		return false, fmt.Errorf("failed to update status: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// UpdateDecline declines a client request that is still in the status the caller
// read, carrying the reason and comment. Same compare-and-set discipline as
// UpdateStatus: a decline must not land on a request the mentee already had
// completed, or on one a concurrent decline already wrote.
func (r *ClientRequestRepository) UpdateDecline(
	ctx context.Context,
	id string,
	expected models.RequestStatus,
	reason models.DeclineReason,
	comment string,
) (applied bool, err error) {

	query := `
		UPDATE client_requests
		SET status = 'declined', decline_reason = $1, decline_comment = $2,
			status_changed_at = NOW(), updated_at = NOW()
		WHERE id = $3 AND status = $4
	`

	tag, err := r.pool.Exec(ctx, query, reason, comment, id, expected)
	if err != nil {
		return false, fmt.Errorf("failed to update decline: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
