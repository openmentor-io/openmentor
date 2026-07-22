package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file holds the worker's own data access layer. The API's
// internal/repository types are shaped around the public API (public field
// filtering, hidden email/contact columns), while the job
// handlers ported from openmentor-func need raw rows including email,
// preferred_contact and login token columns. The SQL below mirrors the
// queries in openmentor-func/lib/utils/db.ts and each function's index.ts.

// JobMentor is the mentor row shape the job handlers need. It mirrors the
// Mentor class in openmentor-func/lib/data/mentor.ts (PgRowAdapter mapping).
type JobMentor struct {
	ID               string // uuid primary key
	LegacyID         int    // legacy numeric id (used in slug generation)
	Name             string
	Email            string
	Status           string
	PreferredContact string
	Slug             string
	JobTitle         string
	Workplace        string
	Price            string
	CalendarURL      string // "Calendly Url" in the func app
	// Draft-status workflow columns (openmentor-only, no func-app
	// counterpart): the reviewer note from a moderation 'return' and the
	// registration email confirmation token.
	ModerationNote         string
	EmailConfirmationToken string
}

// JobRequest is the client_requests row shape the job handlers need,
// mirroring the Request class in openmentor-func/lib/data/mentor.ts.
type JobRequest struct {
	ID               string
	MentorID         string
	Name             string
	Email            string
	PreferredContact string
	Description      string
	Level            string
	Status           string
	DeclineReason    string
	DeclineComment   string
	MentorName       string // populated only by GetJobRequestWithMentorName
}

// JobReview is the reviews row (joined with its client request) used by the
// process-mentee-review job, mirroring the Review class in the func app.
type JobReview struct {
	ID         string
	RequestID  string
	MentorID   string
	MenteeName string // cr.name from the joined client request
	ReviewText string // mentor_review
}

// JobModerator is the moderators row used by the login/moderation jobs.
type JobModerator struct {
	ID    string
	Name  string
	Email string
}

// FinalizeNewMentorParams is the single UPDATE new-mentor-watcher performs
// after processing a fresh registration (mirrors the UPDATE in
// openmentor-func/new-mentor-watcher/index.ts, extended with the email
// confirmation token of the draft-status workflow).
type FinalizeNewMentorParams struct {
	MentorID         string
	Name             string
	PreferredContact string
	Slug             string
	Status           string
	SortOrder        int
	// Email confirmation token (nil for declined duplicates).
	EmailConfirmationToken     *string
	EmailConfirmationExpiresAt *time.Time
}

// JobReminderRequest is the trimmed client_requests row the cron reminder
// jobs list in their emails. DaysAgo carries the SQL-computed
// created_days_ago alias, which flows into Request.daysAgo in the func app
// (age of the request for sessions-watcher, staleness of the last status
// change for update-status-reminder).
type JobReminderRequest struct {
	ID          string
	Name        string
	Description string
	Status      string
	DaysAgo     int
}

// SortOrderUpdate is one mentors.sort_order write applied by SetSortOrders.
type SortOrderUpdate struct {
	MentorID  string
	SortOrder int
}

// JobsRepository is the data access surface used by the job handlers.
// Lookups return (nil, nil) when the record does not exist so handlers can
// map "not found" to a 404 without inspecting driver errors.
type JobsRepository interface {
	GetJobMentorByID(ctx context.Context, mentorID string) (*JobMentor, error)
	CountActiveMentorsByEmail(ctx context.Context, email string) (int, error)
	FinalizeNewMentor(ctx context.Context, params FinalizeNewMentorParams) error
	SetMentorStatus(ctx context.Context, mentorID, status string) error
	GetJobRequestByID(ctx context.Context, requestID string) (*JobRequest, error)
	GetJobRequestWithMentorName(ctx context.Context, requestID string) (*JobRequest, error)
	SetRequestContactPending(ctx context.Context, requestID, contact string) error
	GetJobModeratorByID(ctx context.Context, moderatorID string) (*JobModerator, error)
	GetJobReviewByID(ctx context.Context, reviewID string) (*JobReview, error)

	// Cron job queries (stage 3, timer-triggered functions).
	ListMentorsWithStalePendingRequests(ctx context.Context) ([]JobMentor, error)
	ListStalePendingRequests(ctx context.Context, mentorID string) ([]JobReminderRequest, error)
	ListMentorsWithStaleInProgressRequests(ctx context.Context) ([]JobMentor, error)
	ListStaleInProgressRequests(ctx context.Context, mentorID string) ([]JobReminderRequest, error)
	ListMentorsToDeactivate(ctx context.Context) ([]JobMentor, error)
	DeactivateMentor(ctx context.Context, mentorID string) error
	ListActiveMentorIDs(ctx context.Context) ([]string, error)
	SetSortOrders(ctx context.Context, updates []SortOrderUpdate) error

	// Photo-cutout backfill.
	ListMentorsForCutout(ctx context.Context) ([]CutoutMentor, error)
	GetMentorForCutout(ctx context.Context, mentorID string) (*CutoutMentor, error)
	SetPhotoStyle(ctx context.Context, mentorID, style string) error
}

