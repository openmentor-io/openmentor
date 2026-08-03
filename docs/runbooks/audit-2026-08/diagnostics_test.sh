#!/usr/bin/env bash
#
# diagnostics_test.sh — regression tests for diagnostics.sh and diagnostics.sql.
#
# These are operator tools for a live system, so what they protect is safety:
# the report never lands in a file whose mode we did not set, no detail row ever
# reaches the console, an ambiguous connection is refused rather than guessed,
# and the report is one consistent snapshot.
#
# Two tiers.
#
# SHELL tier — always runs, needs no database. Puts a stub `psql` on PATH and
# drives diagnostics.sh through its file-handling, error and connection-choice
# paths. Also checks diagnostics.sql structurally, which is what keeps the D3
# detail query and its duplicated summary predicate from drifting apart.
#
# DATABASE tier — opt-in. Applies api/migrations/*.up.sql to a throwaway
# database, seeds fixtures and runs diagnostics.sql for real.
#
#     docker run -d --name pg-diagtest -e POSTGRES_USER=openmentor \
#         -e POSTGRES_PASSWORD=scratch -e POSTGRES_DB=openmentor \
#         -p 55444:5432 postgres:16.14-alpine
#     PGHOST=127.0.0.1 PGPORT=55444 PGUSER=openmentor PGPASSWORD=scratch \
#         PGDATABASE=openmentor OM_DIAG_TEST_DB=1 ./diagnostics_test.sh
#     docker rm -f pg-diagtest
#
# It CREATEs and DROPs a database named om_diag_test and writes only there.
# NEVER point it at production: it is the only file in this directory that
# writes anything.
#
# Exit 0 = every assertion passed. Exit 1 = at least one failed.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIAG_SH="$SCRIPT_DIR/diagnostics.sh"
DIAG_SQL="$SCRIPT_DIR/diagnostics.sql"
MIGRATIONS_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)/api/migrations"
TEST_DB="om_diag_test"

# Stands in for a mentor name / email / request body. If this string reaches the
# console in any test, real personal data would too.
CANARY='CANARY-PERSONAL-DATA-mentor@example.invalid'
STUB_DSN='postgresql://someone@dev.invalid:5432/dev'

PASS=0
FAIL=0
DB_CREATED=0

