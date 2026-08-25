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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
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

	// A database owned by cmd/migrate (the compose dev stack) is NEVER mutated
	// here — only verified. The first version of this adoption seeded the
	// ledger from schema_migrations and then applied the newer files itself,
	// which left schema_migrations behind: the NEXT cmd/migrate run walks
	// forward from its recorded version, re-attempts those same files, and
	// fails the dev stack on a duplicate constraint. Ownership is binary — if
	// golang-migrate's table exists, golang-migrate is the only writer.
	var migrateOwned bool
	if scanErr := conn.QueryRow(ctx,
		`SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&migrateOwned); scanErr != nil {
		return scanErr
	}

	files, err := filepath.Glob(filepath.Join(migrationsDir(), "*.up.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)

	if migrateOwned {
		return verifyMigrateOwnedCurrent(ctx, conn, files)
	}

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

	var hasMentors, hasRecords bool
	if scanErr := conn.QueryRow(ctx, `SELECT to_regclass('mentors') IS NOT NULL,
		EXISTS (SELECT 1 FROM dbtest_applied_migrations)`).Scan(&hasMentors, &hasRecords); scanErr != nil {
		return scanErr
	}
	if hasMentors && !hasRecords {
		// Schema without provenance: a long-lived container bootstrapped by
		// this package before the ledger existed. Which files built it is
		// unprovable, so refuse with the fix in the message rather than guess —
		// an earlier version recorded every current file as applied, which
		// preserved the exact staleness bug the ledger exists to fix.
		return fmt.Errorf("dbtest: this database has the schema but no record of which migrations " +
			"built it (it predates dbtest's migration ledger); recreate the throwaway container " +
			"(docker rm -f + docker run, see the package docs) rather than guessing")
	}

	for _, file := range files {
		if err := applyIfPending(ctx, conn, file); err != nil {
			return err
		}
	}
	return nil
}

// verifyMigrateOwnedCurrent checks — without writing anything — that a
// golang-migrate-managed database is at the checkout's newest migration. Tests
// may READ such a database (the compose dev stack keeps it current), but the
// moment it is behind, the fix belongs to cmd/migrate, not to this package.
func verifyMigrateOwnedCurrent(ctx context.Context, conn *pgxpool.Conn, files []string) error {
	var version int64
	var dirty bool
	if err := conn.QueryRow(ctx,
		`SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		return fmt.Errorf("dbtest: reading schema_migrations: %w", err)
	}
	if dirty {
		return fmt.Errorf("dbtest: schema_migrations reports version %d DIRTY — a migration "+
			"failed half-way on this database; repair it with cmd/migrate before running tests", version)
	}

	newest, err := newestFileVersion(files)
	if err != nil {
		return err
	}
	if version < newest {
		return fmt.Errorf("dbtest: this database is managed by cmd/migrate and is at version %d, "+
			"but the checkout has migrations up to %d. Update it with cmd/migrate (compose dev: "+
			"re-run the migrate service) or point %s at a throwaway container — dbtest will not "+
			"apply migrations behind golang-migrate's back, because its next run would re-attempt "+
			"them and fail", version, newest, URLEnv)
	}
	return nil
}

// newestFileVersion reads the numeric prefix of the last NNNNNN_title.up.sql.
func newestFileVersion(files []string) (int64, error) {
	if len(files) == 0 {
		return 0, fmt.Errorf("dbtest: no migration files found")
	}
	name := filepath.Base(files[len(files)-1])
	prefix, _, ok := strings.Cut(name, "_")
	version, err := strconv.ParseInt(prefix, 10, 64)
	if !ok || err != nil {
		return 0, fmt.Errorf("dbtest: cannot read a version out of migration filename %q", name)
	}
	return version, nil
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
	// One transaction for the file AND its ledger row: applied-but-unrecorded
	// is a wedged database — the next run re-attempts a non-idempotent
	// migration and fails on a duplicate column until someone recreates the
	// container. Safe to wrap because no migration here uses a statement that
	// refuses transactions (CREATE INDEX CONCURRENTLY and friends) — the same
	// property cmd/migrate's per-migration transaction already relies on, so a
	// future migration that needed one would fight production first.
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // no-op after commit

	// pgx sends argument-less queries over the simple protocol, so a whole
	// multi-statement migration file goes in one Exec.
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO dbtest_applied_migrations (filename) VALUES ($1)`, filepath.Base(file)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// migrationsDir resolves api/migrations from this file's own location, so the
// path does not depend on which package's directory the test runs in.
func migrationsDir() string {
	_, self, _, _ := runtime.Caller(0) //nolint:dogsled // only the path is needed
	return filepath.Join(filepath.Dir(self), "..", "..", "migrations")
}
