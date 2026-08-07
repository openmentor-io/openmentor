package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// This file holds the worker's own data access layer. The API's
// internal/repository types are shaped around the public API (public field
// filtering, hidden email/contact columns), while the job
// handlers ported from openmentor-func need raw rows including email,
// preferred_contact and login token columns. The SQL below mirrors the
// queries in openmentor-func/lib/utils/db.ts and each function's index.ts.
//
// That rationale is about ROW SHAPES. Where a statement has no shape to
// disagree about, this file calls internal/repository instead of copying it —
// see CreateReviewInvitation, the one write both sides make.

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
	// DeletedAt is set only by GetJobMentorByIDIncludingDeleted — the ordinary
	// lookup filters deleted profiles out, so it is always nil there (D70).
	// The deletion-notice jobs use it as their replay guard.
	DeletedAt *time.Time
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
	// ExpectedEmailConfirmationToken is the token the row carried when it was
	// read ("" for none). FinalizeNewMentor claims only a row still holding
	// exactly this value, which is what makes concurrent claims exclusive.
	ExpectedEmailConfirmationToken string
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
	GetJobMentorByIDIncludingDeleted(ctx context.Context, mentorID string) (*JobMentor, error)
	CountActiveMentorsByEmail(ctx context.Context, email, excludeMentorID string) (int, error)
	FinalizeNewMentor(ctx context.Context, params FinalizeNewMentorParams) (applied bool, err error)
	ReleaseNewMentorFinalization(ctx context.Context, params FinalizeNewMentorParams) error
	GetJobRequestByID(ctx context.Context, requestID string) (*JobRequest, error)
	GetJobRequestWithMentorName(ctx context.Context, requestID string) (*JobRequest, error)
	SetRequestContactPending(ctx context.Context, requestID, contact string) (applied bool, err error)
	GetJobModeratorByID(ctx context.Context, moderatorID string) (*JobModerator, error)
	GetJobReviewByID(ctx context.Context, reviewID string) (*JobReview, error)

	// CreateReviewInvitation persists a minted review capability (H4). Called
	// BEFORE the email carrying the raw token is sent, because a token that was
	// not stored must never be mailed.
	CreateReviewInvitation(ctx context.Context, requestID, tokenHash string, expiresAt time.Time) error

	// Cron job queries (stage 3, timer-triggered functions).
	ListMentorsWithStalePendingRequests(ctx context.Context) ([]JobMentor, error)
	ListStalePendingRequests(ctx context.Context, mentorID string) ([]JobReminderRequest, error)
	ListMentorsWithStaleInProgressRequests(ctx context.Context) ([]JobMentor, error)
	ListStaleInProgressRequests(ctx context.Context, mentorID string) ([]JobReminderRequest, error)
	ListMentorsToDeactivate(ctx context.Context) ([]JobMentor, error)
	ListStuckDraftRegistrations(ctx context.Context) ([]JobMentor, error)
	DeactivateMentor(ctx context.Context, mentorID string) (applied bool, err error)
	ListActiveMentorIDs(ctx context.Context) ([]string, error)
	SetSortOrders(ctx context.Context, updates []SortOrderUpdate) error

	// Profile purge (D70): list the deleted profiles whose retention window has
	// closed, then erase one and everything attached to it.
	ListPurgeableProfiles(ctx context.Context, retentionDays int) ([]PurgeableProfile, error)
	PurgeProfile(ctx context.Context, mentorID string, retentionDays int) (PurgeCounts, error)

	// AcquireJobLease is the cron mutual-exclusion primitive (D82).
	AcquireJobLease(ctx context.Context, job string) (JobLease, error)
}

// jobLeaseNamespace is the first key of every cron advisory lock. Postgres
// advisory locks share one cluster-wide space, so the two-int form namespaces
// ours away from anything else that takes one — notably dbtest's schema
// bootstrap lock and migrate-mentors.js's per-slug reservation.
const jobLeaseNamespace = 0x6f6d6362 // "ombc": openmentor background cron

// JobLease is the outcome of asking for a cron job's lease. A lease that was not
// Acquired means somebody else is running that job right now; Release is safe to
// call on it and does nothing.
type JobLease struct {
	Acquired bool
	release  func()
}