// CutoutMentor is a mentor candidate for the photo-cutout backfill.
type CutoutMentor struct {
	ID   string
	Slug string
}

// Repository is the pgx-backed JobsRepository implementation.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds the worker's data access layer on the worker DB pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetJobMentorByID fetches a mentor row by uuid.
// Mirrors: SELECT * FROM mentors WHERE id = $1 (func app).
func (r *Repository) GetJobMentorByID(ctx context.Context, mentorID string) (*JobMentor, error) {
	query := `
		SELECT id, legacy_id, name, COALESCE(email::text, ''), status,
			COALESCE(preferred_contact, ''), COALESCE(slug, ''), COALESCE(job_title, ''),
			COALESCE(workplace, ''), COALESCE(price, ''), COALESCE(calendar_url, ''),
			COALESCE(moderation_note, ''), COALESCE(email_confirmation_token, '')
		FROM mentors
		WHERE id = $1
	`

	var m JobMentor
	err := r.pool.QueryRow(ctx, query, mentorID).Scan(
		&m.ID, &m.LegacyID, &m.Name, &m.Email, &m.Status,
		&m.PreferredContact, &m.Slug, &m.JobTitle,
		&m.Workplace, &m.Price, &m.CalendarURL,
		&m.ModerationNote, &m.EmailConfirmationToken,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mentor %s: %w", mentorID, err)
	}
	return &m, nil
}

// CountActiveMentorsByEmail counts mentors with the same email and status
// 'active'. Mirrors findDuplicates() in new-mentor-watcher/index.ts
// (SELECT * FROM mentors WHERE email = $1 AND status = 'active').
func (r *Repository) CountActiveMentorsByEmail(ctx context.Context, email string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM mentors WHERE email = $1 AND status = 'active'`,
		email,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count duplicate mentors: %w", err)
	}
	return count, nil
}

// FinalizeNewMentor performs the single UPDATE from new-mentor-watcher:
// trimmed fields, login token, slug, status, randomized sort order and the
// email confirmation token (draft-status workflow).
func (r *Repository) FinalizeNewMentor(ctx context.Context, p FinalizeNewMentorParams) error {
	// SECURITY: new registrations get NO usable login token (L2). A token is
	// only ever minted on demand by RequestLogin; leaving a standing long-lived
	// credential here would widen the blast radius of a DB leak.
	query := `
		UPDATE mentors SET
			name = $1,
			preferred_contact = $2,
			login_token = NULL,
			login_token_expires_at = NULL,
			slug = $3,
			status = $4,
			sort_order = $5,
			email_confirmation_token = $6,
			email_confirmation_expires_at = $7,
			updated_at = NOW()
		WHERE id = $8
	`
	_, err := r.pool.Exec(ctx, query,
		p.Name, p.PreferredContact,
		p.Slug, p.Status, p.SortOrder,
		p.EmailConfirmationToken, p.EmailConfirmationExpiresAt, p.MentorID,
	)
	if err != nil {
		return fmt.Errorf("failed to finalize new mentor %s: %w", p.MentorID, err)
	}
	return nil
}

// SetMentorStatus updates a mentor's status (used by the moderation job's
// idempotency check when the API's status write is missing/stale).
// HARD GUARD: a mentor that has ever been activated (activated_at IS NOT
// NULL) can never be moved back to 'draft'.
func (r *Repository) SetMentorStatus(ctx context.Context, mentorID, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE mentors SET status = $1, updated_at = NOW()
		 WHERE id = $2 AND NOT ($1 = 'draft' AND activated_at IS NOT NULL)`,
		status, mentorID,
	)
	if err != nil {
		return fmt.Errorf("failed to set mentor %s status: %w", mentorID, err)
	}
	return nil
}

const jobRequestColumns = `
	cr.id, cr.mentor_id, cr.name, COALESCE(cr.email::text, ''),
	COALESCE(cr.preferred_contact, ''), COALESCE(cr.description, ''),
	COALESCE(cr.level, ''), cr.status,
	COALESCE(cr.decline_reason::text, ''), COALESCE(cr.decline_comment, '')`

