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

type ModeratorRepository struct {
	pool *pgxpool.Pool
}

func NewModeratorRepository(pool *pgxpool.Pool) *ModeratorRepository {
	return &ModeratorRepository{pool: pool}
}

func (r *ModeratorRepository) GetByEmail(ctx context.Context, email string) (*models.Moderator, error) {
	query := `
		SELECT id, name, email, role
		FROM moderators
		WHERE email = $1
		LIMIT 1
	`

	var moderator models.Moderator
	var role string
	if err := r.pool.QueryRow(ctx, query, email).Scan(
		&moderator.ID,
		&moderator.Name,
		&moderator.Email,
		&role,
	); err != nil {
		return nil, err
	}

	moderator.Role = models.ModeratorRole(role)
	return &moderator, nil
}

// ConsumedModeratorLogin is the moderator row returned by a successful
// magic-link consumption.
type ConsumedModeratorLogin struct {
	ID             string
	Name           string
	Email          string
	Role           models.ModeratorRole
	SessionVersion int
}

// ConsumeLoginToken atomically spends a moderator's magic-link token. Same
// single-UPDATE shape and same reasoning as the mentor side (see
// MentorRepository.ConsumeLoginToken) — and it matters more here, because the
// session this mints is a privileged one: the old check-then-act could hand two
// concurrent clicks two 24-hour admin sessions from one link, and minted one
// anyway when the clearing UPDATE failed.
//
// moderators.email is nullable and no predicate here constrains it, so it is read
// through COALESCE (as ::text, like every other citext read in the codebase);
// without that a NULL would fail the scan and lock the moderator out.
//
// Returns ErrTokenNotConsumable when nothing matched.
func (r *ModeratorRepository) ConsumeLoginToken(ctx context.Context, token string) (*ConsumedModeratorLogin, error) {
	const query = `
		UPDATE moderators
		SET login_token = NULL, login_token_expires_at = NULL, updated_at = NOW()
		WHERE login_token = $1
			AND login_token_expires_at >= NOW()
		RETURNING id, name, COALESCE(email::text, ''), role, session_version
	`

	var consumed ConsumedModeratorLogin
	var role string
	// SECURITY: tokens are stored hashed (L1); match by hash.
	err := r.pool.QueryRow(ctx, query, HashOneTimeToken(token)).Scan(
		&consumed.ID,
		&consumed.Name,
		&consumed.Email,
		&role,
		&consumed.SessionVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotConsumable
	}
	if err != nil {
		return nil, fmt.Errorf("failed to consume moderator login token: %w", err)
	}
	consumed.Role = models.ModeratorRole(role)
	return &consumed, nil
}

// ModeratorSessionState reads the two fields a live admin session is checked
// against on EVERY request: the current role and the revocation counter. One
// primary-key lookup, so demoting or deleting a moderator takes effect on their
// next request instead of up to 24 hours later (D58).
func (r *ModeratorRepository) ModeratorSessionState(ctx context.Context, moderatorID string) (role string, sessionVersion int, err error) {
	err = r.pool.QueryRow(ctx,
		`SELECT role, session_version FROM moderators WHERE id = $1`, moderatorID,
	).Scan(&role, &sessionVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrSessionSubjectGone
	}
	if err != nil {
		return "", 0, fmt.Errorf("failed to read moderator session state: %w", err)
	}
	return role, sessionVersion, nil
}

// BumpModeratorSessionVersion invalidates every session already issued for this
// moderator (D58). Called on logout.
func (r *ModeratorRepository) BumpModeratorSessionVersion(ctx context.Context, moderatorID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE moderators SET session_version = session_version + 1, updated_at = NOW() WHERE id = $1`,
		moderatorID,
	)
	if err != nil {
		return fmt.Errorf("failed to bump moderator session version: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionSubjectGone
	}
	return nil
}

// SetLoginToken stores a fresh magic-link token. Consumption is a single atomic
// UPDATE (ConsumeLoginToken); there is deliberately no read-by-token and no
// separate clear.
func (r *ModeratorRepository) SetLoginToken(ctx context.Context, moderatorID, token string, exp time.Time) error {
	query := `
		UPDATE moderators
		SET login_token = $1, login_token_expires_at = $2, updated_at = NOW()
		WHERE id = $3
	`
	// SECURITY: store the hash, never the plaintext token (L1).
	_, err := r.pool.Exec(ctx, query, HashOneTimeToken(token), exp, moderatorID)
	return err
}
