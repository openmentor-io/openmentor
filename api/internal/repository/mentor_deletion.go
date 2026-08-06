package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openmentor-io/openmentor/api/internal/models"
)

var (
	// ErrMentorAlreadyDeleted reports that the profile was already deleted when
	// the delete was attempted. Distinct from "no such mentor" so a double
	// submit reads as a duplicate rather than a vanished account.
	ErrMentorAlreadyDeleted = errors.New("mentor profile is already deleted")

	// ErrMentorNotDeleted reports a restore aimed at a profile that is not
	// deleted. It is not a silent no-op: a restore that matched nothing means
	// the admin is looking at stale state, and answering "done" would confirm
	// a change that never happened.
	ErrMentorNotDeleted = errors.New("mentor profile is not deleted")
)

// SoftDeleteMentor marks a profile deleted (D70) and, in the SAME transaction,
// tears down everything that could still act on the mentor's behalf:
//
//   - status goes to 'inactive', so every branch that predates deletion treats
//     the profile as hidden even if it never learned to read deleted_at;
//   - login_token is cleared, so a magic link already sitting in the mentor's
//     inbox is dead on arrival rather than good until it expires;
//   - session_version is bumped, which revokes every live portal session (D58)
//     — without this the mentor keeps write access for up to 24 hours after
//     deleting their own profile;
//   - outstanding review invitations for the mentor's requests are revoked, so
//     links already mailed to mentees stop resolving.
//
// One transaction because these are not independent facts: a profile marked
// deleted whose sessions survived, or whose review links still work, is exactly
// the half-deleted state the feature exists to prevent. Returns the number of
// review invitations revoked, for the caller's telemetry.
func (r *MentorRepository) SoftDeleteMentor(ctx context.Context, mentorID string) (invitationsRevoked int, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin delete transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // no-op once Commit succeeded
	}()

	// The compare-and-swap on deleted_at IS NULL is what makes the delete
	// exactly-once: two concurrent submits serialize on the row and the loser
	// matches nothing, so the invitation revoke below cannot run twice.
	var applied bool
	err = tx.QueryRow(ctx, `
		UPDATE mentors
		SET deleted_at = NOW(),
			status = 'inactive',
			login_token = NULL,
			login_token_expires_at = NULL,
			session_version = session_version + 1,
			updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING true
	`, mentorID).Scan(&applied)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either no such mentor or already deleted. Tell them apart with a
		// follow-up read rather than reading before the write — the write has to
		// stay the compare-and-swap for the concurrency argument above to hold.
		// On the transaction, not the pool: pgx's ErrNoRows is "the statement ran
		// and matched nothing", not a SQL error, so tx is still usable — and
		// reusing it avoids taking a second pooled connection while this one is
		// still open.
		var exists bool
		if probeErr := tx.QueryRow(ctx,
			`SELECT true FROM mentors WHERE id = $1`, mentorID,
		).Scan(&exists); probeErr != nil {
			if errors.Is(probeErr, pgx.ErrNoRows) {
				return 0, models.ErrSessionSubjectGone
			}
			return 0, fmt.Errorf("failed to classify failed delete: %w", probeErr)
		}
		return 0, ErrMentorAlreadyDeleted
	}
	if err != nil {
		return 0, fmt.Errorf("failed to delete mentor profile: %w", err)
	}

	// Revoke, not consume: consumed_at means a review was submitted through the
	// link, and conflating the two would corrupt the outstanding-invitation
	// count the H4 cutover is gated on (D4).
	tag, err := tx.Exec(ctx, `
		UPDATE review_invitations ri
		SET revoked_at = NOW()
		FROM client_requests cr
		WHERE ri.client_request_id = cr.id
			AND cr.mentor_id = $1
			AND ri.consumed_at IS NULL
			AND ri.revoked_at IS NULL
	`, mentorID)
	if err != nil {
		return 0, fmt.Errorf("failed to revoke review invitations: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit profile deletion: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// RestoreMentor clears the deletion marker, bringing the profile back as
// 'inactive' — hidden from the catalog, with its owner able to log in again and
// decide whether to publish it. Never straight back to 'active': the profile was
// off the site while it was deleted, and re-publishing it as a side effect of an
// admin clicking Restore is not a decision the admin was asked to make. Status
// is set explicitly rather than trusted to still be 'inactive' from the delete.
//
// Review invitations revoked by the delete stay revoked. They were mailed for
// sessions that ended before the profile disappeared, and un-revoking a bearer
// capability is not something a restore should silently do.
func (r *MentorRepository) RestoreMentor(ctx context.Context, mentorID string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE mentors
		SET deleted_at = NULL, status = 'inactive', updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NOT NULL
	`, mentorID)
	if err != nil {
		return fmt.Errorf("failed to restore mentor profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMentorNotDeleted
	}
	return nil
}

// IsMentorDeleted reports whether a profile carries the deletion marker.
// Returns models.ErrSessionSubjectGone when there is no such mentor.
func (r *MentorRepository) IsMentorDeleted(ctx context.Context, mentorID string) (bool, error) {
	var deleted bool
	err := r.pool.QueryRow(ctx,
		`SELECT deleted_at IS NOT NULL FROM mentors WHERE id = $1`, mentorID,
	).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, models.ErrSessionSubjectGone
	}
	if err != nil {
		return false, fmt.Errorf("failed to read mentor deletion state: %w", err)
	}
	return deleted, nil
}