// GetJobRequestByID fetches a client request by uuid.
// Mirrors: SELECT * FROM client_requests WHERE id = $1 (func app).
func (r *Repository) GetJobRequestByID(ctx context.Context, requestID string) (*JobRequest, error) {
	query := `SELECT ` + jobRequestColumns + ` FROM client_requests cr WHERE cr.id = $1`

	var req JobRequest
	err := r.pool.QueryRow(ctx, query, requestID).Scan(
		&req.ID, &req.MentorID, &req.Name, &req.Email,
		&req.PreferredContact, &req.Description,
		&req.Level, &req.Status,
		&req.DeclineReason, &req.DeclineComment,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client request %s: %w", requestID, err)
	}
	return &req, nil
}

// GetJobRequestWithMentorName fetches a client request joined with the
// mentor's name. Mirrors request-process-finished/index.ts:
// SELECT cr.*, m.name AS mentor_name FROM client_requests cr
// JOIN mentors m ON m.id = cr.mentor_id WHERE cr.id = $1 (inner join: a
// request whose mentor row is gone reads as "not found", like the func).
func (r *Repository) GetJobRequestWithMentorName(ctx context.Context, requestID string) (*JobRequest, error) {
	query := `SELECT ` + jobRequestColumns + `, m.name AS mentor_name
		FROM client_requests cr
		JOIN mentors m ON m.id = cr.mentor_id
		WHERE cr.id = $1`

	var req JobRequest
	err := r.pool.QueryRow(ctx, query, requestID).Scan(
		&req.ID, &req.MentorID, &req.Name, &req.Email,
		&req.PreferredContact, &req.Description,
		&req.Level, &req.Status,
		&req.DeclineReason, &req.DeclineComment,
		&req.MentorName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client request %s with mentor: %w", requestID, err)
	}
	return &req, nil
}

