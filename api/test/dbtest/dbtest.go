// Package dbtest connects tests to a throwaway Postgres and builds the schema
// from api/migrations, the way cmd/migrate does.
//
// Some behavior only exists in SQL — an UPDATE whose WHERE Postgres
// re-evaluates against the winning writer's row, or a NULL column that pgx
// refuses to decode — and no fake repository can model it. Tests that need
// those facts import this package and skip when no database is configured:
//
//	docker run -d --name om-test-pg -e POSTGRES_USER=openmentor \
//	  -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=openmentor -p 55432:5432 postgres:16-alpine
//	OPENMENTOR_TEST_DATABASE_URL='postgres://openmentor:pw@127.0.0.1:55432/openmentor?sslmode=disable' \
//	  go test ./...
//
// CI / API runs them against its own service container (see ci-api.yml).
package dbtest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// URLEnv names the environment variable holding the throwaway database DSN.
const URLEnv = "OPENMENTOR_TEST_DATABASE_URL"

// schemaLockKey serializes the schema bootstrap. `go test ./...` runs packages
// as concurrent processes against the SAME database, and the migrations are not
// all idempotent (000005 does a bare ADD COLUMN), so two packages racing to
// apply them would fail one of them. A session-level advisory lock is the only
// mutex the two processes share.
const schemaLockKey = 8734501982374 // arbitrary, unique to this bootstrap

// Pool connects to the throwaway database and makes sure the schema is there.
// It skips the test when the DSN is unset so the fast, database-less check
// still runs the rest of the suite.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv(URLEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping the Postgres-backed tests", URLEnv)
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", URLEnv, err)
	}
	t.Cleanup(pool.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping the test database: %v", err)
	}
	if err := ensureSchema(ctx, pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return pool
}

// ensureSchema applies every migrations/*.up.sql in order, once per database.
func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	// One connection for the whole critical section: an advisory lock is held
	// by the session that took it, so it must be released on the same conn.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(schemaLockKey)); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, int64(schemaLockKey)) //nolint:errcheck // released with the connection anyway
	}()

	// Applied files are tracked by name in a bookkeeping table rather than by
	// a bare "does mentors exist" probe. The probe had a silent failure mode on
	// exactly the container this package's docs tell people to keep around: a
	// long-lived database bootstrapped before a new migration landed would
	// short-circuit here and never receive it, and the first symptom was a test
	// failing on a missing column with nothing pointing at the stale schema
	// (000015's price_amount was found this way). Names, not a high-water
	// version: the point is only to re-apply the tail after a rebase, and names
	// need no parsing.
	if _, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dbtest_applied_migrations (filename text PRIMARY KEY)
	`); err != nil {
		return err
	}

	// Grandfather a database bootstrapped before the bookkeeping table existed:
	// if mentors is already there but nothing is recorded, the pre-existing
	// migrations were applied by the old code path. Record every CURRENT file
	// as applied only when the schema predates the table — a genuinely fresh
	// database has no mentors and skips this.
	var hasMentors, hasRecords bool
	if scanErr := conn.QueryRow(ctx, `SELECT to_regclass('mentors') IS NOT NULL,
		EXISTS (SELECT 1 FROM dbtest_applied_migrations)`).Scan(&hasMentors, &hasRecords); scanErr != nil {
		return scanErr
	}

	files, err := filepath.Glob(filepath.Join(migrationsDir(), "*.up.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)

	if hasMentors && !hasRecords {
		// Cannot know which files the old path applied; assume "all that existed
		// then". Files added SINCE then are indistinguishable from files applied
		// then, so this grandfathering is one-shot honest at best — but it only
		// ever runs once per pre-existing database, and every later addition is
		// tracked exactly.
		for _, file := range files {
			if _, execErr := conn.Exec(ctx,
				`INSERT INTO dbtest_applied_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING`,
				filepath.Base(file)); execErr != nil {
				return execErr
			}
		}
		return nil
	}

	for _, file := range files {
		if err := applyIfPending(ctx, conn, file); err != nil {
			return err
		}
	}
	return nil
}

// applyIfPending runs one migration file unless dbtest_applied_migrations
// already records it, and records it after a successful apply.
func applyIfPending(ctx context.Context, conn *pgxpool.Conn, file string) error {
	var applied bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM dbtest_applied_migrations WHERE filename = $1)`,
		filepath.Base(file)).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	sql, err := os.ReadFile(file) //nolint:gosec // fixed in-repo migrations directory
	if err != nil {
		return err
	}
	// pgx sends argument-less queries over the simple protocol, so a whole
	// multi-statement migration file goes in one Exec.
	if _, err := conn.Exec(ctx, string(sql)); err != nil {
		return err
	}
	_, err = conn.Exec(ctx,
		`INSERT INTO dbtest_applied_migrations (filename) VALUES ($1)`, filepath.Base(file))
	return err
}

// migrationsDir resolves api/migrations from this file's own location, so the
// path does not depend on which package's directory the test runs in.
func migrationsDir() string {
	_, self, _, _ := runtime.Caller(0) //nolint:dogsled // only the path is needed
	return filepath.Join(filepath.Dir(self), "..", "..", "migrations")
}
