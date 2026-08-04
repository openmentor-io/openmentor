#!/usr/bin/env bash
# ============================================================================
# Tests for the split database identities (SECURITY H8)
# ============================================================================
# Applies EVERY api/migrations/*.up.sql to a throwaway postgres container, as
# the bootstrap superuser (exactly how `migrate` runs today), and then asserts
# the properties H8 exists for. The migration is only worth anything if these
# hold, and none of them are visible from reading the SQL:
#
#   1. the whole migration set applies to an empty database
#   2. the roles exist, are NOLOGIN until an operator acts, and carry none of
#      SUPERUSER / CREATEDB / CREATEROLE / REPLICATION / BYPASSRLS
#   3. nothing in schema public is still owned by the bootstrap superuser
#   4. om_api can do DML and NOTHING else: no COPY ... FROM PROGRAM, no
#      pg_read_file, no CREATE ROLE, no DROP SCHEMA, no CREATE TABLE, no
#      TRUNCATE, no writing golang-migrate's version table
#   5. om_worker has the same shape as om_api
#   6. om_migrate can run migrations — ALTER TABLE, CREATE TABLE, and install a
#      TRUSTED extension — without being superuser, and a table it creates is
#      immediately usable by om_api (the default-privileges half)
#   7. om_backup can pg_dump the database and write nothing
#   8. the scoped monitoring read set excludes the PII tables
#   9. re-applying the migration is a no-op that does NOT clear the password an
#      operator set, i.e. a redeploy cannot lock a live service out
#  10. the down migration restores ownership and removes the roles
#
# No password value is ever passed or printed: the container authenticates
# local socket connections with `trust`, and case 9 compares only whether a
# password is set, never what it is.
#
# Usage: ./db-identity-test.sh        (needs docker; pulls the compose-pinned
#                                     postgres image on first run)
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

MIGRATIONS=../api/migrations
# Same image as the postgres service in docker-compose.yml: role attributes and
# predefined roles (pg_read_all_data) are version-sensitive.
PG_IMAGE=$(awk '/^  postgres:$/ {inpg=1} inpg && /image:/ {print $2; exit}' docker-compose.yml)
CONTAINER=om-h8-identity-test-$$

command -v docker >/dev/null || { echo "error: docker is required" >&2; exit 1; }
[ -d "$MIGRATIONS" ] || { echo "error: $MIGRATIONS not found" >&2; exit 1; }

FAILURES=0
ok()  { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILURES=$((FAILURES + 1)); }
case_start() { printf '%s\n' "$1"; }

