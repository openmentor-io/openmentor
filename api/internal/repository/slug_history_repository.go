package repository

// Slug-history data access for the custom-username feature (D28).
//
// mentor_slug_history holds a mentor's retired slugs. Rows serve two jobs:
//   1. Redirects: an old slug 301s to the mentor's current profile (resolved
//      by GetBySlug's history fallback).
//   2. The 14-day change cooldown: the newest row with changed_by='mentor' is
//      when the mentor last renamed themselves.
// Policy: at most the 2 newest rows per mentor survive a change; older rows
// are deleted, which frees those slugs for anyone to claim.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrSlugTaken is returned when a requested slug is already someone's current
// slug or an active redirect owned by another mentor.
var ErrSlugTaken = errors.New("slug already taken")

// slugHistoryKeepCount is how many retired slugs (redirect hops) a mentor keeps.
const slugHistoryKeepCount = 2

// isUniqueViolation reports whether err is a Postgres unique-constraint error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isSlugUniqueViolation reports whether err is specifically the mentors.slug
// unique constraint (other columns on the row are also UNIQUE).
func isSlugUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		(pgErr.ConstraintName == "mentors_slug_key" || pgErr.ConstraintName == "")
}

// IsSlugTaken reports whether slug is unavailable: it is some mentor's current
// slug, or an active redirect (history row) belonging to a DIFFERENT mentor.
// A mentor's own history rows do not block them (self-reclaim of an old slug).
// excludeMentorID may be empty (registration: nothing is "own").
func (r *MentorRepository) IsSlugTaken(ctx context.Context, slug, excludeMentorID string) (bool, error) {
	var taken bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM mentors WHERE slug = $1)
		    OR EXISTS (
		         SELECT 1 FROM mentor_slug_history
		          WHERE slug = $1 AND ($2 = '' OR mentor_id::text <> $2)
		       )`,
		slug, excludeMentorID,
	).Scan(&taken)
	if err != nil {
		return false, fmt.Errorf("failed to check slug availability: %w", err)
	}
	return taken, nil
}

// SlugHistoryEntry mirrors one retired slug (used for the redirect lookup).
type SlugHistoryEntry struct {
	Slug      string
	MentorID  string
	ChangedBy string
	CreatedAt time.Time
}

// GetSlugHistoryBySlug resolves a retired slug to its mentor, or (nil, nil).
func (r *MentorRepository) GetSlugHistoryBySlug(ctx context.Context, slug string) (*SlugHistoryEntry, error) {
	var e SlugHistoryEntry
	err := r.pool.QueryRow(ctx,
		`SELECT slug, mentor_id, changed_by, created_at FROM mentor_slug_history WHERE slug = $1`,
		slug,
	).Scan(&e.Slug, &e.MentorID, &e.ChangedBy, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up slug history for %q: %w", slug, err)
	}
	return &e, nil
}

// LatestMentorSlugChange returns when the mentor last renamed themselves
// (mentor-initiated only — admin changes don't consume the cooldown), or nil
// when they never have.
func (r *MentorRepository) LatestMentorSlugChange(ctx context.Context, mentorID string) (*time.Time, error) {
	var at *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT MAX(created_at) FROM mentor_slug_history
		  WHERE mentor_id = $1 AND changed_by = 'mentor'`,
		mentorID,
	).Scan(&at)
	if err != nil {
		return nil, fmt.Errorf("failed to read latest slug change: %w", err)
	}
	return at, nil
}

// ChangeSlug atomically renames a mentor: records the old slug in history,
// trims history to the newest slugHistoryKeepCount rows, reclaims the new
// slug's own history row if the mentor is switching back, and updates
// mentors.slug (+updated_at, which also busts image/OG caches).
// Returns the previous slug. ErrSlugTaken when the slug is unavailable.
// newSlug must already be normalized+validated; cooldown is the service's job.
func (r *MentorRepository) ChangeSlug(ctx context.Context, mentorID, newSlug, changedBy string) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to begin slug change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // no-op after commit

	// Lock the mentor row so concurrent changes serialize.
	var oldSlug string
	err = tx.QueryRow(ctx, `SELECT slug FROM mentors WHERE id = $1 FOR UPDATE`, mentorID).Scan(&oldSlug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("mentor %s not found", mentorID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to load mentor for slug change: %w", err)
	}
	if oldSlug == newSlug {
		return oldSlug, nil // no-op
	}

	// Availability inside the tx (the UNIQUE constraint on mentors.slug is
	// the backstop for races on current slugs; this also covers redirects).
	var taken bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM mentors WHERE slug = $1)
		    OR EXISTS (
		         SELECT 1 FROM mentor_slug_history
		          WHERE slug = $1 AND mentor_id <> $2
		       )`,
		newSlug, mentorID,
	).Scan(&taken)
	if err != nil {
		return "", fmt.Errorf("failed to check slug availability: %w", err)
	}
	if taken {
		return "", ErrSlugTaken
	}

	// Reclaim: if the mentor is switching back to one of their own retired
	// slugs, drop that history row (it is about to become current again).
	if _, err = tx.Exec(ctx,
		`DELETE FROM mentor_slug_history WHERE slug = $1 AND mentor_id = $2`,
		newSlug, mentorID,
	); err != nil {
		return "", fmt.Errorf("failed to reclaim slug history row: %w", err)
	}

	// Retire the current slug as a redirect.
	if _, err = tx.Exec(ctx,
		`INSERT INTO mentor_slug_history (slug, mentor_id, changed_by) VALUES ($1, $2, $3)`,
		oldSlug, mentorID, changedBy,
	); err != nil {
		if isUniqueViolation(err) {
			// The old slug already redirects somewhere (should be impossible
			// while it was current, but never corrupt history over it).
			return "", fmt.Errorf("slug history conflict for %q: %w", oldSlug, err)
		}
		return "", fmt.Errorf("failed to record slug history: %w", err)
	}

	// Keep only the newest N redirects; older slugs are freed for others.
	if _, err = tx.Exec(ctx, `
		DELETE FROM mentor_slug_history
		 WHERE mentor_id = $1
		   AND slug NOT IN (
		         SELECT slug FROM mentor_slug_history
		          WHERE mentor_id = $1
		          ORDER BY created_at DESC
		          LIMIT $2
		       )`,
		mentorID, slugHistoryKeepCount,
	); err != nil {
		return "", fmt.Errorf("failed to trim slug history: %w", err)
	}

	if _, err = tx.Exec(ctx,
		`UPDATE mentors SET slug = $1, updated_at = NOW() WHERE id = $2`,
		newSlug, mentorID,
	); err != nil {
		if isUniqueViolation(err) {
			return "", ErrSlugTaken
		}
		return "", fmt.Errorf("failed to update mentor slug: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit slug change: %w", err)
	}
	return oldSlug, nil
}