// Release drops the lease. Always call it (defer) on a lease you acquired: the
// underlying advisory lock is session-scoped, so it also disappears if the
// process dies, but only when the connection actually closes.
func (l JobLease) Release() {
	if l.release != nil {
		l.release()
	}
}

// Repository is the pgx-backed JobsRepository implementation.
type Repository struct {
	pool *pgxpool.Pool
	// reviews is borrowed from the API's data access layer for the one statement
	// both sides issue (CreateReviewInvitation). It is built on the SAME pool, so
	// nothing about which identity the worker connects as changes.
	reviews *repository.ReviewRepository
}

// NewRepository builds the worker's data access layer on the worker DB pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, reviews: repository.NewReviewRepository(pool)}
}

// AcquireJobLease takes a session-scoped Postgres advisory lock named after the
// job, so a scheduled tick and a manual POST /jobs/cron/<name> — or two worker
// processes, if one is ever run alongside another — cannot both be inside the
// same job. pg_try_advisory_lock never waits: the loser is told so and skips,
// which is the desired behavior for a job that will run again on its schedule.
//
// The lock lives on a DEDICATED connection, not one borrowed from the worker
// pool. A session lock has to be held for the whole run, so borrowing would pin
// one of WORKER_DB_MAX_CONNS (5) connections per concurrently-leased job for
// minutes at a time — and update-status-reminder and deactivate-pending-mentors
// share a schedule, so that starves the pool the jobs themselves are querying
// through. One extra short-lived connection per run is the cheaper side of that
// trade.
func (r *Repository) AcquireJobLease(ctx context.Context, job string) (JobLease, error) {
	conn, err := pgx.ConnectConfig(ctx, r.pool.Config().ConnConfig.Copy())
	if err != nil {
		return JobLease{}, fmt.Errorf("open lease connection: %w", err)
	}

	var acquired bool
	err = conn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock($1, hashtext($2))`, jobLeaseNamespace, job,
	).Scan(&acquired)
	if err != nil || !acquired {
		closeLeaseConn(conn, job)
		if err != nil {
			return JobLease{}, fmt.Errorf("try advisory lock: %w", err)
		}
		return JobLease{}, nil
	}

	return JobLease{Acquired: true, release: func() {
		// Closing the connection releases the lock; the explicit unlock is not
		// required, and skipping it keeps release from needing a live context
		// (the run's ctx may already be canceled by shutdown when we get here).
		closeLeaseConn(conn, job)
	}}, nil
}

func closeLeaseConn(conn *pgx.Conn, job string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Close(ctx); err != nil {
		logger.Warn("Failed to close cron lease connection",
			zap.String("job", job),
			logger.RedactedError(err),
		)
	}
}

// GetJobMentorByID fetches a mentor row by uuid.
// Mirrors: SELECT * FROM mentors WHERE id = $1 (func app), plus the deletion
// guard below.
//
// A DELETED profile (D70) reads as absent, and that is the single edit that
// stops the worker emailing a mentor who no longer exists. Every one of the
// eight jobs that reaches a mentor through this function does so in order to
// email them or act on their behalf — the login link, the confirmation, the
// moderation outcome, the new-request notification — and every one of them
// already branches on a nil mentor, because the row could always have been
// missing. Filtering HERE therefore reuses eight existing, tested "not found"
// paths instead of adding a deletion check to eight handlers, where the ninth
// would be the one that gets forgotten.
func (r *Repository) GetJobMentorByID(ctx context.Context, mentorID string) (*JobMentor, error) {
	return r.fetchJobMentor(ctx, mentorID, false)
}

// GetJobMentorByIDIncludingDeleted is the ONE deliberate exception to the filter
// above: it returns the mentor whether or not the profile is deleted.
//
// Exactly two callers may use it — the profile-deleted and profile-restored
// notices (D70) — because those emails are ABOUT the deletion, so refusing to
// load a deleted profile would make the confirmation unsendable. Anything else
// that wants a mentor wants to act on a live one and must use GetJobMentorByID.
// It is a separate method rather than a boolean on the existing one so that
// "email a deleted mentor" is a decision somebody typed out, and so the callers
// are one grep away.
func (r *Repository) GetJobMentorByIDIncludingDeleted(ctx context.Context, mentorID string) (*JobMentor, error) {
	return r.fetchJobMentor(ctx, mentorID, true)
}

func (r *Repository) fetchJobMentor(ctx context.Context, mentorID string, includeDeleted bool) (*JobMentor, error) {
	deletionGuard := "AND deleted_at IS NULL"
	if includeDeleted {
		deletionGuard = ""
	}
	query := `
		SELECT id, legacy_id, name, COALESCE(email::text, ''), status,
			COALESCE(preferred_contact, ''), COALESCE(slug, ''), COALESCE(job_title, ''),
			COALESCE(workplace, ''), COALESCE(price, ''), COALESCE(calendar_url, ''),
			COALESCE(moderation_note, ''), COALESCE(email_confirmation_token, ''),
			deleted_at
		FROM mentors
		WHERE id = $1 ` + deletionGuard

	var m JobMentor
	err := r.pool.QueryRow(ctx, query, mentorID).Scan(
		&m.ID, &m.LegacyID, &m.Name, &m.Email, &m.Status,
		&m.PreferredContact, &m.Slug, &m.JobTitle,
		&m.Workplace, &m.Price, &m.CalendarURL,
		&m.ModerationNote, &m.EmailConfirmationToken,
		&m.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mentor %s: %w", mentorID, err)
	}
	return &m, nil
}

// CountActiveMentorsByEmail counts OTHER mentors with the same email and status
// 'active'. Mirrors findDuplicates() in new-mentor-watcher/index.ts
// (SELECT * FROM mentors WHERE email = $1 AND status = 'active').
//
// excludeMentorID is the registration being finalized. Without it a mentor who is
// already active counts as their OWN duplicate, so a replayed
// new-mentor-watcher against a live profile concludes "duplicate" and heads for
// 'declined'. FinalizeNewMentor's status = 'draft' claim refuses that write
// today, but the count also decides which email the job sends and feeds the
// duplicates_count analytics property, so the exclusion belongs here rather than
// resting on a downstream guard.
func (r *Repository) CountActiveMentorsByEmail(ctx context.Context, email, excludeMentorID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM mentors WHERE email = $1 AND status = 'active' AND id <> $2`,
		email, excludeMentorID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count duplicate mentors: %w", err)
	}
	return count, nil
}

