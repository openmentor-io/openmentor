package repository

import (
	"context"
	"fmt"
	"time"

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

func (r *ModeratorRepository) GetByLoginToken(ctx context.Context, token string) (*models.Moderator, time.Time, error) {
	query := `
		SELECT id, name, email, role, login_token_expires_at
		FROM moderators
		WHERE login_token = $1
		LIMIT 1
	`

	var moderator models.Moderator
	var role string
	var expiresAt *time.Time
	// SECURITY: tokens are stored hashed (L1); look up by hash.
	if err := r.pool.QueryRow(ctx, query, HashLoginToken(token)).Scan(
		&moderator.ID,
		&moderator.Name,
		&moderator.Email,
		&role,
		&expiresAt,
	); err != nil {
		return nil, time.Time{}, err
	}

	if expiresAt == nil {
		return nil, time.Time{}, fmt.Errorf("login token has no expiry")
	}

	moderator.Role = models.ModeratorRole(role)
	return &moderator, *expiresAt, nil
}

func (r *ModeratorRepository) SetLoginToken(ctx context.Context, moderatorID, token string, exp time.Time) error {
	query := `
		UPDATE moderators
		SET login_token = $1, login_token_expires_at = $2, updated_at = NOW()
		WHERE id = $3
	`
	// SECURITY: store the hash, never the plaintext token (L1).
	_, err := r.pool.Exec(ctx, query, HashLoginToken(token), exp, moderatorID)
	return err
}

func (r *ModeratorRepository) ClearLoginToken(ctx context.Context, moderatorID string) error {
	query := `
		UPDATE moderators
		SET login_token = NULL, login_token_expires_at = NULL, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, moderatorID)
	return err
}
