package repository

// Slug-history data access for the custom-username feature (D29).
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
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrSlugTaken is returned when a requested slug is already someone's current
// slug or an active redirect owned by another mentor.
var ErrSlugTaken = errors.New("slug already taken")

// CooldownError reports that a mentor-initiated rename was rejected because the
// 14-day cooldown is still active. Carried out of ChangeSlug so the check runs
// under the same lock/transaction that records the rename (no TOCTOU gap).
type CooldownError struct {
	NextChangeAt time.Time
}

func (e *CooldownError) Error() string {
	return "username change cooldown active until " + e.NextChangeAt.Format(time.RFC3339)
}

// slugHistoryKeepCount is how many retired slugs (redirect hops) a mentor keeps.
const slugHistoryKeepCount = 2

// slugAdvisoryLockNamespace namespaces the transaction-scoped advisory locks
// that serialize operations contending for the same slug string (the ASCII of
// "slug"). Prevents a registration from claiming a slug as its current name
// while another mentor is retiring that same slug into history — which would
// leave the slug simultaneously a live profile and a redirect.
const slugAdvisoryLockNamespace = 0x736c7567

// lockSlugs takes transaction-scoped advisory locks on the given slugs within
// tx, in sorted order (deduped) to avoid deadlocks between callers that touch
// the same pair of slugs. Locks release automatically on commit/rollback.
func lockSlugs(ctx context.Context, tx pgx.Tx, slugs ...string) error {
	seen := make(map[string]struct{}, len(slugs))
	unique := make([]string, 0, len(slugs))
	for _, s := range slugs {
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		unique = append(unique, s)
	}
	sort.Strings(unique)
	for _, s := range unique {
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock($1, hashtext($2))`,
			slugAdvisoryLockNamespace, s,
		); err != nil {
			return fmt.Errorf("failed to lock slug %q: %w", s, err)
		}
	}
	return nil
}

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
	// Read the dedicated cooldown column, NOT the history rows: history is
	// trimmed to 2 hops, so admin renames could otherwise evict the
	// mentor-initiated timestamp and reset the cooldown early.
	var at *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT slug_changed_at FROM mentors WHERE id = $1`,
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
// newSlug must already be normalized+validated.
//
// cooldown enforces the 14-day mentor-initiated limit INSIDE the transaction
// (pass 0 to skip, e.g. admin changes): checking it here — while holding the
// mentor row lock that also records the rename — closes the TOCTOU gap where
// two concurrent POSTs could both pass a pre-check and rename sequentially.
// now is the clock for that check (injectable for tests). Returns
// *CooldownError when the cooldown is still active.
func (r *MentorRepository) ChangeSlug(ctx context.Context, mentorID, newSlug, changedBy string, cooldown time.Duration, now time.Time) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to begin slug change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // no-op after commit

	// Lock the mentor row so concurrent changes serialize. Read the cooldown
	// timestamp under the same lock so the check below can't be raced.
	var oldSlug string
	var slugChangedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT slug, slug_changed_at FROM mentors WHERE id = $1 FOR UPDATE`,
		mentorID,
	).Scan(&oldSlug, &slugChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("mentor %s not found", mentorID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to load mentor for slug change: %w", err)
	}
	if oldSlug == newSlug {
		return oldSlug, nil // no-op
	}

	// Serialize with any registration/rename contending for these slugs: the
	// slug we retire (oldSlug) must not be claimed as a current slug by a
	// concurrent registration, and vice-versa (see lockSlugs).
	if err = lockSlugs(ctx, tx, oldSlug, newSlug); err != nil {
		return "", err
	}

	// Cooldown, evaluated under the mentor row lock so it can't be raced by a
	// second concurrent rename (the loser blocks on the lock, then re-reads the
	// winner's committed slug_changed_at here).
	if cooldown > 0 && slugChangedAt != nil {
		if next := slugChangedAt.Add(cooldown); now.Before(next) {
			return "", &CooldownError{NextChangeAt: next}
		}
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

	// Retire the current slug as a redirect. Stamp created_at with
	// clock_timestamp() (evaluated now, after we hold the row lock) rather than
	// the column DEFAULT NOW() — NOW() is the transaction START time, so under
	// overlapping renames a later-committing tx could stamp an EARLIER time and
	// the "keep newest 2" trim below would then drop the wrong redirect.
	if _, err = tx.Exec(ctx,
		`INSERT INTO mentor_slug_history (slug, mentor_id, changed_by, created_at)
		 VALUES ($1, $2, $3, clock_timestamp())`,
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

	// Update the slug (+updated_at busts image/OG caches). Only mentor-initiated
	// changes advance slug_changed_at, so admin renames neither consume nor
	// reset the mentor's 14-day cooldown.
	if changedBy == "mentor" {
		_, err = tx.Exec(ctx,
			`UPDATE mentors SET slug = $1, updated_at = NOW(), slug_changed_at = NOW() WHERE id = $2`,
			newSlug, mentorID,
		)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE mentors SET slug = $1, updated_at = NOW() WHERE id = $2`,
			newSlug, mentorID,
		)
	}
	if err != nil {
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