cleanup() { docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# psql as <role> over the container's unix socket. `local ... trust` in the
# image's generated pg_hba means no password is involved.
psql_as() {
    local role="$1"; shift
    docker exec -i "$CONTAINER" psql -v ON_ERROR_STOP=1 -X -q -A -t \
        -U "$role" -d openmentor "$@"
}

# Run SQL and report success/failure without leaking the output on success.
try_sql() {
    local role="$1" sql="$2"
    SQL_OUT=$(psql_as "$role" -c "$sql" 2>&1)
}

# assert_allowed <role> <label> <sql>
assert_allowed() {
    if try_sql "$1" "$3"; then ok "$2"; else bad "$2 (unexpected error: ${SQL_OUT})"; fi
}

# assert_denied <role> <label> <sql> [expected substring]
assert_denied() {
    local want="${4:-}"
    if try_sql "$1" "$3"; then
        bad "$2 — SUCCEEDED and must not have"
    elif [ -n "$want" ] && ! printf '%s' "$SQL_OUT" | grep -qi -- "$want"; then
        bad "$2 — denied, but not for the expected reason: ${SQL_OUT}"
    else
        ok "$2"
    fi
}

# assert_query <role> <label> <sql> <expected>
assert_query() {
    local got
    if got=$(psql_as "$1" -c "$3" 2>&1); then
        got=$(printf '%s' "$got" | tr -d ' \n')
        if [ "$got" = "$4" ]; then ok "$2"; else bad "$2 — got '$got', want '$4'"; fi
    else
        bad "$2 — query failed: $got"
    fi
}

echo "postgres image: $PG_IMAGE"
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
# The password is an ephemeral fixture for a container that is created and
# destroyed by this script and publishes no port.
docker run -d --name "$CONTAINER" \
    -e POSTGRES_USER=openmentor \
    -e POSTGRES_PASSWORD=h8-identity-test \
    -e POSTGRES_DB=openmentor \
    "$PG_IMAGE" >/dev/null

for i in $(seq 1 45); do
    docker exec "$CONTAINER" pg_isready -U openmentor -d openmentor >/dev/null 2>&1 && break
    [ "$i" = 45 ] && { echo "postgres never became ready"; docker logs "$CONTAINER"; exit 1; }
    sleep 1
done

# ---------------------------------------------------------------------------
case_start "1. every up-migration applies to an empty database"
apply_all() {
    local f
    for f in "$MIGRATIONS"/*.up.sql; do
        # -1: one transaction per file, like golang-migrate's single Exec
        docker exec -i "$CONTAINER" psql -v ON_ERROR_STOP=1 -X -q -1 \
            -U openmentor -d openmentor < "$f" || return 1
    done
}
# golang-migrate owns this table in production; create it here so the migration
# meets the same shape (case 4 asserts om_api cannot write to it).
psql_as openmentor -c \
    'CREATE TABLE IF NOT EXISTS schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL)' \
    >/dev/null
if OUT=$(apply_all 2>&1); then ok "all *.up.sql applied"; else bad "migration failed: $OUT"; fi

# ---------------------------------------------------------------------------
case_start "2. roles exist, are NOLOGIN, and hold no elevated attribute"
assert_query openmentor "all five roles exist" \
    "SELECT count(*) FROM pg_roles WHERE rolname IN ('om_migrate','om_api','om_worker','om_backup','om_monitor_ro')" \
    5
assert_query openmentor "no elevated attribute on any of them" \
    "SELECT count(*) FROM pg_roles WHERE rolname LIKE 'om\_%' AND (rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls)" \
    0
assert_query openmentor "created NOLOGIN (unusable until an operator acts)" \
    "SELECT count(*) FROM pg_roles WHERE rolname LIKE 'om\_%' AND rolcanlogin" \
    0
if OUT=$(docker exec -i "$CONTAINER" psql -X -q -U om_api -d openmentor -c 'SELECT 1' 2>&1); then
    bad "a NOLOGIN role could still connect"
elif printf '%s' "$OUT" | grep -qi "not permitted to log in"; then
    ok "connecting as om_api is refused before LOGIN is granted"
else
    bad "om_api connection refused for the wrong reason: $OUT"
fi

# From here on the roles need to be able to connect. This is the operator step
# from the runbook, minus the password (socket auth is trust here).
for r in om_migrate om_api om_worker om_backup; do
    psql_as openmentor -c "ALTER ROLE $r LOGIN" >/dev/null
done
# Stand-in for grafana_monitoring: proves the group role's read set without
# touching the real monitoring user.
psql_as openmentor -c "CREATE ROLE om_monitor_probe LOGIN IN ROLE om_monitor_ro" >/dev/null

# ---------------------------------------------------------------------------
case_start "3. ownership moved off the bootstrap superuser"
assert_query openmentor "no public relation is still owned by openmentor" \
    "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = 'public' AND c.relkind IN ('r','p','S','v','m')
       AND c.relowner = 'openmentor'::regrole
       AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_class'::regclass
                       AND d.objid = c.oid AND d.deptype = 'e')" \
    0
assert_query openmentor "the trigger function moved too" \
    "SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = 'public' AND p.proname = 'set_updated_at'
       AND p.proowner = 'om_migrate'::regrole" \
    1
assert_query openmentor "mentors' identity sequence followed its table" \
    "SELECT count(*) FROM pg_class WHERE relname = 'mentors_legacy_id_seq'
       AND relowner = 'om_migrate'::regrole" \
    1

# ---------------------------------------------------------------------------
case_start "4. om_api: DML only"
assert_allowed om_api "SELECT on mentors" "SELECT count(*) FROM mentors"
assert_allowed om_api "INSERT/UPDATE/DELETE on mentors" \
    "INSERT INTO mentors (slug, name, status) VALUES ('h8-probe', 'probe', 'active');
     UPDATE mentors SET name = 'probe2' WHERE slug = 'h8-probe';
     DELETE FROM mentors WHERE slug = 'h8-probe'"
assert_allowed om_api "SELECT on the version table" "SELECT count(*) FROM schema_migrations"
# The H8 acceptance criteria
assert_denied om_api "COPY ... FROM PROGRAM is refused" \
    "COPY mentors FROM PROGRAM 'id'" "permission denied"
assert_denied om_api "COPY ... TO PROGRAM is refused" \
    "COPY mentors TO PROGRAM 'cat > /tmp/x'" "permission denied"
assert_denied om_api "pg_read_file is refused" \
    "SELECT pg_read_file('/etc/passwd')" "permission denied"
assert_denied om_api "pg_ls_dir is refused" \
    "SELECT pg_ls_dir('.')" "permission denied"
assert_denied om_api "CREATE ROLE is refused" "CREATE ROLE h8_escalation SUPERUSER"
assert_denied om_api "DROP SCHEMA public is refused" "DROP SCHEMA public CASCADE"
assert_denied om_api "CREATE TABLE in public is refused" "CREATE TABLE h8_probe (x int)"
assert_denied om_api "TRUNCATE mentors is refused" "TRUNCATE mentors" "permission denied"
assert_denied om_api "DROP TABLE mentors is refused" "DROP TABLE mentors" "must be owner"
assert_denied om_api "ALTER TABLE mentors is refused" \
    "ALTER TABLE mentors ADD COLUMN h8 int" "must be owner"
assert_denied om_api "writing the version table is refused" \
    "UPDATE schema_migrations SET dirty = true" "permission denied"

case_start "5. om_worker has the same shape"
assert_allowed om_worker "SELECT on mentors" "SELECT count(*) FROM mentors"
assert_denied om_worker "COPY ... FROM PROGRAM is refused" \
    "COPY mentors FROM PROGRAM 'id'" "permission denied"
assert_denied om_worker "CREATE TABLE in public is refused" "CREATE TABLE h8_probe_w (x int)"

# ---------------------------------------------------------------------------
case_start "6. om_migrate can migrate without being superuser"
assert_allowed om_migrate "ALTER TABLE on an existing table" \
    "ALTER TABLE mentors ADD COLUMN IF NOT EXISTS h8_probe_col text"
assert_allowed om_migrate "CREATE TABLE + INDEX" \
    "CREATE TABLE h8_new_table (id int PRIMARY KEY, v text);
     CREATE INDEX h8_new_table_v_idx ON h8_new_table (v)"
assert_allowed om_migrate "CREATE EXTENSION (trusted, no superuser)" \
    "CREATE EXTENSION IF NOT EXISTS unaccent"
assert_allowed om_api "DML on a table om_migrate just created (default privileges)" \
    "INSERT INTO h8_new_table (id, v) VALUES (1, 'x'); SELECT * FROM h8_new_table; DELETE FROM h8_new_table"
assert_denied om_migrate "COPY ... FROM PROGRAM is refused for the migrator too" \
    "COPY h8_new_table FROM PROGRAM 'id'" "permission denied"
assert_denied om_migrate "CREATE ROLE is refused for the migrator too" \
    "CREATE ROLE h8_escalation2 SUPERUSER"
psql_as om_migrate -c "DROP TABLE h8_new_table; ALTER TABLE mentors DROP COLUMN h8_probe_col" >/dev/null

# ---------------------------------------------------------------------------
case_start "7. om_backup can dump and cannot write"
assert_allowed om_backup "SELECT on mentors" "SELECT count(*) FROM mentors"
assert_denied om_backup "INSERT is refused" \
    "INSERT INTO mentors (slug, name, status) VALUES ('h8-b', 'b', 'active')" "permission denied"
assert_denied om_backup "COPY ... FROM PROGRAM is refused" \
    "COPY mentors FROM PROGRAM 'id'" "permission denied"
if OUT=$(docker exec "$CONTAINER" pg_dump -U om_backup -d openmentor -Fc -f /tmp/h8.dump 2>&1); then
    ok "pg_dump -Fc succeeds as om_backup"
else
    bad "pg_dump as om_backup failed: $OUT"
fi

# ---------------------------------------------------------------------------
case_start "8. the monitoring read set excludes PII"
assert_allowed om_monitor_probe "SELECT on tags" "SELECT count(*) FROM tags"
for t in mentors client_requests moderators reviews; do
    assert_denied om_monitor_probe "SELECT on $t is refused" \
        "SELECT count(*) FROM $t" "permission denied"
done

# ---------------------------------------------------------------------------
case_start "9. re-applying the migration does not lock a live service out"
psql_as openmentor -c "ALTER ROLE om_api PASSWORD 'h8-identity-test-fixture'" >/dev/null
BEFORE=$(psql_as openmentor -c \
    "SELECT rolcanlogin::text || ':' || (rolpassword IS NOT NULL)::text FROM pg_authid WHERE rolname = 'om_api'")
if OUT=$(docker exec -i "$CONTAINER" psql -v ON_ERROR_STOP=1 -X -q -1 -U openmentor -d openmentor \
        < "$MIGRATIONS/000012_split_database_identities.up.sql" 2>&1); then
    ok "000012 re-applies cleanly"
else
    bad "000012 is not idempotent: $OUT"
fi
AFTER=$(psql_as openmentor -c \
    "SELECT rolcanlogin::text || ':' || (rolpassword IS NOT NULL)::text FROM pg_authid WHERE rolname = 'om_api'")
# Only the shape is compared; the value never leaves the database.
if [ "$BEFORE" = "$AFTER" ] && [ "$(printf '%s' "$AFTER" | tr -d ' \n')" = "true:true" ]; then
    ok "om_api keeps LOGIN and its password across a re-run"
else
    bad "re-running 000012 changed om_api's login state ($BEFORE -> $AFTER)"
fi
assert_allowed om_api "om_api still works after the re-run" "SELECT count(*) FROM mentors"

# ---------------------------------------------------------------------------
case_start "10. the down migration reverses cleanly"
if OUT=$(docker exec -i "$CONTAINER" psql -v ON_ERROR_STOP=1 -X -q -1 -U openmentor -d openmentor \
        < "$MIGRATIONS/000012_split_database_identities.down.sql" 2>&1); then
    ok "000012 down applies"
else
    bad "000012 down failed: $OUT"
fi
assert_query openmentor "the roles are gone" \
    "SELECT count(*) FROM pg_roles WHERE rolname IN ('om_migrate','om_api','om_worker','om_backup','om_monitor_ro')" \
    0
assert_query openmentor "ownership is back with the bootstrap superuser" \
    "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relowner <> 'openmentor'::regrole" \
    0
assert_allowed openmentor "the schema still works after the reversal" "SELECT count(*) FROM mentors"
if OUT=$(docker exec -i "$CONTAINER" psql -v ON_ERROR_STOP=1 -X -q -1 -U openmentor -d openmentor \
        < "$MIGRATIONS/000012_split_database_identities.up.sql" 2>&1); then
    ok "and 000012 can be applied again afterwards"
else
    bad "re-applying 000012 after the down failed: $OUT"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "✅ database identity tests passed"
else
    echo "❌ $FAILURES database identity assertion(s) failed"
    exit 1
fi
