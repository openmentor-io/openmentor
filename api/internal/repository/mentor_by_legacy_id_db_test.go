package repository

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/cache"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/require"
)

// GetByID used to answer "one mentor by legacy_id" by materializing the entire
// public catalog and scanning the slice (audit C4). Whether it still does is a
// fact about the SQL that reaches Postgres and the plan Postgres picks for it —
// neither is observable from a fake repository, so these tests need a real
// database (see the dbtest package docs) and skip without one.

// legacyIDTestCorpus is how many extra active mentors the plan comparison seeds.
// Big enough that a catalog scan and an index lookup cannot be confused for one
// another, small enough to seed in well under a second.
const legacyIDTestCorpus = 500

const legacyIDTestSlugPrefix = "perf-c4-"

func legacyIDTestRepo(t *testing.T, pool *pgxpool.Pool) *MentorRepository {
	t.Helper()
	return NewMentorRepository(pool, cache.NewTagsCache(
		func(context.Context) (map[string]string, error) { return map[string]string{}, nil },
	))
}

// seedLegacyIDMentor inserts one mentor with the given status and returns its
// generated legacy_id.
func seedLegacyIDMentor(t *testing.T, pool *pgxpool.Pool, suffix, status string) int {
	t.Helper()
	var legacyID int
	err := pool.QueryRow(context.Background(), `
		INSERT INTO mentors (slug, name, status)
		VALUES ($1, 'Legacy ID Test', $2)
		RETURNING legacy_id
	`, legacyIDTestSlugPrefix+suffix, status).Scan(&legacyID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE slug = $1`, legacyIDTestSlugPrefix+suffix)
	})
	return legacyID
}

// queryRecorder captures the SQL a pool actually sends, which is the only way
// to tell "read one indexed row" from "read the catalog and filter in Go" —
// both return the same mentor.
type queryRecorder struct {
	mu  sync.Mutex
	sql []string
}

func (q *queryRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sql = append(q.sql, data.SQL)
	return ctx
}

func (q *queryRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (q *queryRecorder) recorded() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.sql...)
}

// recordingPool is a second pool onto the same test database with a query
// tracer attached. dbtest.Pool is reused first so the schema is in place.
func recordingPool(t *testing.T) (*pgxpool.Pool, *queryRecorder) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv(dbtest.URLEnv))
	require.NoError(t, err)
	rec := &queryRecorder{}
	cfg.ConnConfig.Tracer = rec
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, rec
}

// TestGetByIDReadsOneIndexedRow is the regression test for C4: the lookup must
// reach Postgres as a legacy_id predicate, not as the catalog read it used to
// borrow from GetAll.
func TestGetByIDReadsOneIndexedRow(t *testing.T) {
	shared := dbtest.Pool(t)
	legacyID := seedLegacyIDMentor(t, shared, "indexed", "active")

	pool, rec := recordingPool(t)
	repo := legacyIDTestRepo(t, pool)

	mentor, err := repo.GetByID(context.Background(), legacyID, models.FilterOptions{OnlyVisible: true})
	require.NoError(t, err)
	require.Equal(t, legacyID, mentor.LegacyID)

	statements := rec.recorded()
	require.Len(t, statements, 1, "one row should cost one query, got: %v", statements)
	require.Contains(t, statements[0], "m.legacy_id = $1",
		"the lookup must be pushed into SQL, not filtered in Go")
	require.NotContains(t, statements[0], "ORDER BY m.sort_order",
		"ORDER BY sort_order means the whole catalog was read to find one mentor")
}

// TestGetByIDKeepsCatalogVisibility pins the behavior the old
// GetAll-and-scan implementation had by accident: reading the catalog meant a
// non-active or deleted mentor was unreachable by numeric ID regardless of the
// FilterOptions the caller passed. The internal by-id endpoint takes those
// options from a request body, so widening them here would expose draft
// profiles.
func TestGetByIDKeepsCatalogVisibility(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := legacyIDTestRepo(t, pool)
	ctx := context.Background()

	// The most permissive options any caller could ask for.
	permissive := models.FilterOptions{ShowHidden: true, AllowAnyStatus: true, IncludeDeleted: true}

	active := seedLegacyIDMentor(t, pool, "visible-active", "active")
	found, err := repo.GetByID(ctx, active, permissive)
	require.NoError(t, err, "an active mentor must stay reachable by legacy ID")
	require.Equal(t, active, found.LegacyID)

	for _, status := range []string{"pending", "declined", "inactive"} {
		hidden := seedLegacyIDMentor(t, pool, "visible-"+status, status)
		_, err := repo.GetByID(ctx, hidden, permissive)
		require.Error(t, err, "status %q must not be reachable by legacy ID", status)
	}

	deleted := seedLegacyIDMentor(t, pool, "visible-deleted", "active")
	_, err = pool.Exec(ctx, `UPDATE mentors SET deleted_at = now() WHERE legacy_id = $1`, deleted)
	require.NoError(t, err)
	_, err = repo.GetByID(ctx, deleted, permissive)
	require.Error(t, err, "a deleted profile must not be reachable by legacy ID")

	// And a legacy ID nobody holds is a miss, not the first catalog row.
	_, err = repo.GetByID(ctx, -1, permissive)
	require.Error(t, err)
}

var executionTime = regexp.MustCompile(`Execution Time: ([0-9.]+) ms`)

// explain runs the query under EXPLAIN (ANALYZE, BUFFERS) and returns the plan.
func explain(t *testing.T, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	rows, err := pool.Query(context.Background(), "EXPLAIN (ANALYZE, BUFFERS) "+query, args...)
	require.NoError(t, err)
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	require.NoError(t, rows.Err())
	return plan.String()
}

// TestMentorByLegacyIDPlanIsIndexed is the measurement behind C4. It seeds a
// catalog, then EXPLAIN ANALYZEs both the query GetByID used to borrow and the
// one it runs now, logging each plan's execution time so the improvement is a
// number rather than an assertion. The assertion is the durable part: the
// single-mentor read must go through mentors_legacy_id_uniq, because a plan
// change is how this regresses silently.
func TestMentorByLegacyIDPlanIsIndexed(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	t.Cleanup(func() {
		// client_requests.mentor_id is ON DELETE SET NULL, so the requests have
		// to go first or the mentor delete orphans them.
		_, _ = pool.Exec(context.Background(), `
			DELETE FROM client_requests WHERE mentor_id IN (SELECT id FROM mentors WHERE slug LIKE $1)
		`, legacyIDTestSlugPrefix+"corpus-%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE slug LIKE $1`, legacyIDTestSlugPrefix+"corpus-%")
	})

	_, err := pool.Exec(ctx, `
		INSERT INTO mentors (slug, name, status, job_title, workplace, about, details, competencies, sort_order)
		SELECT $1 || g, 'Perf Mentor ' || g, 'active', 'Staff Engineer', 'ACME',
		       repeat('a', 400), repeat('d', 400), repeat('c', 400), g
		FROM generate_series(1, $2) g
	`, legacyIDTestSlugPrefix+"corpus-", legacyIDTestCorpus)
	require.NoError(t, err)

	// Tags and completed requests are what make the catalog read expensive: one
	// join fan-out plus a COUNT(*) subquery per row.
	_, err = pool.Exec(ctx, `
		INSERT INTO mentor_tags (mentor_id, tag_id)
		SELECT m.id, t.id
		FROM mentors m CROSS JOIN (SELECT id FROM tags ORDER BY name LIMIT 4) t
		WHERE m.slug LIKE $1
	`, legacyIDTestSlugPrefix+"corpus-%")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO client_requests (mentor_id, name, email, status)
		SELECT m.id, 'Mentee', 'mentee@example.com', 'done'
		FROM mentors m CROSS JOIN generate_series(1, 3)
		WHERE m.slug LIKE $1
	`, legacyIDTestSlugPrefix+"corpus-%")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `ANALYZE mentors, mentor_tags, tags, client_requests`)
	require.NoError(t, err)

	var legacyID int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT legacy_id FROM mentors WHERE slug = $1`,
		fmt.Sprintf("%scorpus-%d", legacyIDTestSlugPrefix, legacyIDTestCorpus/2),
	).Scan(&legacyID))

	before := explain(t, pool, mentorCatalogQuery)
	after := explain(t, pool, mentorByLegacyIDQuery, legacyID)

	t.Logf("C4 before — GetAll + scan in Go (%d seeded mentors):\n%s", legacyIDTestCorpus, before)
	t.Logf("C4 after — WHERE m.legacy_id = $1:\n%s", after)
	t.Logf("C4 execution time: before %s ms, after %s ms",
		executionTime.FindStringSubmatch(before)[1], executionTime.FindStringSubmatch(after)[1])

	require.Contains(t, after, "mentors_legacy_id_uniq",
		"the single-mentor read must use the unique index on legacy_id")
	require.NotContains(t, after, "Seq Scan on mentors",
		"a sequential scan on mentors means the catalog is being read again")
}