pass()    { PASS=$((PASS + 1)); printf '  ok    %s\n' "$1"; }
fail()    { FAIL=$((FAIL + 1)); printf '  FAIL  %s\n' "$1"; [ $# -lt 2 ] || printf '        %s\n' "$2"; }
skip()    { printf '  skip  %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

assert_eq() {
    if [ "$2" = "$3" ]; then pass "$1"; else fail "$1" "expected [$3], got [$2]"; fi
}
assert_contains() {
    if printf '%s' "$2" | grep -qF -- "$3"; then pass "$1"; else fail "$1" "missing [$3]"; fi
}
assert_absent() {
    if printf '%s' "$2" | grep -qF -- "$3"; then fail "$1" "found [$3], which must never appear here"; else pass "$1"; fi
}
assert_no_file() {
    if [ -e "$2" ]; then fail "$1" "$2 exists"; else pass "$1"; fi
}

# Octal permission bits, GNU stat or BSD stat.
file_mode() {
    stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1" 2>/dev/null
}

WORK="$(mktemp -d "${TMPDIR:-/tmp}/om-diag-test.XXXXXX")"
finish() {
    rm -rf "$WORK"
    if [ "$DB_CREATED" = 1 ]; then
        psql -X -q -c "DROP DATABASE IF EXISTS $TEST_DB" >/dev/null 2>&1 || true
    fi
}
trap finish EXIT

# ---------------------------------------------------------------------------
# Stub psql. Writes its argv where the test can read it, drains the SQL on
# stdin, prints a detail row containing the canary, and then either succeeds
# with a SUMMARY block or fails the way a statement timeout does.
# ---------------------------------------------------------------------------
STUB_DIR="$WORK/bin"
mkdir -p "$STUB_DIR"
cat > "$STUB_DIR/psql" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$OM_STUB_ARGV"
cat > /dev/null
printf '## D1 — mentors with sort_order IS NULL\n'
printf ' id | email\n----+------\n  1 | %s\n(1 row)\n' "$OM_STUB_CANARY"
if [ "$OM_STUB_MODE" = fail ]; then
    printf 'psql:<stdin>:301: ERROR:  canceling statement due to statement timeout\n' >&2
    exit 3
fi
printf '\n## SUMMARY\n D1 | 1 row\n\n## END\n'
STUB
chmod +x "$STUB_DIR/psql"

RUN_OUT="$WORK/run.out"
RUN_ERR="$WORK/run.err"
RUN_STATUS=0

# run_diag <conn: none|pg|dsn|both> <stub: ok|fail> <argv-file> [diagnostics.sh args...]
#
# The subshell is what keeps the caller's PG* environment intact for the
# database tier below — nothing here leaks out.
run_diag() {
    local conn="$1" stub="$2" argvf="$3"
    shift 3
    (
        unset PGHOST PGHOSTADDR PGPORT PGUSER PGDATABASE PGSERVICE DATABASE_URL
        case "$conn" in
            pg)   export PGHOST=stub.invalid PGDATABASE=stubdb ;;
            dsn)  export DATABASE_URL="$STUB_DSN" ;;
            both) export PGHOST=stub.invalid PGDATABASE=stubdb DATABASE_URL="$STUB_DSN" ;;
            none) : ;;
        esac
        export PATH="$STUB_DIR:$PATH"
        export OM_STUB_ARGV="$argvf" OM_STUB_MODE="$stub" OM_STUB_CANARY="$CANARY"
        "$DIAG_SH" "$@"
    ) >"$RUN_OUT" 2>"$RUN_ERR"
    RUN_STATUS=$?
}

section 'diagnostics.sh — the report file (an existing path keeps its own mode)'

# umask does not apply to an inode that already exists, and `: > file` truncates
# in place and preserves the mode. So a leftover 0644 path would have received
# names, emails and request text at 0644.
EXISTING="$WORK/existing-0644.txt"
printf 'pre-existing content\n' > "$EXISTING"
chmod 644 "$EXISTING"
run_diag pg ok "$WORK/argv" --local --out "$EXISTING"
assert_eq       'refuses an existing report path (exit 3)' "$RUN_STATUS" 3
assert_contains 'says why it refused'                      "$(cat "$RUN_ERR")" 'report path already exists'
assert_eq       'leaves the existing content alone'        "$(cat "$EXISTING")" 'pre-existing content'
assert_eq       'leaves the existing mode alone'           "$(file_mode "$EXISTING")" '644'

SYMLINK="$WORK/link.txt"
ln -s "$WORK/symlink-target.txt" "$SYMLINK"
run_diag pg ok "$WORK/argv" --local --out "$SYMLINK"
assert_eq      'refuses a symlink report path (exit 3)' "$RUN_STATUS" 3
assert_no_file 'does not create the symlink target'     "$WORK/symlink-target.txt"

FRESH="$WORK/fresh.txt"
run_diag pg ok "$WORK/argv" --local --out "$FRESH"
assert_eq       'accepts a new report path (exit 0)'   "$RUN_STATUS" 0
assert_eq       'creates the report as 0600'           "$(file_mode "$FRESH")" '600'
assert_contains 'the report holds the detail rows'     "$(cat "$FRESH")" "$CANARY"
assert_no_file  'removes its stderr scratch file'      "$FRESH.psql-stderr"

section 'diagnostics.sh — the console never carries detail rows'

run_diag pg ok "$WORK/argv" --local --out "$WORK/summary-only.txt"
assert_absent   'success: stdout has no detail rows'    "$(cat "$RUN_OUT")" "$CANARY"
assert_contains 'success: stdout has the SUMMARY block' "$(cat "$RUN_OUT")" '## SUMMARY'

