package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PurgeableProfile identifies one deleted profile whose retention window has
// closed. Deliberately narrow: the purge job logs and counts, it does not email
// anybody, and the less PII it carries through a job that is about erasing PII
// the better. Name is kept out for the same reason — the id is what an operator
// needs to correlate with the analytics event.
type PurgeableProfile struct {
	MentorID  string
	DeletedAt time.Time
}

// PurgeCounts is what one profile's erasure removed, for the run summary.
type PurgeCounts struct {
	Requests    int
	Reviews     int
	Invitations int
}

// ListPurgeableProfiles returns the deleted profiles whose retention window has
// closed, oldest deletion first, so a run that is interrupted has erased the
// profiles that have been waiting longest.
//
// retentionDays is passed as an interval multiplier rather than interpolated
// into the SQL: `NOW() - ($1 || ' days')::interval` would work but builds the
// interval from a string, and make_interval takes it as a number.
func (r *Repository) ListPurgeableProfiles(ctx context.Context, retentionDays int) ([]PurgeableProfile, error) {
	if retentionDays <= 0 {
		// Refuse rather than defaulting: reaching here with 0 means config
		// validation was bypassed, and "purge everything deleted, right now" is
		// not a safe interpretation of a missing value.
		return nil, fmt.Errorf("purge retention must be a positive number of days, got %d", retentionDays)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, deleted_at
		FROM mentors
		WHERE deleted_at IS NOT NULL
			AND deleted_at < NOW() - make_interval(days => $1)
		ORDER BY deleted_at ASC
	`, retentionDays)
	if err != nil {
		return nil, fmt.Errorf("failed to list purgeable profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]PurgeableProfile, 0)
	for rows.Next() {
		var p PurgeableProfile
		if scanErr := rows.Scan(&p.MentorID, &p.DeletedAt); scanErr != nil {
			return nil, fmt.Errorf("failed to scan purgeable profile: %w", scanErr)
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// PurgeProfile erases one deleted profile and everything attached to it, in one
// transaction.
//
// The retention predicate is RE-EVALUATED here, against the row, rather than
// trusted from the listing that produced this id. Between the list and this call
// an admin may have restored the profile — this is the last moment that can be
// noticed, and the cost of noticing is one predicate. A restored profile simply
// matches nothing and is reported as skipped.
//
// Deletion order follows the foreign keys, and does NOT rely on the mentors FK
// alone:
//
//   - client_requests.mentor_id is ON DELETE SET NULL, so dropping the mentor
//     row would ORPHAN every request rather than remove it — the mentee's name,
//     email and message would survive the erasure of the mentor they were sent
//     to. Requests are therefore deleted explicitly, first.
//   - reviews and review_invitations both cascade from client_requests, so that
//     one DELETE takes them with it. They are counted first (a count after the
//     delete is always zero) so the summary says what was actually removed.
//   - mentor_tags and mentor_slug_history cascade from mentors.
//
// Not covered: the mentor's profile images in object storage. cmd/worker holds
// no S3 credential by design (see config.ValidateForWorker and the a57aec2
// note), so this transaction cannot delete them. They are keyed by mentor UUID
// and become unreferenced here; see the D70 row in DECISIONS.md.
func (r *Repository) PurgeProfile(ctx context.Context, mentorID string, retentionDays int) (PurgeCounts, error) {
	var counts PurgeCounts

	if retentionDays <= 0 {
		return counts, fmt.Errorf("purge retention must be a positive number of days, got %d", retentionDays)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return counts, fmt.Errorf("failed to begin purge transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // no-op once Commit succeeded

	// Claim the row under the retention predicate. FOR UPDATE holds it against
	// a concurrent restore for the rest of the transaction.
	var claimed string
	err = tx.QueryRow(ctx, `
		SELECT id FROM mentors
		WHERE id = $1
			AND deleted_at IS NOT NULL
			AND deleted_at < NOW() - make_interval(days => $2)
		FOR UPDATE
	`, mentorID, retentionDays).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return counts, ErrProfileNotPurgeable
	}
	if err != nil {
		return counts, fmt.Errorf("failed to claim profile for purge: %w", err)
	}

	// Count the cascade victims before the DELETE removes them.
	if err = tx.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM client_requests WHERE mentor_id = $1),
			(SELECT COUNT(*) FROM reviews rv
				JOIN client_requests cr ON cr.id = rv.client_request_id
				WHERE cr.mentor_id = $1),
			(SELECT COUNT(*) FROM review_invitations ri
				JOIN client_requests cr ON cr.id = ri.client_request_id
				WHERE cr.mentor_id = $1)
	`, mentorID).Scan(&counts.Requests, &counts.Reviews, &counts.Invitations); err != nil {
		return counts, fmt.Errorf("failed to count purge targets: %w", err)
	}

	// Requests first — the FK would otherwise null the mentor out and keep them.
	if _, err = tx.Exec(ctx, `DELETE FROM client_requests WHERE mentor_id = $1`, mentorID); err != nil {
		return counts, fmt.Errorf("failed to delete client requests: %w", err)
	}

	// Then the mentor, taking mentor_tags and mentor_slug_history with it.
	if _, err = tx.Exec(ctx, `DELETE FROM mentors WHERE id = $1`, mentorID); err != nil {
		return counts, fmt.Errorf("failed to delete mentor: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return counts, fmt.Errorf("failed to commit purge: %w", err)
	}
	return counts, nil
}

// ErrProfileNotPurgeable reports that the profile no longer qualified when the
// purge went to erase it — almost always an admin restore between the listing
// and the write. Not an error condition for the run: the guard did its job.
var ErrProfileNotPurgeable = errors.New("profile is no longer eligible for purge")