// FinalizeNewMentor performs the single UPDATE from new-mentor-watcher:
// trimmed fields, login token, slug, status, randomized sort order and the
// email confirmation token (draft-status workflow).
//
// applied reports whether this caller won the row and therefore actually wrote
// it. It is an EXCLUSIVE CLAIM, not a blind write: the caller must not email a
// confirmation token this returned false for, because that token was not stored.
func (r *Repository) FinalizeNewMentor(ctx context.Context, p FinalizeNewMentorParams) (applied bool, err error) {
	// SECURITY: new registrations get NO usable login token (L2). A token is
	// only ever minted on demand by RequestLogin; leaving a standing long-lived
	// credential here would widen the blast radius of a DB leak.
	// slug is only FILLED when still empty (the generate-when-missing fallback);
	// it never overwrites an existing slug. A draft mentor can log in and rename
	// themselves, and finalization carries the slug value read at the start of
	// this job — writing it unconditionally would clobber a concurrent rename
	// and leave a dangling history row. The CASE preserves whatever slug the row
	// currently holds when it's already set.
	//
	// status = 'draft' in the WHERE keeps a mentor who confirms (or a moderator
	// who acts) between the read and this write from being pushed back to draft
	// with a fresh token. Both callers only ever finalize a draft row, so this
	// excludes nothing legitimate.
	//
	// The status check ALONE does not make the claim exclusive, because for a
	// non-duplicate registration the target status ($4) is 'draft' too: the row
	// still matches after the first claim commits, so an overlapping cron pass or
	// a redelivered POST /jobs/new-mentor-watcher would claim it again, overwrite
	// the token the first caller already emailed, and email a second link. Two
	// more conditions close that:
	//
	//   - compare-and-swap on the token READ before this call ($9): concurrent
	//     claimers all see the same value, so only the first write can match it
	//     (Postgres re-evaluates the WHERE against the winner's committed row).
	//   - never touch a token whose window is still open: a caller that reads the
	//     row AFTER a successful claim would legitimately pass the CAS, and
	//     rewriting the token then invalidates a link that is already in the
	//     mentor's inbox. Fresh registrations carry no token, and the replay only
	//     lists rows whose token has expired, so no legitimate claim is blocked.
	query := `
		UPDATE mentors SET
			name = $1,
			preferred_contact = $2,
			login_token = NULL,
			login_token_expires_at = NULL,
			slug = CASE WHEN mentors.slug IS NULL OR mentors.slug = '' THEN $3 ELSE mentors.slug END,
			status = $4,
			sort_order = $5,
			email_confirmation_token = $6,
			email_confirmation_expires_at = $7,
			updated_at = NOW()
		WHERE id = $8
			AND status = 'draft'
			AND COALESCE(email_confirmation_token, '') = $9
			AND (email_confirmation_expires_at IS NULL OR email_confirmation_expires_at <= NOW())
	`
	tag, err := r.pool.Exec(ctx, query,
		p.Name, p.PreferredContact,
		p.Slug, p.Status, p.SortOrder,
		p.EmailConfirmationToken, p.EmailConfirmationExpiresAt, p.MentorID,
		p.ExpectedEmailConfirmationToken,
	)
	if err != nil {
		return false, fmt.Errorf("failed to finalize new mentor %s: %w", p.MentorID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// ReleaseNewMentorFinalization undoes the claim FinalizeNewMentor made, putting
// the row back into the shape the INSERT left (draft, no sort_order, no
// confirmation token) so finalize-stuck-registrations picks it up again on its
// next pass. new-mentor-watcher calls it when the email it claimed the row for
// could not be sent.
//
// Compare-and-swap on our own write: it matches only a row that still carries
// the exact status, sort_order and token this job wrote, so a moderator action or
// a confirmation that landed in between is never reverted. Together with the
// exclusive claim in FinalizeNewMentor that also makes it caller-private — while
// this row carries our claim nobody else can have finalized it, so this can only
// ever undo our own write, never someone else's.
func (r *Repository) ReleaseNewMentorFinalization(ctx context.Context, p FinalizeNewMentorParams) error {
	token := ""
	if p.EmailConfirmationToken != nil {
		token = *p.EmailConfirmationToken
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE mentors SET
			status = 'draft',
			sort_order = NULL,
			email_confirmation_token = NULL,
			email_confirmation_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $1
			AND status = $2
			AND sort_order = $3
			AND COALESCE(email_confirmation_token, '') = $4
	`, p.MentorID, p.Status, p.SortOrder, token)
	if err != nil {
		return fmt.Errorf("failed to release finalization of mentor %s: %w", p.MentorID, err)
	}
	return nil
}

// There is deliberately NO mentor-status write on this interface. The worker used
// to carry one for the moderation job's "idempotency" repair, which reconciled
// the row toward whatever the callback payload said — so a redelivered decline
// landing after a newer approve flipped a live mentor to 'declined'. The API
// writes the status before it fires the trigger and aborts if that write fails
// (internal/services/admin_mentors_service.go), so the worker never had a
// legitimate reason to write it, and the absence of the method is what keeps that
// bug from being reintroduced. See MentorModerationAction.

// JobRequest is all plain strings and pgx fails the WHOLE row scan on a NULL in
// a non-pointer destination, so every nullable column here is COALESCEd — an
// un-COALESCEd one would leave the job unable to read that request at all.
// mentor_id is nullable because the FK is ON DELETE SET NULL; the empty string
// makes the mentor lookup miss, which the handlers already report as "mentor
// not found".
const jobRequestColumns = `
	cr.id, COALESCE(cr.mentor_id::text, ''), COALESCE(cr.name, ''),
	COALESCE(cr.email::text, ''),
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
		// The id is deliberately NOT in the message: a client_requests id is the
		// bearer capability of the review flow, and an error travels to sinks the
		// call site cannot redact (PostHog error tracking, a wrapping error).
		// Every caller logs the hashed request_ref, which identifies the row.
		return nil, fmt.Errorf("failed to fetch client request: %w", err)
	}
	return &req, nil
}

// GetJobRequestWithMentorName fetches a client request joined with the
// mentor's name. Mirrors request-process-finished/index.ts:
// SELECT cr.*, m.name AS mentor_name FROM client_requests cr
// JOIN mentors m ON m.id = cr.mentor_id WHERE cr.id = $1 (inner join: a
// request whose mentor row is gone reads as "not found", like the func).
//
// The join carries the same deleted_at guard as GetJobMentorByID (D70): this is
// the one job path that reaches a mentor WITHOUT that lookup, and skipping the
// guard here would let a trigger retried after the deletion mint a fresh review
// invitation and mail the mentee a link naming the deleted mentor. The link
// would die at the eligibility check, but the email itself is exactly what the
// deletion lockout exists to stop — and the unspendable invitation row would
// pollute the outstanding-invitation count the H4 cutover is gated on (D4).
func (r *Repository) GetJobRequestWithMentorName(ctx context.Context, requestID string) (*JobRequest, error) {
	query := `SELECT ` + jobRequestColumns + `, m.name AS mentor_name
		FROM client_requests cr
		JOIN mentors m ON m.id = cr.mentor_id AND m.deleted_at IS NULL
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
		// Id omitted for the same reason as GetJobRequestByID.
		return nil, fmt.Errorf("failed to fetch client request with mentor: %w", err)
	}
	return &req, nil
}

// SetRequestContactPending stores the trimmed contact details and stamps the
// request as accepted into the 'pending' queue. Mirrors
// new-request-watcher/index.ts, plus the replay guard the func lacked.
//
// applied reports whether this caller claimed the request. It is a ONE-SHOT
// CLAIM, not a blind write: the caller must not email anyone about a request this
// returned false for, because the state those emails describe is not the state
// the row is in.
//
// status_changed_at IS NULL is the claim marker, and it needs no new column: the
// INSERT (internal/repository/client_request_repository.go) creates the row
// 'pending' with status_changed_at NULL and never sets it, while every later
// status write goes through UpdateStatus/UpdateDecline, which always stamp it. So
// a NULL means "no one has moved this request yet" and this UPDATE is what fills
// it — which makes both failure modes impossible at once:
//
//   - A replay after the mentor (or the mentee) moved the request on writes
//     nothing. Without the guard the blind status = 'pending' silently REOPENED a
//     declined or completed request, and because it left status_changed_at at the
//     older transition, the reopened request also read as stale to the reminder
//     jobs. (updated_at DID move even before this change — trg_client_requests_updated_at
//     from migration 000001 is a BEFORE UPDATE trigger — so the row was not
//     entirely traceless, just misleading. It is set explicitly anyway, like every
//     other write here.)
//   - A second delivery while the request is still untouched writes nothing
//     either, so the mentee/mentor/moderator emails go out exactly once.
//
// Two concurrent callers therefore produce exactly one winner: Postgres
// re-evaluates the WHERE against the winner's committed row, whose
// status_changed_at is no longer NULL.
func (r *Repository) SetRequestContactPending(ctx context.Context, requestID, contact string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE client_requests
		SET preferred_contact = $1, status = 'pending',
			status_changed_at = NOW(), updated_at = NOW()
		WHERE id = $2
			AND status = 'pending'
			AND status_changed_at IS NULL
	`, contact, requestID)
	if err != nil {
		// Id omitted for the same reason as GetJobRequestByID.
		return false, fmt.Errorf("failed to update client request: %w", err)
	}
	return tag.RowsAffected() > 0, nil
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
		SELECT cr.id, COALESCE(cr.name, ''), COALESCE(cr.description, ''), cr.status,
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
			-- The only one of the cron mentor queries that needs this spelled
			-- out: it selects on "not declined" rather than "active", so a
			-- deleted profile (D70, status 'inactive') would otherwise match and
			-- be nagged about requests it can no longer see.
			AND m.deleted_at IS NULL
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
		SELECT cr.id, COALESCE(cr.name, ''), COALESCE(cr.description, ''), cr.status,
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

// ListStuckDraftRegistrations returns draft registrations that hold no usable
// confirmation link, which is a dead end for the mentor: without the token from
// that link even /mentors/confirm/resend cannot help them, and no other job
// touches the row (randomize-sort-order only selects status='active').
//
// Two shapes qualify, and they need different age rules:
//
//   - Finalization never ran, or ran and released its claim: NULL sort_order and
//     no token, the shape the INSERT leaves. The API dispatches finalization
//     through a bare goroutine (pkg/trigger.CallAsync) with no persistence and no
//     retry, so a lost HTTP call lands here. Unambiguous — that mentor was never
//     emailed anything — so it is replayed at any age.
//   - A token was issued but its 24h window has passed. Either the email never
//     went out (finalization writes the token before it sends, so a send failure
//     whose release also failed leaves the row like this) or it went out and was
//     never clicked; nothing in the schema distinguishes them without a new
//     column, which Phase 0 cannot add. So re-issue, but only within 3 days of
//     signing up: at a 24h TTL that is at most two extra emails, after which an
//     abandoned registration is left alone instead of being nagged forever.
//
// A mentor a moderator returned to draft matches neither leg (they confirmed
// once, so their token is NULL, and finalization already set their sort_order) —
// they must not be asked to confirm their email again.
//
// activated_at IS NULL keeps a formerly-live profile out of the replay:
// finalization can DECLINE a duplicate email, which must never happen to a
// mentor who has already been active.
func (r *Repository) ListStuckDraftRegistrations(ctx context.Context) ([]JobMentor, error) {
	query := `
		SELECT m.id, m.name, COALESCE(m.email::text, '')
		FROM mentors m
		WHERE m.status = 'draft'
			AND m.activated_at IS NULL
			AND m.created_at < NOW() - INTERVAL '10 minutes'
			AND (
				(m.sort_order IS NULL AND m.email_confirmation_token IS NULL)
				OR (m.email_confirmation_token IS NOT NULL
					AND m.email_confirmation_expires_at < NOW()
					AND m.created_at > NOW() - INTERVAL '3 days')
			)
		ORDER BY m.created_at ASC
	`
	mentors, err := r.listJobMentors(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list stuck draft registrations: %w", err)
	}
	return mentors, nil
}

// DeactivateMentor sets a mentor's status to 'inactive'. Mirrors the UPDATE in
// deactivate-pending-mentors/index.ts, plus the guard the func lacked.
//
// applied reports whether this caller actually deactivated the mentor; the caller
// must not send the "your profile was deactivated" email when it is false,
// because the profile was not deactivated by this run.
//
// status = 'active' is what ListMentorsToDeactivate selected on, so re-checking it
// in the write closes the window between the two: a mentor who is declined,
// returned to draft or already inactive by the time the loop reaches them is left
// alone instead of being pulled sideways into 'inactive' — and, since the same
// predicate drives both, a second overlapping pass over the same list deactivates
// and emails each mentor exactly once.
//
// updated_at is now stamped explicitly, like every other write in this file. The
// func's "no updated_at touch" was never actually observable: mentors carries
// trg_mentors_updated_at, a BEFORE UPDATE trigger (migration 000001), so the
// column moved regardless. What the guard adds is that it stops moving on a
// write that changes nothing.
func (r *Repository) DeactivateMentor(ctx context.Context, mentorID string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE mentors SET status = 'inactive', updated_at = NOW()
		 WHERE id = $1 AND status = 'active'`,
		mentorID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to deactivate mentor %s: %w", mentorID, err)
	}
	return tag.RowsAffected() > 0, nil
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
		SELECT r.id, cr.id AS request_id, COALESCE(cr.mentor_id::text, '') AS mentor_id,
			COALESCE(cr.name, '') AS mentee_name,
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

// CreateReviewInvitation stores a minted review capability (H4).
//
// It DELEGATES rather than repeating the INSERT: the worker is the minting path,
// so a second copy here would be the copy that matters, and it had already
// drifted — the API-side implementation rejects a value that is not a 64-char
// digest before it reaches the database, this one relied on the CHECK constraint
// alone. One implementation means the guard, the column list and the
// append-never-update rule cannot diverge again. Sharing is possible because the
// method takes and returns nothing shaped by either package: the row shapes that
// justify the worker's separate data access layer (see the file header) do not
// apply to an insert of two strings and a timestamp.
//
// The pool is the WORKER's, so after H8 this still connects as om_worker.
func (r *Repository) CreateReviewInvitation(ctx context.Context, requestID, tokenHash string, expiresAt time.Time) error {
	return r.reviews.CreateReviewInvitation(ctx, requestID, tokenHash, expiresAt)
}