FAILREPORT="$WORK/failed.txt"
run_diag pg fail "$WORK/argv" --local --out "$FAILREPORT"
assert_eq       'failure: exits 1'                            "$RUN_STATUS" 1
assert_absent   'failure: stderr has no detail rows'          "$(cat "$RUN_ERR")" "$CANARY"
assert_absent   'failure: stdout has no detail rows'          "$(cat "$RUN_OUT")" "$CANARY"
assert_contains 'failure: stderr carries psql diagnostics'    "$(cat "$RUN_ERR")" 'statement timeout'
assert_contains 'failure: stderr names the report path'       "$(cat "$RUN_ERR")" "$FAILREPORT"
assert_contains 'failure: the report still holds the rows'    "$(cat "$FAILREPORT")" "$CANARY"
assert_contains 'failure: the report holds the diagnostics'   "$(cat "$FAILREPORT")" 'statement timeout'
assert_no_file  'failure: removes its stderr scratch file'    "$FAILREPORT.psql-stderr"

section 'diagnostics.sh — connection selection'

# libpq gives a DSN argument precedence over PGHOST/PGDATABASE, so accepting
# both silently would report on a different database than the operator named.
run_diag both ok "$WORK/argv" --local --out "$WORK/ambiguous.txt"
assert_eq       'DATABASE_URL + PG* is refused (exit 2)' "$RUN_STATUS" 2
assert_contains 'explains the ambiguity'                 "$(cat "$RUN_ERR")" 'ambiguous connection'
assert_contains 'names the offending variables'          "$(cat "$RUN_ERR")" 'PGHOST'
assert_no_file  'writes no report when refusing'         "$WORK/ambiguous.txt"

run_diag dsn ok "$WORK/argv-dsn" --local --out "$WORK/dsn-only.txt"
assert_eq       'DATABASE_URL alone is still used (exit 0)' "$RUN_STATUS" 0
# shellcheck disable=SC2016  # the literal '$DATABASE_URL' is what the script prints
assert_contains 'labels the DATABASE_URL target'           "$(cat "$RUN_OUT")" 'connection from $DATABASE_URL'
assert_contains 'passes the DSN to psql'                   "$(cat "$WORK/argv-dsn")" "$STUB_DSN"

run_diag pg ok "$WORK/argv-pg" --local --out "$WORK/pg-only.txt"
assert_eq       'PG* alone is still used (exit 0)' "$RUN_STATUS" 0
assert_contains 'labels the PG* target'           "$(cat "$RUN_OUT")" 'connection from the PG* environment'
assert_absent   'passes no DSN to psql'           "$(cat "$WORK/argv-pg")" 'postgresql://'

# An explicit DSN after `--` is the documented way out of the ambiguity.
run_diag both ok "$WORK/argv-pass" --local --out "$WORK/passthrough.txt" -- 'service=openmentor-prod'
assert_eq       'an explicit DSN after -- resolves the ambiguity' "$RUN_STATUS" 0
assert_contains 'uses the passthrough args'                       "$(cat "$WORK/argv-pass")" 'service=openmentor-prod'

section 'diagnostics.sh — dry run touches nothing'

run_diag pg ok "$WORK/argv" --local --dry-run --out "$WORK/dryrun.txt"
assert_eq      'dry run exits 0'            "$RUN_STATUS" 0
assert_no_file 'dry run creates no report'  "$WORK/dryrun.txt"
run_diag pg ok "$WORK/argv" --local --dry-run --out "$EXISTING"
assert_contains 'dry run flags an unusable report path' "$(cat "$RUN_OUT")" 'BLOCKED'

section 'diagnostics.sql — structure'

count_of() { grep -cF -- "$1" "$DIAG_SQL"; }
sql_body="$(cat "$DIAG_SQL")"

# Every D3 sink predicate has to appear in BOTH the detail query and its copy
# inside the SUMMARY block. Two occurrences each — four for `name`, which covers
# two tables — is what "keep the two in sync" means mechanically.
assert_eq 'D3 sink in detail + summary: description' \
    "$(count_of "description ~* '<\\s*(a|img|div|table|script|style)\\y'")" 2
