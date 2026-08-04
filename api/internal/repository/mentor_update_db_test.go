package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/cache"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MentorRepository.Update builds its SQL by string concatenation from a
// caller-supplied map, so its failure modes are all shapes of that map rather
// than shapes of the data. They cannot be tested against a fake: the interesting
// answers ("multiple assignments to same column", "syntax error at or near ,")
// come from Postgres.
//
// Requires a database (see the dbtest package docs); skips without one.

func updateTestRepo(t *testing.T) (*MentorRepository, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	return NewMentorRepository(pool, cache.NewTagsCache(
		func(context.Context) (map[string]string, error) { return map[string]string{}, nil },
	)), pool
}

// seedMentor inserts a minimal mentor and returns its id.
func seedMentor(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO mentors (slug, name, status) VALUES ($1, 'Update Test', 'active')
		RETURNING id
	`, slug).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
	})
	return id
}

// TestUpdateWithNoChangesIsANoOp: an empty map used to build
// `UPDATE mentors SET , updated_at = NOW() WHERE id = $1`, which is a syntax
// error — so a caller whose diff came out empty got a failed save.
func TestUpdateWithNoChangesIsANoOp(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "update-noop")

	before := readUpdatedAt(t, pool, id)
	require.NoError(t, repo.Update(ctx, id, map[string]interface{}{}))
	require.NoError(t, repo.Update(ctx, id, nil))

	// A no-op must also leave updated_at alone: it busts the OG/image caches,
	// and touching it on every empty save would invalidate them for nothing.
	assert.Equal(t, before, readUpdatedAt(t, pool, id))
}

// TestUpdateRejectsUpdatedAt: Update always appends `updated_at = NOW()`, so
// accepting the column from a caller produced a double assignment, which Postgres
// rejects outright — a 500 on a save. It was in the allowlist.
func TestUpdateRejectsUpdatedAt(t *testing.T) {
	repo, pool := updateTestRepo(t)
	id := seedMentor(t, pool, "update-updated-at")

	err := repo.Update(context.Background(), id, map[string]interface{}{
		"about":      "new bio",
		"updated_at": time.Now(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid column name: updated_at")

	// Rejected as a whole: the about change must not have landed either.
	var about string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COALESCE(about, '') FROM mentors WHERE id = $1`, id).Scan(&about))
	assert.Empty(t, about)
}

// TestUpdateRejectsColumnsOutsideTheAllowlist is the injection guard. The values
// are parameterized but the column names are concatenated, so the allowlist is
// the only thing between a caller and arbitrary SQL.
func TestUpdateRejectsColumnsOutsideTheAllowlist(t *testing.T) {
	repo, pool := updateTestRepo(t)
	id := seedMentor(t, pool, "update-allowlist")
	ctx := context.Background()

	for _, column := range []string{
		"login_token",
		"login_token_expires_at",
		"slug_changed_at",
		"legacy_id",
		"id",
		"about = 'x', status",              // a smuggled second assignment
		"status = 'active' WHERE true; --", // a smuggled statement
	} {
		err := repo.Update(ctx, id, map[string]interface{}{column: "x"})
		require.Error(t, err, "column %q must be rejected", column)
		assert.Contains(t, err.Error(), "invalid column name")
	}

	// login_token in particular: a caller able to set it could mint themselves a
	// magic link for any mentor.
	var token *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT login_token FROM mentors WHERE id = $1`, id).Scan(&token))
	assert.Nil(t, token)
}

// TestUpdateWritesEveryAllowedColumn drives the whole allowlist through the real
// builder: multi-column updates renumber placeholders as they are appended, and
// map iteration order is random, so a per-column test would not exercise the
// same query twice.
func TestUpdateWritesEveryAllowedColumn(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "update-all-columns")

	updates := map[string]interface{}{
		"name":              "Renamed Mentor",
		"email":             "update-all-columns@example.test",
		"job_title":         "Staff Engineer",
		"workplace":         "OpenMentor",
		"about":             "about text",
		"details":           "details text",
		"competencies":      "competencies text",
		"experience":        "5-10",
		"price":             "free",
		"preferred_contact": "email",
		"calendar_url":      "https://cal.example/mentor",
		"status":            "inactive",
		"photo_style":       "hero",
	}
	for column := range updates {
		require.True(t, allowedUpdateColumns[column],
			"the test must cover the allowlist as it is, and %q is not in it", column)
	}
	require.Len(t, updates, len(allowedUpdateColumns)-1,
		"every allowed column except slug (which ChangeSlug owns) must be covered here")

	before := readUpdatedAt(t, pool, id)
	require.NoError(t, repo.Update(ctx, id, updates))

	mentor, err := repo.fetchMentorByUUIDFromDB(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "Renamed Mentor", mentor.Name)
	assert.Equal(t, "Staff Engineer", mentor.Job)
	assert.Equal(t, "https://cal.example/mentor", mentor.CalendarURL)
	assert.Equal(t, "inactive", mentor.Status)
	assert.True(t, mentor.UpdatedAt.After(before),
		"Update must advance updated_at: the OG card and image URLs are cached on it")
}

// TestUpdateOfMissingMentorIsNotAnError documents the existing contract: the
// builder issues a bare UPDATE, so zero affected rows is silent. Callers all
// fetch the mentor first (and the services check), but anyone tightening this
// into an error should do so deliberately.
func TestUpdateOfMissingMentorIsNotAnError(t *testing.T) {
	repo, _ := updateTestRepo(t)
	err := repo.Update(context.Background(), "00000000-0000-0000-0000-0000000000ff",
		map[string]interface{}{"about": "nobody"})
	assert.NoError(t, err)
}

func readUpdatedAt(t *testing.T, pool *pgxpool.Pool, id string) time.Time {
	t.Helper()
	var at time.Time
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT updated_at FROM mentors WHERE id = $1`, id).Scan(&at))
	return at
}
