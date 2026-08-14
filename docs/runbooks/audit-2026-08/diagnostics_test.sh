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
# detail query and its duplicated summary predicate from drifting apart, and
# checks the prose runbooks for instructions that were removed because an
# operator following them would damage data or verify nothing. Documentation is
# not usually testable, but these particular sentences are executable: they are
# commands someone pastes into a production shell.
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
# It CREATEs a database named om_diag_test_<pid>_<random> and writes only there,
# and it DROPs only a database this run created — a name that already exists is
# left alone and the tier skips. NEVER point it at production: it is the only
# file in this directory that writes anything.
#
# Exit 0 = every assertion passed. Exit 1 = at least one failed.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIAG_SH="$SCRIPT_DIR/diagnostics.sh"
DIAG_SQL="$SCRIPT_DIR/diagnostics.sql"
REPAIR_MD="$SCRIPT_DIR/data-repair.md"
DECISIONS_MD="$SCRIPT_DIR/outstanding-decisions.md"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
PLAN_MD="$REPO_ROOT/docs/audit/2026-08-remediation-plan.md"
MIGRATIONS_DIR="$REPO_ROOT/api/migrations"
# Unique per run, and never dropped unless THIS run created it (see
# db_tier_ready / finish). A fixed `om_diag_test` plus a `DROP DATABASE IF
# EXISTS` before the CREATE meant that pointing PG* at a shared server destroyed
# whatever already owned that name — the scratch-database warning above is a
# warning, not a guard.
TEST_DB="om_diag_test_$$_${RANDOM}"
# The old fixed name. Nothing here may touch it; the database tier asserts that.
LEGACY_TEST_DB="om_diag_test"

# Stands in for a mentor name / email / request body. If this string reaches the
# console in any test, real personal data would too.
CANARY='CANARY-PERSONAL-DATA-mentor@example.invalid'
STUB_DSN='postgresql://someone@dev.invalid:5432/dev'

PASS=0
FAIL=0
DB_CREATED=0
BYSTANDER_CREATED=0

