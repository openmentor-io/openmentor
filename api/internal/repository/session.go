package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/tokenhash"
)

// ErrTokenNotConsumable reports that a one-time token matched no row that could
// still be consumed: it never existed, it was already used, or it expired. The
// three are deliberately indistinguishable — the consumption is a single UPDATE,
// and telling them apart would mean reading the row first, which is exactly the
// check-then-act this replaces.
var ErrTokenNotConsumable = errors.New("one-time token cannot be consumed")

// ErrSessionSubjectGone reports that the identity behind a live session no longer
// exists. Callers must fail the request closed. Defined in models so the session
// middleware can branch on it without importing this package.
var ErrSessionSubjectGone = models.ErrSessionSubjectGone

// ConsumedMentorLogin is the mentor row returned by a successful magic-link
// consumption. Deliberately narrow: every column in it is NOT NULL, so unlike a
// full mentor read it cannot fail on an un-COALESCEd nullable profile field — a
// mentor with a NULL price must not lose the ability to log in.
type ConsumedMentorLogin struct {
	MentorID       string
	LegacyID       int
	Name           string
	Status         string
	SessionVersion int
}

// ConsumeLoginToken atomically spends a mentor's magic-link token and returns
// the row it belonged to.
//
// SECURITY (H1): one UPDATE does the match, the expiry check and the clearing,
// so the token is gone before the caller can mint anything. The previous shape —
// SELECT by token, compare the expiry in Go, UPDATE to clear — had two defects
// this removes by construction:
//
//   - two concurrent verifications both passed the SELECT and both minted a
//     24-hour session from one link. Here Postgres re-evaluates the WHERE against
//     the winner's committed row, so the second UPDATE matches nothing.
//   - a failing clear was logged and the session minted anyway, leaving the link
//     reusable. Here there is no row to mint from unless the clear succeeded.
//
// Returns ErrTokenNotConsumable when nothing matched.
func (r *MentorRepository) ConsumeLoginToken(ctx context.Context, token string) (*ConsumedMentorLogin, error) {
	const query = `
		UPDATE mentors
		SET login_token = NULL, login_token_expires_at = NULL, updated_at = NOW()
		WHERE login_token = $1
			AND login_token_expires_at >= NOW()
		RETURNING id, legacy_id, name, status, session_version
	`

	var consumed ConsumedMentorLogin
	// SECURITY: tokens are stored hashed (L1); match by hash.
	err := r.pool.QueryRow(ctx, query, HashOneTimeToken(token)).Scan(
		&consumed.MentorID,
		&consumed.LegacyID,
		&consumed.Name,
		&consumed.Status,
		&consumed.SessionVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotConsumable
	}
	if err != nil {
		return nil, fmt.Errorf("failed to consume mentor login token: %w", err)
	}
	return &consumed, nil
}

// MentorSessionState reads the two fields a live mentor session is checked
// against: the current status and the revocation counter. One primary-key
// lookup, and narrow for the same reason as ConsumedMentorLogin.
func (r *MentorRepository) MentorSessionState(ctx context.Context, mentorID string) (status string, sessionVersion int, err error) {
	err = r.pool.QueryRow(ctx,
		`SELECT status, session_version FROM mentors WHERE id = $1`, mentorID,
	).Scan(&status, &sessionVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrSessionSubjectGone
	}
	if err != nil {
		return "", 0, fmt.Errorf("failed to read mentor session state: %w", err)
	}
	return status, sessionVersion, nil
}

// BumpMentorSessionVersion invalidates every session already issued for this
// mentor (D58). Called on logout, so a cookie copied off a shared machine stops
// working instead of outliving the logout by up to 24 hours.
func (r *MentorRepository) BumpMentorSessionVersion(ctx context.Context, mentorID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE mentors SET session_version = session_version + 1, updated_at = NOW() WHERE id = $1`,
		mentorID,
	)
	if err != nil {
		return fmt.Errorf("failed to bump mentor session version: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionSubjectGone
	}
	return nil
}

// confirmationTokenPredicate builds the "this row belongs to that confirmation
// token" match, at the given $n placeholder positions.
//
// Two positions because tokens issued before D57 are still stored in plaintext
// and their links live 24h; see tokenhash.IsLegacyPlaintext for why the fallback
// is prefix-scoped and when to delete it (#80). Once it goes, this collapses to
// a single equality and the helper can go with it.
func confirmationTokenPredicate(hashIdx, legacyIdx int) string {
	return fmt.Sprintf("(email_confirmation_token = $%d OR email_confirmation_token = $%d)",
		hashIdx, legacyIdx)
}

// confirmationTokenArgs returns the two values confirmationTokenPredicate
// expects. The legacy slot is nil for anything that is not a pre-D57 plaintext
// token, and `column = NULL` never matches — so the fallback disappears for
// newly issued tokens without a second query shape.
func confirmationTokenArgs(token string) (hash string, legacy any) {
	if tokenhash.IsLegacyPlaintext(token) {
		return tokenhash.Hash(token), token
	}
	return tokenhash.Hash(token), nil
}

// ConsumeConfirmationToken is the email-confirmation counterpart of
// ConsumeLoginToken: it moves the mentor draft -> pending and clears the token
// in ONE statement, so a double-clicked link produces exactly one transition and
// exactly one "application in review" email.
//
// applied=false means the row was no longer a confirmable draft — another caller
// won the race, a moderator moved it, or the window closed. The caller treats
// that as idempotent success and must NOT fire the follow-up trigger.
//
// The caller looks the mentor up first, which is what preserves the three-way
// invalid/expired/already contract the web client depends on; the atomicity lives
// in this WHERE, the same read-then-compare-and-swap shape as the worker's
// FinalizeNewMentor.
func (r *MentorRepository) ConsumeConfirmationToken(ctx context.Context, token string) (applied bool, err error) {
	hash, legacy := confirmationTokenArgs(token)
	query := `
		UPDATE mentors
		SET status = 'pending',
			email_confirmation_token = NULL,
			email_confirmation_expires_at = NULL,
			updated_at = NOW()
		WHERE ` + confirmationTokenPredicate(1, 2) + `
			AND status = 'draft'
			AND email_confirmation_expires_at >= NOW()
	`
	tag, err := r.pool.Exec(ctx, query, hash, legacy)
	if err != nil {
		return false, fmt.Errorf("failed to consume confirmation token: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// RotateConfirmationToken swaps a mentor's confirmation token for a fresh one,
// keyed on the token the caller read. Returns applied=false when the row moved
// under it, in which case the caller must NOT email the fresh token — it was
// never stored, so the link would be dead on arrival.
//
// This is also what makes "the old link stops working after a resend" a fact
// rather than a hope: the swap and the invalidation of the previous token are the
// same write.
func (r *MentorRepository) RotateConfirmationToken(
	ctx context.Context, oldToken, newToken string, expiresAt time.Time,
) (applied bool, err error) {

	oldHash, oldLegacy := confirmationTokenArgs(oldToken)
	query := `
		UPDATE mentors
		SET email_confirmation_token = $1,
			email_confirmation_expires_at = $2,
			updated_at = NOW()
		WHERE ` + confirmationTokenPredicate(3, 4) + `
			AND status = 'draft'
	`
	tag, err := r.pool.Exec(ctx, query, tokenhash.Hash(newToken), expiresAt, oldHash, oldLegacy)
	if err != nil {
		return false, fmt.Errorf("failed to rotate confirmation token: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
