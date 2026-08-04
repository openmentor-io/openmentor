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

	var exists bool
	if scanErr := conn.QueryRow(ctx, `SELECT to_regclass('mentors') IS NOT NULL`).Scan(&exists); scanErr != nil {
		return scanErr
	}
	if exists {
		return nil
	}

	files, err := filepath.Glob(filepath.Join(migrationsDir(), "*.up.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, file := range files {
		sql, readErr := os.ReadFile(file) //nolint:gosec // fixed in-repo migrations directory
		if readErr != nil {
			return readErr
		}
		// pgx sends argument-less queries over the simple protocol, so a whole
		// multi-statement migration file goes in one Exec.
		if _, execErr := conn.Exec(ctx, string(sql)); execErr != nil {
			return execErr
		}
	}
	return nil
}

// migrationsDir resolves api/migrations from this file's own location, so the
// path does not depend on which package's directory the test runs in.
func migrationsDir() string {
	_, self, _, _ := runtime.Caller(0) //nolint:dogsled // only the path is needed
	return filepath.Join(filepath.Dir(self), "..", "..", "migrations")
}