// SetRequestContactPending stores the trimmed contact details and moves
// the request to 'pending'. Mirrors new-request-watcher/index.ts exactly
// (UPDATE client_requests SET preferred_contact = $1, status = $2 WHERE id = $3 -
// deliberately no updated_at/status_changed_at touch, matching the func).
func (r *Repository) SetRequestContactPending(ctx context.Context, requestID, contact string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE client_requests SET preferred_contact = $1, status = 'pending' WHERE id = $2`,
		contact, requestID,
	)
	if err != nil {
		return fmt.Errorf("failed to update client request %s: %w", requestID, err)
	}
	return nil
}

// GetJobModeratorByID fetches a moderator row by uuid.
func (r *Repository) GetJobModeratorByID(ctx context.Context, moderatorID string) (*JobModerator, error) {
	query := `SELECT id, name, COALESCE(email::text, '') FROM moderators WHERE id = $1`

	var m JobModerator
	err := r.pool.QueryRow(ctx, query, moderatorID).Scan(&m.ID, &m.Name, &m.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch moderator %s: %w", moderatorID, err)
	}
	return &m, nil
}

// listJobMentors runs a mentors query returning (id, name, email) rows.
func (r *Repository) listJobMentors(ctx context.Context, query string) ([]JobMentor, error) {
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mentors []JobMentor
	for rows.Next() {
		var m JobMentor
		if err := rows.Scan(&m.ID, &m.Name, &m.Email); err != nil {
			return nil, err
		}
		mentors = append(mentors, m)
	}
	return mentors, rows.Err()
}

// listJobReminderRequests runs a per-mentor client_requests query returning
// (id, name, description, status, created_days_ago) rows.
func (r *Repository) listJobReminderRequests(ctx context.Context, query, mentorID string) ([]JobReminderRequest, error) {
	rows, err := r.pool.Query(ctx, query, mentorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []JobReminderRequest
	for rows.Next() {
		var req JobReminderRequest
		if err := rows.Scan(&req.ID, &req.Name, &req.Description, &req.Status, &req.DaysAgo); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

// ListMentorsWithStalePendingRequests returns active mentors that have
// pending client requests older than 1 day. Mirrors the mentors query in
// sessions-watcher/index.ts verbatim (the pending_sessions_count column the
// func also selected feeds Mentor.pendingSessionsCount, which the reminder
// email never uses, so it is not selected here; the grouping semantics are
// unchanged).
func (r *Repository) ListMentorsWithStalePendingRequests(ctx context.Context) ([]JobMentor, error) {
	query := `
		SELECT m.id, m.name, COALESCE(m.email::text, '')
		FROM mentors m
			INNER JOIN client_requests cr ON cr.mentor_id = m.id
		WHERE m.status = 'active'
			AND cr.status = 'pending'
			AND (cr.created_at < NOW() - INTERVAL '1 days')
		GROUP BY m.id, m.name, m.email
		HAVING COUNT(cr.id) > 0
	`
	mentors, err := r.listJobMentors(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list mentors with stale pending requests: %w", err)
	}
	return mentors, nil
}

// ListStalePendingRequests returns a mentor's pending requests older than
// 24 hours, oldest first. Mirrors the per-mentor requests query in
// sessions-watcher/index.ts (same predicates, interval, days-ago
// computation and ordering; unused columns - preferred_contact, email, level,
// mentor_name, decline fields - are not selected).
func (r *Repository) ListStalePendingRequests(ctx context.Context, mentorID string) ([]JobReminderRequest, error) {
	query := `
		SELECT cr.id, cr.name, COALESCE(cr.description, ''), cr.status,
			EXTRACT(DAY FROM NOW() - cr.created_at)::int AS created_days_ago
		FROM client_requests cr
		WHERE cr.mentor_id = $1
			AND cr.status IN ('pending')
			AND (cr.created_at < NOW() - INTERVAL '24 hours')
		ORDER BY cr.created_at ASC
	`
	requests, err := r.listJobReminderRequests(ctx, query, mentorID)
	if err != nil {
		return nil, fmt.Errorf("failed to list stale pending requests for mentor %s: %w", mentorID, err)
	}
	return requests, nil
}

// ListMentorsWithStaleInProgressRequests returns non-declined mentors that
// have requests stuck in 'contacted'/'working' with no status update for
// more than 120 hours. Mirrors the mentors query in
// update-status-reminder/index.ts verbatim.
func (r *Repository) ListMentorsWithStaleInProgressRequests(ctx context.Context) ([]JobMentor, error) {
	query := `
		SELECT m.id, m.name, COALESCE(m.email::text, '')
		FROM mentors m
		INNER JOIN client_requests cr ON cr.mentor_id = m.id
		WHERE
			m.status != 'declined'
			AND cr.status_changed_at < NOW() - INTERVAL '120 hours'
			AND cr.status IN ('contacted', 'working')
		GROUP BY m.id, m.name, m.email
		HAVING COUNT(cr.id) > 0
	`
	mentors, err := r.listJobMentors(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list mentors with stale in-progress requests: %w", err)
	}
	return mentors, nil
}

// ListStaleInProgressRequests returns a mentor's 'contacted'/'working'
// requests whose status has not changed for more than 120 hours, oldest
// status change first. Mirrors the per-mentor requests query in
// update-status-reminder/index.ts: the days-since-last-update value is
// aliased created_days_ago (from status_changed_at, NOT created_at) so it
// flows into the same DaysAgo field the reminder wording uses.
func (r *Repository) ListStaleInProgressRequests(ctx context.Context, mentorID string) ([]JobReminderRequest, error) {
	query := `
		SELECT cr.id, cr.name, COALESCE(cr.description, ''), cr.status,
			EXTRACT(DAY FROM NOW() - cr.status_changed_at)::int AS created_days_ago
		FROM client_requests cr
		WHERE cr.mentor_id = $1
			AND cr.status IN ('contacted', 'working')
			AND cr.status_changed_at < NOW() - INTERVAL '120 hours'
		ORDER BY cr.status_changed_at ASC
	`
	requests, err := r.listJobReminderRequests(ctx, query, mentorID)
	if err != nil {
		return nil, fmt.Errorf("failed to list stale in-progress requests for mentor %s: %w", mentorID, err)
	}
	return requests, nil
}

// ListMentorsToDeactivate returns active mentors with requests pending for
// more than 30 days. Mirrors the query in
// deactivate-pending-mentors/index.ts verbatim.
func (r *Repository) ListMentorsToDeactivate(ctx context.Context) ([]JobMentor, error) {
	query := `
		SELECT m.id, m.name, COALESCE(m.email::text, '')
		FROM mentors m
			INNER JOIN client_requests cr ON cr.mentor_id = m.id
		WHERE m.status = 'active'
			AND cr.status = 'pending'
			AND (cr.created_at < NOW() - INTERVAL '30 days')
		GROUP BY m.id, m.name, m.email
		HAVING COUNT(cr.id) > 0
	`
	mentors, err := r.listJobMentors(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list mentors to deactivate: %w", err)
	}
	return mentors, nil
}

// DeactivateMentor sets a mentor's status to 'inactive'. Mirrors the UPDATE
// in deactivate-pending-mentors/index.ts verbatim (deliberately no
// updated_at touch, unlike SetMentorStatus).
func (r *Repository) DeactivateMentor(ctx context.Context, mentorID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE mentors SET status = 'inactive' WHERE id = $1`,
		mentorID,
	)
	if err != nil {
		return fmt.Errorf("failed to deactivate mentor %s: %w", mentorID, err)
	}
	return nil
}

