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
bare_aws_lines() {
    awk -v q="'" '
        index($0, "sh -c " q)             { inblock = 1; next }
        inblock && index($0, q)           { inblock = 0; next }
        !inblock && /(^|[[:space:]])aws / { n++ }
        END { print n + 0 }' "$1"
}

# Every needle below is a literal fragment of markdown, so SC2016 (backticks and
# $VAR inside single quotes) is the point rather than a mistake.
# shellcheck disable=SC2016
check_plan() {
local plan
section 'remediation plan — corrected operator instructions'

plan="$(cat "$PLAN_MD")"

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
    "description ~* '<\\s*(a|img|div|table|script|style)\\y'" \
    "name ~* '<\\s*[a-z]'" \
    "preferred_contact ~* '<\\s*[a-z]'" \
    "price ~* '<\\s*[a-z]'" \
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
  ('22222222-2222-2222-2222-222222222222','imported-one','Imported One','inactive','imported@example.invalid',
   NULL, NULL, 'getmentor:4242', '$30 / hour', 'lots', NULL),
  -- mentor-supplied price reaches {{request_price}} in new-request /
  -- new-request-calendly, unescaped
  ('44444444-4444-4444-4444-444444444444','price-injected','Price Injected','active','priceinj@example.invalid',
   11, now(), NULL, '<a href="https://evil.example/pay">$50</a>', '10+', NULL),
  ('55555555-5555-5555-5555-555555555555','clean','Clean Mentor','active','clean@example.invalid',
   12, now(), NULL, '$100', '10+', 'https://cal.example/clean'),
  -- calendar_url reaches href="{{calendly_url}}" in new-request-calendly. This
  -- value keeps the https:// scheme and still closes the attribute, so a
  -- scheme-only D3 predicate reports it as clean.
  ('66666666-6666-6666-6666-666666666666','calendar-injected','Calendar Injected','active','calinj@example.invalid',
   13, now(), NULL, '$150', '10+', 'https://evil.example/x"><img src=x onerror=alert(1)>'),
  -- a real Calendly link with query, fragment and sub-delims must NOT match
  ('77777777-7777-7777-7777-777777777777','calendar-ok','Calendar Ok','active','calok@example.invalid',
   14, now(), NULL, '$200', '10+', 'https://calendly.com/m/30min?month=2026-08&back=1#pick');

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
    assert_contains 'D3 finds the price injection'              "$d3" 'mentors.price'
    assert_contains 'D3 still finds the description injection'  "$d3" 'client_requests.description'
    # The https:// payload row. Its id, not just the field label, so this cannot
    # pass on a non-https row instead.
    assert_contains 'D3 finds markup inside an https calendar_url' \
        "$d3" '66666666-6666-6666-6666-666666666666'
    assert_contains 'and labels it as calendar_url'  "$d3" 'mentors.calendar_url'
    assert_absent   'D3 leaves a real Calendly link with query + fragment alone' \
        "$d3" '77777777-7777-7777-7777-777777777777'
    assert_eq       'D3 detail row count' \
        "$(printf '%s\n' "$d3" | grep -cE '\| (client_requests|mentors|reviews)\.')" 4
    assert_contains 'the D3 summary count agrees with the detail rows' "$body" '4 markup / non-https hits'

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