assert_eq 'D3 sink in detail + summary: name (2 tables)' \
    "$(count_of "name ~* '<\\s*[a-z]'")" 4
assert_eq 'D3 sink in detail + summary: preferred_contact' \
    "$(count_of "preferred_contact ~* '<\\s*[a-z]'")" 2
assert_eq 'D3 sink in detail + summary: price' \
    "$(count_of "price ~* '<\\s*[a-z]'")" 2
assert_eq 'D3 sink in detail + summary: mentor_review' \
    "$(count_of "mentor_review ~* '<\\s*[a-z]'")" 2
assert_eq 'D3 sink in detail + summary: platform_review' \
    "$(count_of "platform_review ~* '<\\s*[a-z]'")" 2
assert_eq 'D3 sink in detail + summary: calendar_url' \
    "$(count_of "calendar_url !~* '^https://'")" 2
assert_contains 'D3 labels the preferred_contact rows' "$sql_body" "'client_requests.preferred_contact'"
assert_contains 'D3 labels the price rows'             "$sql_body" "'mentors.price'"

assert_eq 'exactly one read-only REPEATABLE READ transaction' \
    "$(count_of 'BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;')" 1
assert_eq 'exactly one COMMIT' "$(count_of 'COMMIT;')" 1

section 'diagnostics.sql — behaviour against a scratch database'

db_tier_ready() {
    if [ "${OM_DIAG_TEST_DB:-0}" != 1 ]; then
        skip 'database tier (set OM_DIAG_TEST_DB=1 and point PG* at a SCRATCH database)'
        return 1
    fi
    if ! command -v psql >/dev/null 2>&1; then
        skip 'database tier (psql is not on PATH)'
        return 1
    fi
    if [ ! -d "$MIGRATIONS_DIR" ]; then
        skip "database tier (no migrations at $MIGRATIONS_DIR)"
        return 1
    fi
    if ! psql -X -q -c 'SELECT 1' >/dev/null 2>&1; then
        skip 'database tier (cannot connect with the current PG* settings)'
        return 1
    fi
    if ! psql -X -q -c "DROP DATABASE IF EXISTS $TEST_DB" >/dev/null 2>&1 \
       || ! psql -X -q -c "CREATE DATABASE $TEST_DB" >/dev/null 2>&1; then
        skip "database tier (cannot CREATE DATABASE $TEST_DB with these credentials)"
        return 1
    fi
    DB_CREATED=1
    return 0
}

test_psql() { psql -X -q -v ON_ERROR_STOP=1 -d "$TEST_DB" "$@"; }

