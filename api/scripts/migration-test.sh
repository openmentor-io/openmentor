#!/usr/bin/env bash
# ============================================================================
# Apply every migration up, then back down, then up again (H12)
# ============================================================================
# The one property nothing else in CI checks: that the .down.sql files actually
# run and actually reverse their .up.sql. The Docker smoke test in
# "CI / API -> Build Docker Image" only ever migrates UP on an empty database,
# so a down-migration could be syntactically broken — or missing — for months,
# and an operator would find out mid-incident (see H10: rollback across a
# migration boundary).
#
# Driven by the golang-migrate CLI at the SAME version api/go.mod pins, so the
# version bookkeeping under test (schema_migrations, dirty flag, versionExists)
# is the code production runs, not an approximation with psql.
#
# Missing .down.sql files are tolerated on purpose. 000002_populate_tags has
# none on main; the sibling H10 branch adds it. Until then the harness walks
# down only to the highest version whose down file is missing and says so —
# rather than failing on a known gap and being disabled for it. When that file
# lands, this script automatically starts testing the full range down to zero.
#
# Postgres: reuses $MIGRATION_TEST_DATABASE_URL when set, otherwise starts and
# removes its own throwaway container (docker required). Credentials for the
# throwaway are ephemeral fixtures, not secrets, and the DSN is masked in output.
#
# Usage: ./scripts/migration-test.sh
#        make migration-test
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")/.."

MIGRATIONS_DIR=migrations
CONTAINER=openmentor-migration-test
HOST_PORT=${MIGRATION_TEST_PORT:-55445}

# The CLI version comes from go.mod: a harness testing bookkeeping with a
# different library version than the app links is testing the wrong thing.
MIGRATE_VERSION=$(awk '$1 == "github.com/golang-migrate/migrate/v4" {print $2; exit}' go.mod)
if [ -z "$MIGRATE_VERSION" ]; then
    echo "❌ could not read the golang-migrate version out of go.mod"
    exit 1
fi
echo "golang-migrate CLI: $MIGRATE_VERSION (from go.mod)"

cleanup() {
    if [ -n "${STARTED_CONTAINER:-}" ]; then
        docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

if [ -n "${MIGRATION_TEST_DATABASE_URL:-}" ]; then
    DSN=$MIGRATION_TEST_DATABASE_URL
    echo "Using MIGRATION_TEST_DATABASE_URL (host/credentials not printed)"
else
    command -v docker >/dev/null || {
        echo "❌ docker is required (or set MIGRATION_TEST_DATABASE_URL to an EMPTY throwaway database)"
        exit 1
    }
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    echo "Starting throwaway Postgres on 127.0.0.1:$HOST_PORT..."
    docker run -d --name "$CONTAINER" -p "$HOST_PORT:5432" \
        -e POSTGRES_USER=openmentor \
        -e POSTGRES_PASSWORD=migtest \
        -e POSTGRES_DB=openmentor_migtest \
        postgres:16-alpine >/dev/null
    STARTED_CONTAINER=1
    for _ in $(seq 1 30); do
        # -h forces a TCP check: on the unix socket the entrypoint's bootstrap
        # server answers before Postgres listens on TCP.
        if docker exec "$CONTAINER" pg_isready -h 127.0.0.1 -U openmentor -d openmentor_migtest >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    docker exec "$CONTAINER" pg_isready -h 127.0.0.1 -U openmentor -d openmentor_migtest >/dev/null 2>&1 || {
        echo "❌ postgres never became ready"
        docker logs "$CONTAINER"
        exit 1
    }
    DSN="postgres://openmentor:migtest@127.0.0.1:$HOST_PORT/openmentor_migtest?sslmode=disable"
fi

# `postgres` build tag: the CLI compiles in database drivers per tag.
migrate_cli() {
    go run -tags 'postgres' \
        "github.com/golang-migrate/migrate/v4/cmd/migrate@$MIGRATE_VERSION" \
        -path "$MIGRATIONS_DIR" -database "$DSN" "$@"
}

# `migrate version` writes its answer to stderr, and exits NON-ZERO with
# "error: no migration" when the database is at no version at all (the state
# `down -all` leaves behind) — so capture both streams and never let that
# expected failure abort the script under `set -e`.
version_output() {
    migrate_cli version 2>&1 || true
}

assert_version() {
    local want="$1" label="$2" out got
    out=$(version_output)
    got=$(printf '%s\n' "$out" | head -1)
    case "$out" in
        *dirty*)
            echo "❌ $label: schema_migrations is DIRTY at '$got' — a migration failed halfway and left the database unusable to golang-migrate"
            return 1
            ;;
    esac
    if [ "$got" != "$want" ]; then
        echo "❌ $label: expected version $want, got '$got'"
        return 1
    fi
    echo "  ok   $label: version $got"
}

# --- the version range this repo can actually walk down ---------------------
MAX_VERSION=""
FLOOR=0
MISSING=""
for up in "$MIGRATIONS_DIR"/*.up.sql; do
    base=$(basename "$up" .up.sql)
    ver=${base%%_*}
    MAX_VERSION=$((10#$ver))
    if [ ! -f "$MIGRATIONS_DIR/$base.down.sql" ]; then
        MISSING="$MISSING $base"
        # Everything at or below a missing down file is unreachable going down
        [ "$((10#$ver))" -gt "$FLOOR" ] && FLOOR=$((10#$ver))
    fi
done
if [ -z "$MAX_VERSION" ]; then
    echo "❌ no *.up.sql found in $MIGRATIONS_DIR"
    exit 1
fi
if [ -n "$MISSING" ]; then
    echo "⚠️  no .down.sql for:$MISSING"
    echo "   Walking down only to version $FLOOR. Rolling production back across"
    echo "   that boundary is the H10 hazard; adding the missing file makes this"
    echo "   harness test the full range automatically."
fi

FAILURES=0
step() { "$@" || FAILURES=$((FAILURES + 1)); }

echo
echo "1/3 up (all $MAX_VERSION migrations)"
migrate_cli up
step assert_version "$MAX_VERSION" "after up"

echo
echo "2/3 down to $FLOOR"
if [ "$FLOOR" -eq 0 ]; then
    migrate_cli down -all
    # golang-migrate reports "no migration" once the table is back to nothing
    out=$(version_output)
    case "$out" in
        *"no migration"*) echo "  ok   after down: no version (empty schema_migrations)" ;;
        *) echo "❌ after down -all: expected no version, got '$(printf '%s\n' "$out" | head -1)'"; FAILURES=$((FAILURES + 1)) ;;
    esac
else
    migrate_cli goto "$FLOOR"
    step assert_version "$FLOOR" "after down"
fi

echo
echo "3/3 up again (proves the downs left a re-appliable schema)"
migrate_cli up
step assert_version "$MAX_VERSION" "after re-up"

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "✅ migrations apply up, down to $FLOOR, and up again"
else
    echo "$FAILURES migration assertion(s) failed"
    exit 1
fi