pass()    { PASS=$((PASS + 1)); printf '  ok    %s\n' "$1"; }
fail()    { FAIL=$((FAIL + 1)); printf '  FAIL  %s\n' "$1"; [ $# -lt 2 ] || printf '        %s\n' "$2"; }
skip()    { printf '  skip  %s\n' "$1"; }
section() { printf '\n%s\n' "$1"; }

assert_eq() {
    if [ "$2" = "$3" ]; then pass "$1"; else fail "$1" "expected [$3], got [$2]"; fi
}
# A here-string, not `printf | grep`: grep -q exits on the first match, and on a
# haystack larger than the pipe buffer that leaves printf with SIGPIPE, whose
# non-zero status `pipefail` then promotes into the pipeline's — so a matched
# needle reported as missing. Bit when these assertions started reading whole
# documents instead of short reports.
assert_contains() {
    if grep -qF -- "$3" <<<"$2"; then pass "$1"; else fail "$1" "missing [$3]"; fi
}
assert_absent() {
    if grep -qF -- "$3" <<<"$2"; then fail "$1" "found [$3], which must never appear here"; else pass "$1"; fi
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
    # Only ever drop databases this run created — DB_CREATED for the scratch
    # database, BYSTANDER_CREATED for the legacy-name canary below.
    if [ "$DB_CREATED" = 1 ]; then
        psql -X -q -c "DROP DATABASE IF EXISTS $TEST_DB" >/dev/null 2>&1 || true
    fi
    if [ "$BYSTANDER_CREATED" = 1 ]; then
        psql -X -q -c "DROP DATABASE IF EXISTS $LEGACY_TEST_DB" >/dev/null 2>&1 || true
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

section 'diagnostics.sh — PSQL_ARGS may only define a connection'

# psql processes command/file options in command-line order, so a passthrough
# -c/-f runs BEFORE the wrapper's own `-f -` — i.e. before diagnostics.sql sets
# default_transaction_read_only. Measured against a scratch Postgres on the
# unguarded script: `-- -c 'CREATE TABLE …'`, `-- -wc 'CREATE TABLE …'`,
# `-- -f other.sql` and `-- --command='CREATE TABLE …'` each created their table
# on a run that reported itself read-only. Hence an allowlist, and hence these
# rejections.
#
# reject_case <label> <report-name> <args...>
reject_case() {
    local label="$1" name="$2"
    shift 2
    run_diag pg ok "$WORK/argv-reject" --local --out "$WORK/$name" -- "$@"
    assert_eq      "refuses $label (exit 2)" "$RUN_STATUS" 2
    assert_no_file "writes no report for $label" "$WORK/$name"
    assert_contains "explains why it refused $label" "$(cat "$RUN_ERR")" 'may only define a connection'
    # The stub records its argv; if the option had reached psql it would be here.
    assert_no_file "never invokes psql for $label" "$WORK/argv-reject"
}

reject_case '-c (execute a command)'          r-c.txt          -c 'CREATE TABLE t (x int)'
reject_case '--command= ("="-joined)'         r-command.txt    --command=SELECT
reject_case '--command (space-separated)'     r-command2.txt   --command 'SELECT 1'
reject_case '-f (read commands from a file)'  r-f.txt          -f /tmp/other.sql
reject_case '--file='                         r-file.txt       --file=/tmp/other.sql
reject_case '-o (write output to a file)'     r-o.txt          -o /tmp/out.txt
reject_case '-l (list databases and exit)'    r-l.txt          -l
reject_case '-1 (single transaction)'         r-1.txt          -1
reject_case '-v (set a psql variable)'        r-v.txt          -v ON_ERROR_STOP=0
reject_case 'a bare - (SQL from stdin)'       r-dash.txt       -
# `-wc 'SQL'` is the sharp one: -w takes no value, so psql reads the SECOND
# letter as the option. A per-token first-letter check that stopped at `w` would
# wave this straight through.
reject_case 'a bundled short option (-wc)'    r-bundle.txt     -wc 'CREATE TABLE t (x int)'
reject_case 'a bundle after a valid flag'     r-bundle2.txt    -w -Wc 'SELECT 1'
# An option smuggled AFTER a legitimate connection option, so the walk cannot
# stop at the first accepted token.
reject_case '-c following a valid -h'         r-after.txt      -h db.invalid -c 'SELECT 1'
reject_case '-c following an attached -p'     r-after2.txt     -p5432 -c 'SELECT 1'

# A rejection must not echo the VALUE: a DSN can embed a password.
run_diag pg ok "$WORK/argv-secret" --local --out "$WORK/r-secret.txt" \
    -- 'postgresql://u:hunter2@db.invalid/x' -c 'SELECT 1'
assert_eq     'refuses an execution option next to a DSN' "$RUN_STATUS" 2
assert_absent 'and never echoes the DSN it was passed'    "$(cat "$RUN_ERR")" 'hunter2'

# Everything that DOES define a connection still gets through, in every spelling
# psql accepts — otherwise this guard would just break the documented workstation
# path.
accept_case() {
    local label="$1" name="$2"
    shift 2
    run_diag none ok "$WORK/argv-$name" --local --out "$WORK/$name" -- "$@"
    assert_eq       "accepts $label (exit 0)" "$RUN_STATUS" 0
    assert_contains "passes $label to psql"   "$(cat "$WORK/argv-$name")" "$1"
}

accept_case '-h with a separate value'   a-h.txt        -h db.invalid
accept_case '-h with an attached value'  a-hattached.txt -hdb.invalid
accept_case '--host='                    a-hosteq.txt   --host=db.invalid
accept_case '--host with a value'        a-host.txt     --host db.invalid
accept_case '-U'                         a-u.txt        -U openmentor
accept_case '-d'                         a-d.txt        -d openmentor
accept_case '-w (no password prompt)'    a-w.txt        -w
accept_case 'a service= positional'      a-svc.txt      service=openmentor-prod

run_diag none ok "$WORK/argv-multi" --local --out "$WORK/a-multi.txt" \
    -- -h db.invalid -p 5432 -U openmentor -d openmentor -w
assert_eq       'accepts a full connection spelled out' "$RUN_STATUS" 0
assert_contains 'and passes all of it through'          "$(cat "$WORK/argv-multi")" 'openmentor'

section 'diagnostics.sh — dry run touches nothing, and its exit code is a preflight'

run_diag pg ok "$WORK/argv" --local --dry-run --out "$WORK/dryrun.txt"
assert_eq      'dry run exits 0 when every precondition is met' "$RUN_STATUS" 0
assert_no_file 'dry run creates no report'                      "$WORK/dryrun.txt"

# A dry run that prints MISSING/BLOCKED and still exits 0 makes an automated
# preflight report success for a run that cannot start — and the real run exits 3
# on these same two checks. The container name below does not exist, so this is
# deterministic whether or not docker is installed (missing binary and missing
# container are both MISSING).
run_diag none ok "$WORK/argv" --dry-run --docker \
    --container "om-diag-absent-$$" --out "$WORK/dry-docker.txt"
assert_eq       'dry run exits 3 on a missing precondition' "$RUN_STATUS" 3
assert_contains 'dry run says what is missing'              "$(cat "$RUN_OUT")" 'MISSING'
assert_contains 'dry run says a real run could not connect' "$(cat "$RUN_OUT")" 'would NOT get as far'
assert_no_file  'a blocked dry run still writes nothing'    "$WORK/dry-docker.txt"

# Same contract for the report path: a real run refuses an existing path with
# exit 3, so the preflight has to as well.
run_diag pg ok "$WORK/argv" --local --dry-run --out "$EXISTING"
assert_contains 'dry run flags an unusable report path'  "$(cat "$RUN_OUT")" 'BLOCKED'
assert_eq       'dry run exits 3 on a blocked report path' "$RUN_STATUS" 3
assert_eq       'a blocked dry run leaves that file alone' "$(cat "$EXISTING")" 'pre-existing content'

section 'diagnostics.sql — structure'

count_of() { grep -cF -- "$1" "$DIAG_SQL"; }
sql_body="$(cat "$DIAG_SQL")"
# Executable lines only. The comments deliberately quote the predicates that were
# REMOVED and say why, so an "this must not appear" check has to read the SQL and
# not the prose around it.
sql_statements="$(grep -v '^[[:space:]]*--' "$DIAG_SQL")"

# Every D3 sink predicate has to appear in BOTH the detail query and its copy
# inside the SUMMARY block. Two occurrences each — four for `name`, which covers
# two tables — is what "keep the two in sync" means mechanically.
#
# `description` is checked with the SAME permissive predicate as every other
# free-text sink. It used to carry a tag allowlist,
# `<\s*(a|img|div|table|script|style)\y`, which reported `<form>`, `<iframe>`,
# `<svg>` and `<input>` clean even though all four reach the unescaped
# {{request_details}} / {{mentee_request}} interpolation from an unauthenticated
# contact request. `assert_absent` below is what stops that coming back.
assert_eq 'D3 sink in detail + summary: description' \
    "$(count_of "description ~* '<\\s*[a-z]'")" 2
assert_absent 'D3 no longer matches description against a list of tag names' \
    "$sql_statements" '(a|img|div|table|script|style)'
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
# moderation_note -> {{reviewer_note}} in new-mentor-returned
# (job_mentor_moderation.go:166-174), unescaped, and listed in the plan's own P6
# sink table. D3 used to jump from calendar URLs straight to the review fields.
assert_eq 'D3 sink in detail + summary: moderation_note' \
    "$(count_of "moderation_note ~* '<\\s*[a-z]'")" 2
assert_contains 'D3 labels the moderation_note rows' "$sql_body" "'mentors.moderation_note'"
assert_eq 'D3 sink in detail + summary: calendar_url scheme' \
    "$(count_of "calendar_url !~* '^https://'")" 2
# The scheme test alone passes `https://evil.example/x"><img src=x>`, which is a
# complete breakout of href="{{calendly_url}}". Both copies must also match the
# characters that end an HTML attribute.
assert_eq 'D3 sink in detail + summary: calendar_url markup' \
    "$(count_of "calendar_url ~ '[<>\"''\`[:space:]]'")" 2
assert_contains 'D3 labels the preferred_contact rows' "$sql_body" "'client_requests.preferred_contact'"
assert_contains 'D3 labels the price rows'             "$sql_body" "'mentors.price'"

assert_eq 'exactly one read-only REPEATABLE READ transaction' \
    "$(count_of 'BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;')" 1
assert_eq 'exactly one COMMIT' "$(count_of 'COMMIT;')" 1

# `price` is nullable TEXT with no default (migration 000001), so a NULL is a
# value the schema permits and is not one of the six select options either — and
# it is the worse case, since ScanMentor fails the whole row on it
# (models/mentor.go:23,138: Mentor.Price is a plain string). A `price IS NOT NULL`
# filter therefore hid an exposed row from D2b, D2c and the summary count.
# `experience` has the same shape, which is why D2d always counted NULL.
assert_absent 'D2 no longer filters NULL prices out of the exposure set' \
    "$sql_statements" 'price IS NOT NULL'
assert_eq 'NULL prices counted in D2b, D2c and the summary' \
    "$(count_of 'WHERE price IS NULL')" 3
assert_eq 'D2d still counts NULL experience the same way' \
    "$(count_of 'experience IS NULL')" 2

# ---------------------------------------------------------------------------
# Prose tiers. The blocks below are commands an operator pastes into a
# production shell, so they get pinned like code: each assertion corresponds to
# an instruction that was removed because following it damaged data or verified
# nothing. Claims the audit originally made are quoted in the plan's own
# corrections section (§4.1) and must survive there — that is why some of these
# assert a count of one rather than absence.
# ---------------------------------------------------------------------------
count_in() { grep -cF -- "$2" "$1"; }

# Counts fenced ```sql blocks that BOTH open a transaction and commit it. Must be
# zero: an operator pastes a block whole, so a trailing COMMIT executes before
# they have read the row count the guards produce — committing a partial repair
# that the very next sentence tells them to ROLLBACK. The commit has to be its
# own block.
self_committing_sql_blocks() {
    awk '
        /^```sql$/           { inb = 1; begun = 0; committed = 0; next }
        inb && /^```$/       { if (begun && committed) n++; inb = 0; next }
        inb && /^BEGIN;/     { begun = 1 }
        inb && /^COMMIT;/    { committed = 1 }
        END { print n + 0 }' "$1"
}

# Counts `aws` command lines that are NOT inside a `docker exec … sh -c '…'`
# block. Must be zero in every runbook: the VM holds no AWS credentials, so an
# `aws` call anywhere else fails before the operator gets started.
#
# A `sh -c '…'` span can open AND close on one line — prose, or a table cell
# citing the form. The first version of this treated any such line as opening a
# multi-line block and never closed it, so ONE inline mention blinded the check
# for the whole rest of the file: the `error=s3_upload_failed` row in
# postgres-backup-restore.md's failure-mode table is exactly that, and a bare
# `aws s3 cp` appended after it was counted as 0. So consume
# closed spans in place and only enter block state on an unterminated opener,
# then test what is LEFT of the line.
bare_aws_lines() {
    awk -v q="'" '
        {
            line = $0
            if (inblock) {
                c = index(line, q)
                if (c == 0) { next }            # still inside the sh -c quote
                inblock = 0
                line = substr(line, c + 1)
            }
            while ((p = index(line, "sh -c " q)) > 0) {
                line = substr(line, p + length("sh -c " q))
                c = index(line, q)
                if (c == 0) { inblock = 1; line = ""; break }
                line = substr(line, c + 1)      # span closed on this same line
            }
            if (line ~ /(^|[[:space:]])aws /) { n++ }
        }
        END { print n + 0 }' "$1"
}

# Every needle below is a literal fragment of markdown, so SC2016 (backticks and
# $VAR inside single quotes) is the point rather than a mistake.
# shellcheck disable=SC2016
check_plan() {
local plan plan_d3_block plan_d2_block plan_p2
section 'remediation plan — corrected operator instructions'

plan="$(cat "$PLAN_MD")"

# The runnable ```sql blocks, separated from the prose around them: §4.1 quotes
# the wording each of these used to have, so an absence check has to look at the
# block an operator actually pastes, not at the whole document.
plan_sql_block() {
    awk -v h="^### $1 " '$0 ~ h {f=1} f && /^```sql$/{inb=1;next} inb && /^```$/{exit} inb' "$PLAN_MD"
}
plan_d3_block="$(plan_sql_block D3)"
plan_d2_block="$(plan_sql_block D2)"
assert_eq 'the plan D3 sql block is extractable' \
    "$([ -n "$plan_d3_block" ] && printf 'yes')" 'yes'
assert_eq 'the plan D2 sql block is extractable' \
    "$([ -n "$plan_d2_block" ] && printf 'yes')" 'yes'

# P2's own item, without §4.1 — which quotes P2's old wording AND describes the
# replacement, so a whole-document grep passes even with the item left wrong.
plan_p2="$(awk '/^### P2 /{f=1} /^### P3 /{f=0} f' "$PLAN_MD")"
assert_eq 'the P2 item is extractable' \
    "$([ -n "$plan_p2" ] && printf 'yes')" 'yes'

# D1 is a mixed bag (imported, active, just-committed rows), and
# NewMentorWatcher rewrites status, re-mints the confirmation token and re-sends
# email. "Finalize every D1 row, the handler is idempotent" is none of those.
# The old wording survives once, quoted in §4.1, which is the audit trail.
assert_eq       '"POST /jobs/..., idempotent" survives only as a quoted correction' \
    "$(count_in "$PLAN_MD" '`POST /jobs/...`, idempotent')" 1
assert_contains 'D1 repair states the handler is not idempotent' \
    "$plan" 'is **not idempotent**'
assert_contains 'D1 repair points at the classified runbook procedure' \
    "$plan" 'runbooks/audit-2026-08/data-repair.md'
assert_contains 'D1 repair keeps the in-flight age cutoff' "$plan" '**15 minutes**'
assert_contains 'D1 repair keeps the immediate re-check'   "$plan" 're-check each row'
# "D1 returns zero rows" is what drove the blanket replay: imported profiles
# never reach zero without damaging them.
assert_absent   'D1 acceptance no longer demands a zero D1' \
    "$plan" '`D1` returns zero rows after repair'
assert_contains 'D1 acceptance targets D1b stuck_registration rows instead' \
    "$plan" 'returns zero `stuck_registration` rows'

# The plan's embedded D3 must carry every sink predicate diagnostics.sql checks.
# It shipped without preferred_contact and price, both of which reach signed
# email unescaped, so a clean D3 read from the plan proved nothing about them.
for pred in \
    "description ~* '<\\s*[a-z]'" \
    "name ~* '<\\s*[a-z]'" \
    "preferred_contact ~* '<\\s*[a-z]'" \
    "price ~* '<\\s*[a-z]'" \
    "moderation_note ~* '<\\s*[a-z]'" \
    "mentor_review ~* '<\\s*[a-z]'" \
    "platform_review ~* '<\\s*[a-z]'" \
    "calendar_url !~* '^https://'" \
    "calendar_url ~ '[<>\"''\`[:space:]]'"
do
    if grep -qF -- "$pred" "$DIAG_SQL" && grep -qF -- "$pred" "$PLAN_MD"; then
        pass "plan D3 matches diagnostics.sql: $pred"
    else
        fail "plan D3 matches diagnostics.sql: $pred" 'the two D3 copies have drifted'
    fi
done
# In PostgreSQL's regex flavour \b is BACKSPACE, so this branch matched nothing.
assert_absent 'plan D3 no longer uses \b as a word boundary' \
    "$plan" '(a|img|div|table|script|style)\b'

# A tag allowlist is a denylist wearing an allowlist's clothes: <form>, <iframe>,
# <svg> and <input> are all absent from it and all reach {{request_details}} /
# {{mentee_request}} unescaped from an UNAUTHENTICATED contact request, so the
# plan's D3 called them clean. The old predicate survives once, in §4.1 item 8,
# with markdown-escaped pipes.
assert_absent 'the plan D3 block matches every tag, not a listed set' \
    "$plan_d3_block" '(a|img|div|table|script|style)'
assert_eq     'the tag allowlist survives only as a quoted correction' \
    "$(count_in "$PLAN_MD" 'a\|img\|div\|table\|script\|style')" 1
assert_contains 'the plan D3 block covers moderation_note' \
    "$plan_d3_block" "moderation_note ~* '<\\s*[a-z]'"
assert_contains 'and names the template prop it reaches' \
    "$plan" '`moderation_note` → `{{reviewer_note}}`'

# D2's exposure query filtered `price IS NOT NULL`, which hid rows whose NULL
# price is the WORSE case — ScanMentor fails the whole row on it. Both the old
# wording (§4.1 item 10) and the changelog entry quote the filter, so this checks
# the paste-able block.
assert_absent 'the plan D2 exposure query no longer filters NULL prices out' \
    "$plan_d2_block" 'price IS NOT NULL'
assert_contains 'and counts them instead' "$plan_d2_block" 'WHERE price IS NULL'

# P2 step 4 told the reader the cron job's SELECTION predicate was what made a
# non-idempotent handler safe. It is not: a scheduled tick overlapping a manual
# POST can have both runs select the same candidate before either writes. Safety
# lives in the write, which is what the implementation on fix/audit-p0-api does.
assert_eq '"the selection is what keeps the job safe" survives only as a quoted correction' \
    "$(count_in "$PLAN_MD" 'the selection is what keeps the job safe')" 1
assert_contains 'P2 step 4 says the predicate is not what makes this safe' \
    "$plan_p2" '**the selection predicate is not what makes this safe**'
assert_contains 'P2 step 4 requires an exclusive claim' "$plan_p2" 'exclusive claim'
assert_contains 'P2 step 4 names the compare-and-swap on the token that was read' \
    "$plan_p2" 'compare-and-swap on the `email_confirmation_token` the run read'
assert_contains 'P2 step 4 gates the email on the affected-row count' \
    "$plan_p2" "Gate the email on the claim's affected-row count"
assert_contains 'P2 step 4 keeps SkipIfStillRunning for scheduled overlap' \
    "$plan_p2" 'cron.SkipIfStillRunning'
assert_contains 'P2 acceptance exercises concurrent finalization' \
    "$plan_p2" 'produces exactly one email'

# P2: FetchAllMentorsFromDB (the public catalog) was the missed fourth query.
assert_contains 'P2 lists all four raw sort_order queries' \
    "$plan" 'mentor_repository.go:115, 379, 513, 547'
assert_contains 'P2 fixes all four'    "$plan" 'in **all four** `ScanMentor` queries'

# P4: the experience select has the identical uncontrolled-select defect.
assert_contains 'P4 covers the experience select' "$plan" '`experience` has the same bug'
assert_contains 'P4 asserts an experience round-trip' "$plan" 'Rendering with `experience:'
assert_eq       '"experience is unaffected" survives only as a quoted correction' \
    "$(count_in "$PLAN_MD" 'is unaffected')" 1

# P6: html/template does not inspect a template.HTML value, so wrapping today's
# concatenated fragment and deleting html.EscapeString re-creates the injection.
assert_absent   'P6 no longer says to wrap the fragments as they are' \
    "$plan" '`template.HTML` at their producing sites'
assert_contains 'P6 says to rebuild the fragments as html/template templates' \
    "$plan" 'as `html/template` templates and return the result as `template.HTML`'
assert_contains 'P6 warns that wrapping alone re-creates the injection' \
    "$plan" 'Do not simply wrap'

# P8: Docker keeps an UNHEALTHY container in `running` state, and both deploy
# gates test only `.State.Status`, so the proposed healthcheck had no consumer —
# verified: `grep -rn 'State.Health' infra/` returns nothing.
assert_contains 'P8 requires the deploy gate to consume the healthcheck' \
    "$plan" '{{.State.Health.Status}}'
assert_contains 'P8 names the deploy check that ignores health' \
    "$plan" 'infra/deploy-remote.sh:173'
assert_contains 'P8 names the rollback check that ignores health' \
    "$plan" 'infra/rollback.sh:209'
assert_contains 'P8 acceptance exercises unhealthy-but-running' \
    "$plan" 'unhealthy but still `running`'

# P14: RequestDistinctID had 26 call sites in 6 producer files. Containing only
# the review and contact flows would have left the capability flowing to PostHog
# from every mentor status change and every worker job.
assert_contains 'P14 says to delete the helper outright' \
    "$plan" 'delete `RequestDistinctID` entirely'
assert_contains 'P14 states the real call-site count' "$plan" '26 call sites'
assert_contains 'P14 acceptance is greppable' "$plan" 'grep -rn RequestDistinctID api/` returns **nothing**'
assert_eq       '"review and contact events" survives only as a quoted correction' \
    "$(count_in "$PLAN_MD" 'stop passing `RequestDistinctID` for review and contact events')" 1

# The plan carries operator SQL too; same rule as data-repair.md.
assert_eq 'no paste-able sql block in the plan commits for the operator' \
    "$(self_committing_sql_blocks "$PLAN_MD")" 0
}
check_plan

# shellcheck disable=SC2016  # literal markdown fragments, as above
check_repair_md() {
local repair ssh_line
section 'data-repair.md — commands that can run where they are written'

repair="$(cat "$REPAIR_MD")"

# The VM holds no AWS credentials (infra/.env.production.example), and the backup
# keys reach only openmentor-postgres-backup — which is also the only container
# carrying aws-cli. A bare `aws s3 ls` on the VM fails before recovery starts.
assert_eq       'no bare aws command outside the backup sidecar' \
    "$(bare_aws_lines "$REPAIR_MD")" 0
assert_contains 'S3 reads run inside the backup sidecar' \
    "$repair" 'docker exec openmentor-postgres-backup sh -c'
assert_contains 'the sidecar maps BACKUP_AWS_* onto AWS_*' \
    "$repair" 'AWS_ACCESS_KEY_ID="$BACKUP_AWS_ACCESS_KEY_ID"'
assert_contains 'the dump is copied out of the sidecar' \
    "$repair" 'docker cp openmentor-postgres-backup:/backups/restore-candidate.dump'

# `docker cp` behaves like `cp -a` and applies the mode from the tar header, so
# the sidecar's 0644 (aws s3 cp under the ordinary 022 umask) lands on the host
# copy — a complete production dump readable by every local user on the VM.
# Measured: `docker cp` of a 0644 file produced 0644 on the host under umask 022
# AND under umask 077, and only the explicit chmod produced 600. So all three
# guards are load-bearing and the chmod is the one that actually fixes it.
assert_contains 'the host dump copy refuses a path it did not create' \
    "$repair" '[ -e /tmp/candidate.dump ] || [ -L /tmp/candidate.dump ]'
assert_contains 'the host dump copy is created under umask 077' \
    "$repair" '( umask 077'
assert_contains 'the host dump copy is forced to mode 600' \
    "$repair" 'chmod 600 /tmp/candidate.dump'
assert_contains 'and the operator is told what mode to expect' \
    "$repair" 'must print -rw------- before you continue'
# umask alone would look like a fix and is not one, so the runbook has to say so.
assert_contains 'the runbook says umask alone does not fix docker cp' \
    "$repair" 'Setting `umask 077` does NOT'
assert_contains 'the scratch restore waits for readiness' "$repair" 'pg_isready'
assert_contains 'the sidecar copy is deleted afterwards' \
    "$repair" 'rm -f /backups/restore-candidate.dump'

# Inside `ssh <vm>`, ./db.sh cannot work: it reads VM_SSH_HOST/VM_SSH_USER from
# infra/.env.production, and deployment puts the live environment in .env.
ssh_line="$(grep -n '^ssh <vm>' "$REPAIR_MD" | head -1 | cut -d: -f1)"
assert_eq 'the runbook still has the ssh step this check depends on' \
    "$([ -n "$ssh_line" ] && printf 'yes')" 'yes'
assert_eq 'no ./db.sh command after the operator has ssh-ed into the VM' \
    "$(awk -v n="${ssh_line:-0}" 'NR > n && /^\.\/db\.sh/' "$REPAIR_MD" | wc -l | tr -d ' ')" 0
assert_contains 'the post-trigger check queries Postgres on the VM' \
    "$repair" 'docker exec openmentor-postgres psql'

# The D2 write-back is a live production repair. Its guards only help if the
# operator sees `UPDATE <n>` before deciding, so no block may commit for them.
assert_eq       'no paste-able sql block both opens a transaction and commits it' \
    "$(self_committing_sql_blocks "$REPAIR_MD")" 0
assert_contains 'the UPDATE block says it stops short of the commit' \
    "$repair" 'ends without `COMMIT`'
assert_contains 'the count is the documented decision point' \
    "$repair" 'read the count before you type anything else'
assert_contains 'ROLLBACK is offered as its own step' "$repair" 'ROLLBACK;'
}
check_repair_md

check_decisions_md() {
local decisions
section 'outstanding-decisions.md — the rehearsal TLS check'

decisions="$(cat "$DECISIONS_MD")"

# A drill server has no certificate for the live hostname, so curl aborts at
# verification and reports http 000 without ever seeing a response — it cannot
# confirm that Traefik and the restored app serve. Reproduced against a local
# self-signed TLS server: `curl -sS --resolve ...` exits 60 and prints 000;
# the same call with -k prints 200 (and ssl_verify_result 18).
# Same credential split as data-repair.md: no bare aws call, in either file.
assert_eq 'no bare aws command outside the backup sidecar' \
    "$(bare_aws_lines "$DECISIONS_MD")" 0
assert_contains 'the dump listing runs inside the backup sidecar' \
    "$decisions" 'docker exec openmentor-postgres-backup sh -c'

assert_eq 'every --resolve probe is explicitly insecure' \
    "$(grep -cE 'curl -sSk .*--resolve' "$DECISIONS_MD")" \
    "$(grep -cE 'curl .*--resolve' "$DECISIONS_MD")"
assert_contains 'the insecure probe says what it does not prove' \
    "$decisions" 'NOTHING about the certificate'
assert_contains 'it explains the 000 an unverified probe reports' "$decisions" 'reports 000'
assert_contains 'a TLS-verifying alternative uses the drill hostname' \
    "$decisions" 'curl -sS https://drill.openmentor.io/'

# The §1 note sized the PostHog cleanup from a file count. `grep -rn
# RequestDistinctID api/` was 27 hits: 26 calls in 6 producer files, plus the
# helper — so "seven call sites" was seven FILES, and understated the scope.
assert_contains 'the D4 note states the real RequestDistinctID scope' \
    "$decisions" '26 call sites in 6 producer files'
assert_absent   'and no longer counts files as call sites' \
    "$decisions" 'from **seven** call sites'
}
check_decisions_md

# ---------------------------------------------------------------------------
# The sidecar-only S3 convention is not specific to the audit runbooks: it comes
# from where the credentials live, so it binds every runbook that touches the
# backup bucket. Until this section existed, reintroducing a bare `aws s3 cp`
# "on the VM" failed `make check` in data-repair.md and passed silently in the
# two operational runbooks — the exact H11 defect, in the files an operator is
# most likely to be holding at 3am.
#
# Note for editors: bare_aws_lines counts any line with an unquoted `aws ` token
# outside a `sh -c '…'` block, so PROSE about the CLI trips it too. Write it as
# `aws` in backticks, the way both runbooks already do; do not loosen the awk.
# ---------------------------------------------------------------------------
# The needles are literal markdown fragments, so SC2016 is the point (as above).
# shellcheck disable=SC2016
check_operational_runbooks() {
local restore_md upgrade_md restore
section 'operational runbooks — S3 only ever runs inside the backup sidecar'

restore_md="$REPO_ROOT/docs/runbooks/postgres-backup-restore.md"
upgrade_md="$REPO_ROOT/docs/runbooks/postgres-16-to-18-upgrade.md"

assert_eq 'postgres-backup-restore.md: no bare aws command outside the backup sidecar' \
    "$(bare_aws_lines "$restore_md")" 0
assert_eq 'postgres-16-to-18-upgrade.md: no bare aws command outside the backup sidecar' \
    "$(bare_aws_lines "$upgrade_md")" 0

# The restore runbook is the one that actually reaches S3, so it must also carry
# the sidecar form itself rather than only asserting the rule.
restore="$(cat "$restore_md")"
assert_contains 'the restore runbook fetches dumps through the sidecar' \
    "$restore" 'docker exec openmentor-postgres-backup sh -c'
assert_contains 'and maps BACKUP_AWS_* onto AWS_* inside the container' \
    "$restore" 'AWS_ACCESS_KEY_ID="$BACKUP_AWS_ACCESS_KEY_ID"'

# The upgrade runbook deliberately owns no S3 block; it delegates. If that
# pointer rots, the next editor's fix is to paste an `aws` line back in.
assert_contains 'the upgrade runbook delegates its S3 step to the restore runbook' \
    "$(cat "$upgrade_md")" 'postgres-backup-restore.md'
}
check_operational_runbooks

section 'diagnostics_test.sh — the scratch database is one it created itself'

# This harness used to CREATE a fixed `om_diag_test` after an unconditional
# `DROP DATABASE IF EXISTS` on it. Pointed at a shared server, that deleted
# whatever already held the name.
assert_eq 'the scratch database name is unique to this run' \
    "$([ "$TEST_DB" != "$LEGACY_TEST_DB" ] && printf 'yes')" 'yes'

# Plant a database under the OLD fixed name, with a marker table, BEFORE setup
# runs — then assert below that setup left it intact. If the name is already
# taken it belongs to somebody else, so we do not touch it and the check is
# simply not made.
plant_legacy_bystander() {
    [ "${OM_DIAG_TEST_DB:-0}" = 1 ] || return 0
    command -v psql >/dev/null 2>&1 || return 0
    psql -X -q -c 'SELECT 1' >/dev/null 2>&1 || return 0
    psql -X -q -c "CREATE DATABASE $LEGACY_TEST_DB" >/dev/null 2>&1 || return 0
    BYSTANDER_CREATED=1
    psql -X -q -d "$LEGACY_TEST_DB" -c 'CREATE TABLE not_yours (x int)' >/dev/null 2>&1 || true
}
plant_legacy_bystander

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
    # CREATE only — no pre-emptive DROP. The name is unique per run, so if the
    # CREATE fails the database is somebody else's and we skip rather than
    # deleting it. DB_CREATED is what licenses the DROP in `finish`.
    if ! psql -X -q -c "CREATE DATABASE $TEST_DB" >/dev/null 2>&1; then
        skip "database tier (cannot CREATE DATABASE $TEST_DB with these credentials)"
        return 1
    fi
    DB_CREATED=1
    return 0
}

test_psql() { psql -X -q -v ON_ERROR_STOP=1 -d "$TEST_DB" "$@"; }

if db_tier_ready; then
    if [ "$BYSTANDER_CREATED" = 1 ]; then
        # Its marker table is still there => setup neither dropped nor recreated
        # a database it did not make. An empty result means the database was
        # replaced under us.
        assert_eq "setup leaves a pre-existing $LEGACY_TEST_DB untouched" \
            "$(psql -X -q -At -d "$LEGACY_TEST_DB" -c 'SELECT count(*) FROM not_yours' 2>/dev/null)" 0
    else
        skip "pre-existing $LEGACY_TEST_DB canary (that name is already in use)"
    fi

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
  -- '$30' rather than the '$30 / hour' this row used to carry: mentors_price_chk
  -- (000014) refuses the free-text shape, and one rejected value in this
  -- multi-row INSERT takes every other fixture row with it. `experience` stays
  -- off-list ('lots') — THAT column still has the uncontrolled-select bug, which
  -- is what D2d is for.
  ('22222222-2222-2222-2222-222222222222','imported-one','Imported One','inactive','imported@example.invalid',
   NULL, NULL, 'getmentor:4242', '$30', 'lots', NULL),
  -- This row used to carry '<a href="https://evil.example/pay">$50</a>': the
  -- mentor-supplied price reaches {{request_price}} in new-request /
  -- new-request-calendly UNESCAPED, so the column was a live injection sink.
  -- mentors_price_chk closes it by construction — markup is no longer a storable
  -- price — so the fixture cannot seed it and D3 has nothing to find here. The
  -- assertion that replaced it proves the stronger property: the INSERT is
  -- refused. D3 keeps its price branch anyway, for a database whose constraint
  -- has been rolled back below 000014.
  ('44444444-4444-4444-4444-444444444444','price-injected','Price Injected','active','priceinj@example.invalid',
   11, now(), NULL, '$50', '10+', NULL),
  ('55555555-5555-5555-5555-555555555555','clean','Clean Mentor','active','clean@example.invalid',
   12, now(), NULL, '$100', '10+', 'https://cal.example/clean'),
  -- calendar_url reaches href="{{calendly_url}}" in new-request-calendly. This
  -- value keeps the https:// scheme and still closes the attribute, so a
  -- scheme-only D3 predicate reports it as clean.
  ('66666666-6666-6666-6666-666666666666','calendar-injected','Calendar Injected','active','calinj@example.invalid',
   13, now(), NULL, '$150', '10+', 'https://evil.example/x"><img src=x onerror=alert(1)>'),
  -- a real Calendly link with query, fragment and sub-delims must NOT match
  ('77777777-7777-7777-7777-777777777777','calendar-ok','Calendar Ok','active','calok@example.invalid',
   14, now(), NULL, '$200', '10+', 'https://calendly.com/m/30min?month=2026-08&back=1#pick'),
  -- price IS NULL: permitted by the schema (`price TEXT`, no default), not one
  -- of the six select options, and the case a `price IS NOT NULL` filter hid.
  ('88888888-8888-8888-8888-888888888888','price-null','Price Null','active','pricenull@example.invalid',
   15, now(), NULL, NULL, '10+', NULL);

-- moderation_note reaches {{reviewer_note}} in new-mentor-returned unescaped
-- (job_mentor_moderation.go:166-174). Set separately: it is not in the column
-- list above, and the point is that D3 has a branch for it at all.
UPDATE mentors SET moderation_note = 'Please fix <iframe src=x> in your bio'
WHERE id = '55555555-5555-5555-5555-555555555555';

INSERT INTO client_requests (id, mentor_id, email, name, preferred_contact, description, level, status)
VALUES
  -- mentee-supplied preferred_contact reaches {{mentee_contact}} in
  -- new-request-mentor, unescaped
  ('aaaaaaaa-0000-0000-0000-000000000001','55555555-5555-5555-5555-555555555555','mentee1@example.invalid',
   'Mentee One','<a href="javascript:alert(1)">telegram</a>','Please help me with Go.','Junior','done'),
  ('aaaaaaaa-0000-0000-0000-000000000002','55555555-5555-5555-5555-555555555555','mentee2@example.invalid',
   'Mentee Two','telegram: @two','Career advice please.','Senior','done'),
  ('aaaaaaaa-0000-0000-0000-000000000003','55555555-5555-5555-5555-555555555555','mentee3@example.invalid',
   'Mentee Three','signal','Hello <img src=x onerror=alert(1)> there','Middle','pending'),
  -- <iframe> is NOT in the tag list D3's description branch used to carry, yet it
  -- reaches {{request_details}} / {{mentee_request}} unescaped from an
  -- unauthenticated contact request. A tag-allowlist predicate calls this clean.
  ('aaaaaaaa-0000-0000-0000-000000000004','55555555-5555-5555-5555-555555555555','mentee4@example.invalid',
   'Mentee Four','email','Hi <iframe src=//evil.example></iframe> please advise','Middle','pending');

INSERT INTO reviews (client_request_id, mentor_review, platform_review)
VALUES ('aaaaaaaa-0000-0000-0000-000000000002','Great mentor, 10/10','nice site');
FIXTURES

    report="$WORK/db-report.txt"
    if psql -X -d "$TEST_DB" -v ON_ERROR_STOP=1 -f "$DIAG_SQL" >"$report" 2>"$WORK/db.err"; then
        pass 'diagnostics.sql runs clean against a migrated database'
    else
        fail 'diagnostics.sql runs clean against a migrated database' "$(cat "$WORK/db.err")"
    fi

    # The plan is handed to operators as self-contained, so its embedded copy of
    # D3 has to run, not just match diagnostics.sql textually.
    plan_d3="$WORK/plan-d3.sql"
    awk '/^### D3 /{f=1} f && /^```sql$/{inb=1;next} inb && /^```$/{exit} inb' \
        "$PLAN_MD" > "$plan_d3"
    if [ -s "$plan_d3" ] && test_psql -f "$plan_d3" >/dev/null 2>"$WORK/plan-d3.err"; then
        pass "the plan's embedded D3 query executes against the schema"
    else
        fail "the plan's embedded D3 query executes against the schema" "$(cat "$WORK/plan-d3.err")"
    fi

    body="$(cat "$report")"
    d3="$(awk '/^## D3 /{f=1;next} /^## D4 /{f=0} f' "$report")"
    assert_contains 'D3 finds the preferred_contact injection'  "$d3" 'client_requests.preferred_contact'
    assert_contains 'D3 still finds the description injection'  "$d3" 'client_requests.description'
    # The https:// payload row. Its id, not just the field label, so this cannot
    # pass on a non-https row instead.
    assert_contains 'D3 finds markup inside an https calendar_url' \
        "$d3" '66666666-6666-6666-6666-666666666666'
    assert_contains 'and labels it as calendar_url'  "$d3" 'mentors.calendar_url'
    assert_absent   'D3 leaves a real Calendly link with query + fragment alone' \
        "$d3" '77777777-7777-7777-7777-777777777777'
    # An unlisted tag in a description. Its id, not just the field label, so this
    # cannot pass on the <img> row that the old tag list already matched.
    assert_contains 'D3 finds an unlisted tag (<iframe>) in a description' \
        "$d3" 'aaaaaaaa-0000-0000-0000-000000000004'
    assert_contains 'D3 finds markup in a moderation note'  "$d3" 'mentors.moderation_note'
    # Five, not six: mentors.price can no longer hold markup at all (D87), so
    # the row that used to supply the sixth hit is refused by the database
    # instead of reported by D3. The assertion below proves that refusal.
    assert_eq       'D3 detail row count' \
        "$(printf '%s\n' "$d3" | grep -cE '\| (client_requests|mentors|reviews)\.')" 5
    assert_contains 'the D3 summary count agrees with the detail rows' "$body" '5 markup / non-https hits'

    # The price injection sink, closed by construction rather than detected.
    if test_psql -c "INSERT INTO mentors (slug, name, status, price) VALUES ('d3-price-markup','X','active','<a href=\"https://evil.example/pay\">\$50</a>')" >/dev/null 2>"$WORK/price-markup.err"; then
        fail 'mentors_price_chk refuses markup in a price' 'the INSERT was accepted'
    elif grep -q 'mentors_price_chk' "$WORK/price-markup.err"; then
        pass 'mentors_price_chk refuses markup in a price'
    else
        fail 'mentors_price_chk refuses markup in a price' "$(cat "$WORK/price-markup.err")"
    fi

    # D2b/D2c/D2d exposure. D2b flags what the GRAMMAR rejects now, not what the
    # old six-option select could not represent (D87) — so '$30' and the former
    # markup price are no longer hits, and the NULL row is the only one left. It
    # is still a hit because mentors_price_chk is a CHECK, and a CHECK is
    # satisfied by NULL; it is also the case a `price IS NOT NULL` filter once
    # dropped from both the count and the value list.
    d2b="$(awk '/^## D2b /{f=1;next} /^## D2c /{f=0} f' "$report")"
    d2c="$(awk '/^## D2c /{f=1;next} /^## D2d /{f=0} f' "$report")"
    assert_eq 'D2b counts the NULL price as off-grammar' \
        "$(printf '%s\n' "$d2b" | grep -oE '^ *[0-9]+' | head -1 | tr -d ' ')" 1
    assert_contains 'D2c shows the NULL price as its own bucket' "$d2c" '<NULL>'
    assert_absent 'D2c no longer reports a representable amount as off-grammar' "$d2c" '$30'
    assert_contains 'the D2b summary count agrees with the detail query' \
        "$body" '1 mentor row(s) hold an off-grammar price'

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