if db_tier_ready; then
    migrate_ok=1
    for f in "$MIGRATIONS_DIR"/*.up.sql; do
        test_psql -f "$f" >/dev/null 2>>"$WORK/migrate.err" || migrate_ok=0
    done
    assert_eq 'api/migrations/*.up.sql apply cleanly' "$migrate_ok" 1

    test_psql -f - >/dev/null 2>&1 <<'FIXTURES'
INSERT INTO mentors (id, slug, name, status, email, sort_order, activated_at, airtable_id, price, experience, calendar_url)
VALUES
  ('11111111-1111-1111-1111-111111111111','stuck-one','Stuck One','draft','stuck@example.invalid',
   NULL, NULL, NULL, '$50', '2-5', NULL),
  ('22222222-2222-2222-2222-222222222222','imported-one','Imported One','inactive','imported@example.invalid',
   NULL, NULL, 'getmentor:4242', '$30 / hour', 'lots', NULL),
  -- mentor-supplied price reaches {{request_price}} in new-request /
  -- new-request-calendly, unescaped
  ('44444444-4444-4444-4444-444444444444','price-injected','Price Injected','active','priceinj@example.invalid',
   11, now(), NULL, '<a href="https://evil.example/pay">$50</a>', '10+', NULL),
  ('55555555-5555-5555-5555-555555555555','clean','Clean Mentor','active','clean@example.invalid',
   12, now(), NULL, '$100', '10+', 'https://cal.example/clean');

INSERT INTO client_requests (id, mentor_id, email, name, preferred_contact, description, level, status)
VALUES
  -- mentee-supplied preferred_contact reaches {{mentee_contact}} in
  -- new-request-mentor, unescaped
  ('aaaaaaaa-0000-0000-0000-000000000001','55555555-5555-5555-5555-555555555555','mentee1@example.invalid',
   'Mentee One','<a href="javascript:alert(1)">telegram</a>','Please help me with Go.','Junior','done'),
  ('aaaaaaaa-0000-0000-0000-000000000002','55555555-5555-5555-5555-555555555555','mentee2@example.invalid',
   'Mentee Two','telegram: @two','Career advice please.','Senior','done'),
  ('aaaaaaaa-0000-0000-0000-000000000003','55555555-5555-5555-5555-555555555555','mentee3@example.invalid',
   'Mentee Three','signal','Hello <img src=x onerror=alert(1)> there','Middle','pending');

INSERT INTO reviews (client_request_id, mentor_review, platform_review)
VALUES ('aaaaaaaa-0000-0000-0000-000000000002','Great mentor, 10/10','nice site');
FIXTURES

    report="$WORK/db-report.txt"
    if psql -X -d "$TEST_DB" -v ON_ERROR_STOP=1 -f "$DIAG_SQL" >"$report" 2>"$WORK/db.err"; then
        pass 'diagnostics.sql runs clean against a migrated database'
    else
        fail 'diagnostics.sql runs clean against a migrated database' "$(cat "$WORK/db.err")"
    fi

    body="$(cat "$report")"
    d3="$(awk '/^## D3 /{f=1;next} /^## D4 /{f=0} f' "$report")"
    assert_contains 'D3 finds the preferred_contact injection'  "$d3" 'client_requests.preferred_contact'
    assert_contains 'D3 finds the price injection'              "$d3" 'mentors.price'
    assert_contains 'D3 still finds the description injection'  "$d3" 'client_requests.description'
    assert_eq       'D3 detail row count' \
        "$(printf '%s\n' "$d3" | grep -cE '\| (client_requests|mentors|reviews)\.')" 3
    assert_contains 'the D3 summary count agrees with the detail rows' "$body" '3 markup / non-https hits'

    # One snapshot: commit a matching row from a second connection after the
    # detail queries have run but before the summary. Without the transaction the
    # summary counts it and the two halves of the report disagree.
    cat > "$WORK/race-insert.sh" <<RACE
#!/usr/bin/env bash
psql -X -q -d "$TEST_DB" -c "INSERT INTO mentors (slug, name, status, sort_order) VALUES ('race-row', 'Race Row', 'draft', NULL)"
RACE
    chmod +x "$WORK/race-insert.sh"

    raced_sql="$WORK/raced.sql"
    awk -v inject="\\\\! $WORK/race-insert.sh" '
        /^\\echo .## SUMMARY./ && !done { print inject; done = 1 }
        { print }
    ' "$DIAG_SQL" > "$raced_sql"
    assert_eq 'the race harness injected its concurrent INSERT' \
        "$(grep -cF 'race-insert.sh' "$raced_sql")" 1

    raced_out="$WORK/raced-report.txt"
    psql -X -d "$TEST_DB" -v ON_ERROR_STOP=1 -f "$raced_sql" >"$raced_out" 2>&1
    d1_detail="$(awk '/^## D1 /{f=1;next} /^## D1b/{f=0} f' "$raced_out" | grep -cE '^ [0-9a-f]{8}-')"
    d1_summary="$(sed -n 's/.*| *\([0-9]*\) of [0-9]* mentor row(s) have sort_order IS NULL.*/\1/p' "$raced_out")"
    assert_eq 'the summary D1 count matches the D1 detail rows despite a concurrent INSERT' \
        "$d1_summary" "$d1_detail"
fi

section 'result'
printf '  %d passed, %d failed\n\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