// ListActiveMentorIDs returns the ids of all catalog-visible mentors.
// Mirrors randomize-sort-order/index.ts:
// SELECT id FROM mentors WHERE status = 'active'.
func (r *Repository) ListActiveMentorIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM mentors WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("failed to list active mentors: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan active mentor id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list active mentors: %w", err)
	}
	return ids, nil
}

// SetSortOrders applies all sort_order updates in ONE transaction, in the
// given order. Mirrors randomize-sort-order/index.ts: BEGIN, one
// 'UPDATE mentors SET sort_order = $1 WHERE id = $2' per row, COMMIT
// (ROLLBACK on any failure). Callers put the highlighted-mentor pins last
// so they overwrite the random orders, exactly like the func.
func (r *Repository) SetSortOrders(ctx context.Context, updates []SortOrderUpdate) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin sort order transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	for _, u := range updates {
		if _, err := tx.Exec(ctx,
			`UPDATE mentors SET sort_order = $1 WHERE id = $2`,
			u.SortOrder, u.MentorID,
		); err != nil {
			return fmt.Errorf("failed to set sort order for mentor %s: %w", u.MentorID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit sort order transaction: %w", err)
	}
	return nil
}

// GetJobReviewByID fetches a review joined with its client request (mentee
// name, mentor id, request id). Mirrors process-mentee-review/index.ts.
func (r *Repository) GetJobReviewByID(ctx context.Context, reviewID string) (*JobReview, error) {
	query := `
		SELECT r.id, cr.id AS request_id, cr.mentor_id, cr.name AS mentee_name,
			COALESCE(r.mentor_review, '')
		FROM reviews r
		JOIN client_requests cr ON cr.id = r.client_request_id
		WHERE r.id = $1
	`

	var rv JobReview
	err := r.pool.QueryRow(ctx, query, reviewID).Scan(
		&rv.ID, &rv.RequestID, &rv.MentorID, &rv.MenteeName, &rv.ReviewText,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch review %s: %w", reviewID, err)
	}
	return &rv, nil
}

// ListMentorsForCutout returns mentors eligible for the photo-cutout backfill:
// those on a public status (active/inactive) that could have a profile photo
// in object storage. Draft/pending/declined are excluded (not public, and
// their photos may still be churning).
func (r *Repository) ListMentorsForCutout(ctx context.Context) ([]CutoutMentor, error) {
	const query = `
		SELECT id, slug
		FROM mentors
		WHERE status IN ('active', 'inactive')
		ORDER BY created_at
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list mentors for cutout: %w", err)
	}
	defer rows.Close()

	var mentors []CutoutMentor
	for rows.Next() {
		var m CutoutMentor
		if err := rows.Scan(&m.ID, &m.Slug); err != nil {
			return nil, fmt.Errorf("failed to scan cutout mentor: %w", err)
		}
		mentors = append(mentors, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cutout mentors: %w", err)
	}
	return mentors, nil
}

// GetMentorForCutout fetches one mentor's (id, slug) for the single-mentor
// cutout job. Returns (nil, nil) when the mentor does not exist so the handler
// can map it to a 404. Unlike ListMentorsForCutout it applies no status filter:
// the caller passes an explicit id to (re)process a specific known mentor.
func (r *Repository) GetMentorForCutout(ctx context.Context, mentorID string) (*CutoutMentor, error) {
	var m CutoutMentor
	err := r.pool.QueryRow(ctx,
		`SELECT id, COALESCE(slug, '') FROM mentors WHERE id = $1`,
		mentorID,
	).Scan(&m.ID, &m.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mentor %s for cutout: %w", mentorID, err)
	}
	return &m, nil
}

// SetPhotoStyle updates a mentor's photo_style (bumping updated_at so the
// frontend image cache-buster picks up the new hero asset).
func (r *Repository) SetPhotoStyle(ctx context.Context, mentorID, style string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE mentors SET photo_style = $1, updated_at = NOW() WHERE id = $2`,
		style, mentorID,
	)
	if err != nil {
		return fmt.Errorf("failed to set photo_style for mentor %s: %w", mentorID, err)
	}
	return nil
}
