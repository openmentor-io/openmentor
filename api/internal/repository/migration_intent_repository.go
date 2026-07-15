package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationIntentRepository stores getmentor.dev migration opt-ins
// (migration_intents table, consumed by infra/migration tooling).
type MigrationIntentRepository struct {
	pool *pgxpool.Pool
}

// NewMigrationIntentRepository creates a new migration intent repository.
func NewMigrationIntentRepository(pool *pgxpool.Pool) *MigrationIntentRepository {
	return &MigrationIntentRepository{pool: pool}
}

// Create records a migration intent for the given getmentor slug. It is
// idempotent: re-submitting an existing slug keeps the original row and
// returns created=false.
func (r *MigrationIntentRepository) Create(ctx context.Context, slug string) (created bool, err error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO migration_intents (slug) VALUES ($1)
		 ON CONFLICT (slug) DO NOTHING`,
		slug,
	)
	if err != nil {
		return false, fmt.Errorf("failed to insert migration intent: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
