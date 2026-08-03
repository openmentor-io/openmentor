#!/usr/bin/env bash
#
# diagnostics.sh — run the 2026-08 audit diagnostics (D1-D4) and write a report.
#
# Read-only: it feeds diagnostics.sql to psql and captures the output. The SQL
# pins the session read-only, so nothing here can write to the database.
#
# Two ways to reach production Postgres (it publishes no host port):
#
#   ON THE VM (default mode) — psql inside the postgres container:
#       ssh <vm>
#       cd /opt/openmentor   # wherever the repo/runbook copy lives
#       ./diagnostics.sh
#
#   FROM A WORKSTATION — point it at any psql-reachable database:
#       ./diagnostics.sh --local -- "service=openmentor-prod"
#       PGHOST=localhost PGPORT=5433 PGUSER=openmentor ./diagnostics.sh --local
#     (infra/db.sh tunnel 5433 opens that tunnel.)
#
# Zero-setup alternative that needs no copy of this script on the VM:
#       infra/db.sh < docs/runbooks/audit-2026-08/diagnostics.sql
#     — same SQL, output to your terminal, no report file.
#
# Passwords: this script never prints, logs or writes a password. It does not
# read one either — supply credentials the way libpq expects (PGPASSWORD,
# ~/.pgpass, PGPASSFILE, a service file) or use the default container mode,
# which connects over the container's local socket and needs none.
#
# The report contains PERSONAL DATA (names, emails, request text). It is
# written with mode 600. Delete it when you are done.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL_FILE="$SCRIPT_DIR/diagnostics.sql"

MODE="docker"
CONTAINER="${OM_PG_CONTAINER:-openmentor-postgres}"
PG_USER="${OM_PG_USER:-openmentor}"
PG_DB="${OM_PG_DB:-openmentor}"
OUT_FILE="${OM_DIAG_OUT:-}"
DRY_RUN=0
PSQL_PASSTHROUGH=()

readonly EXIT_USAGE=2
readonly EXIT_PRECONDITION=3

usage() {
    cat <<'EOF'
Usage: diagnostics.sh [options] [-- PSQL_ARGS...]

Runs the read-only 2026-08 audit diagnostics (D1-D4) and writes a report.

Options:
  --docker              run psql inside the postgres container (default)
  --local               run the local psql binary instead
  --container NAME      container name for --docker (default: openmentor-postgres,
                        or $OM_PG_CONTAINER)
  --user NAME           database user for --docker (default: openmentor,
                        or $OM_PG_USER)
  --db NAME             database name for --docker (default: openmentor,
                        or $OM_PG_DB)
  --out FILE            report path (default: ./openmentor-diagnostics-<UTC>.txt,
                        or $OM_DIAG_OUT)
  -n, --dry-run         show what would run and check preconditions; do not
                        connect to any database
  -h, --help            this text
  -- PSQL_ARGS...       passed verbatim to psql in --local mode (e.g. a DSN or
                        a service name). NEVER echoed back.

Connection details for --local come from the standard libpq environment
(PGHOST, PGPORT, PGUSER, PGDATABASE, PGPASSWORD, PGPASSFILE, PGSSLMODE) or
from PSQL_ARGS. DATABASE_URL is used if set and nothing else is given; note
that a DSN on a command line is visible in the host process list, so prefer
PG* variables or a service file when the DSN embeds a password.

Exit codes: 0 ok (findings are not failures) · 1 psql/query failed
            2 bad usage · 3 precondition missing
EOF
}

die() {
    printf 'diagnostics.sh: %s\n' "$1" >&2
    exit "${2:-$EXIT_USAGE}"
}

need_value() {
    [ "$2" -gt 1 ] || die "$1 needs a value"
}

while [ $# -gt 0 ]; do
    case "$1" in
        --docker) MODE="docker"; shift ;;
        --local)  MODE="local";  shift ;;
        --container) need_value "$1" $#; CONTAINER="$2"; shift 2 ;;
        --user)      need_value "$1" $#; PG_USER="$2";   shift 2 ;;
        --db)        need_value "$1" $#; PG_DB="$2";     shift 2 ;;
        --out)       need_value "$1" $#; OUT_FILE="$2";  shift 2 ;;
        -n|--dry-run) DRY_RUN=1; shift ;;
        -h|--help) usage; exit 0 ;;
        --) shift; PSQL_PASSTHROUGH=("$@"); break ;;
        *) die "unknown argument: $1 (see --help)" ;;
    esac
done

[ -f "$SQL_FILE" ] || die "diagnostics.sql not found next to this script ($SQL_FILE)" "$EXIT_PRECONDITION"

if [ ${#PSQL_PASSTHROUGH[@]} -gt 0 ] && [ "$MODE" = "docker" ]; then
    die "PSQL_ARGS are only used with --local"
fi

if [ -z "$OUT_FILE" ]; then
    OUT_FILE="./openmentor-diagnostics-$(date -u +%Y%m%d-%H%M%S).txt"
fi

# Build the command. psql flags: -X skips ~/.psqlrc so the output is
# reproducible, -v ON_ERROR_STOP=1 aborts on the first error, -f - reads the
# SQL from stdin (which is how the file is fed in, in both modes).
psql_flags=(-X -v ON_ERROR_STOP=1 -f -)
target_label=""
cmd=()

case "$MODE" in
    docker)
        cmd=(docker exec -i "$CONTAINER" psql -U "$PG_USER" -d "$PG_DB" "${psql_flags[@]}")
        target_label="container $CONTAINER, database $PG_DB as $PG_USER"
        ;;
    local)
        cmd=(psql)
        if [ ${#PSQL_PASSTHROUGH[@]} -gt 0 ]; then
            cmd+=("${PSQL_PASSTHROUGH[@]}")
            target_label="local psql (connection from the arguments after --, not shown)"
        elif [ -n "${DATABASE_URL:-}" ]; then
            cmd+=("$DATABASE_URL")
            target_label="local psql (connection from \$DATABASE_URL, not shown)"
        else
            target_label="local psql (connection from the PG* environment, not shown)"
        fi
        cmd+=("${psql_flags[@]}")
        ;;
esac

printf 'OpenMentor audit diagnostics (2026-08)\n'
printf '  sql     : %s\n' "$SQL_FILE"
printf '  target  : %s\n' "$target_label"
printf '  report  : %s\n' "$OUT_FILE"
printf '  mode    : read-only (the SQL pins default_transaction_read_only)\n'
printf '\n'

if [ "$DRY_RUN" -eq 1 ]; then
    printf 'DRY RUN — no database connection is made.\n\n'
    case "$MODE" in
        docker)
            if ! command -v docker >/dev/null 2>&1; then
                printf '  MISSING : docker is not on PATH (run this on the VM, or use --local)\n'
            elif ! docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -qx true; then
                printf '  MISSING : container %s is not running\n' "$CONTAINER"
            else
                printf '  ok      : container %s is running\n' "$CONTAINER"
            fi
            printf '  would run: docker exec -i %s psql -U %s -d %s -X -v ON_ERROR_STOP=1 -f - < diagnostics.sql\n' \
                "$CONTAINER" "$PG_USER" "$PG_DB"
            ;;
        local)
            if command -v psql >/dev/null 2>&1; then
                printf '  ok      : psql found at %s\n' "$(command -v psql)"
            else
                printf '  MISSING : psql is not on PATH\n'
            fi
            printf '  would run: psql <connection args, not shown> -X -v ON_ERROR_STOP=1 -f - < diagnostics.sql\n'
            ;;
    esac
    printf '  would write the report to: %s\n' "$OUT_FILE"
    printf '\nNothing was read or written. Re-run without --dry-run.\n'
    exit 0
fi

# Preconditions for a real run.
case "$MODE" in
    docker)
        command -v docker >/dev/null 2>&1 \
            || die "docker is not on PATH — run this on the VM, or use --local" "$EXIT_PRECONDITION"
        docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -qx true \
            || die "container $CONTAINER is not running (docker compose ps)" "$EXIT_PRECONDITION"
        ;;
    local)
        command -v psql >/dev/null 2>&1 \
            || die "psql is not on PATH" "$EXIT_PRECONDITION"
        ;;
esac

# The report holds personal data: create it 0600 and keep it that way.
umask 077
: > "$OUT_FILE"

{
    printf '# OpenMentor audit diagnostics (2026-08)\n'
    printf '# generated: %s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
    printf '# target   : %s\n' "$target_label"
    printf '# CONTAINS PERSONAL DATA — handle like a database export, then delete.\n'
    printf '\n'
} >> "$OUT_FILE"

printf 'Running diagnostics...\n'

status=0
"${cmd[@]}" < "$SQL_FILE" >> "$OUT_FILE" 2>&1 || status=$?

if [ "$status" -ne 0 ]; then
    printf '\npsql exited %d. Last 20 lines of the report:\n\n' "$status" >&2
    tail -n 20 "$OUT_FILE" >&2
    printf '\nFull output: %s\n' "$OUT_FILE" >&2
    exit 1
fi

printf '\n'
# The SQL emits a '## SUMMARY' marker; reprint from there so the console shows
# the counts without the personal data in the detail sections.
if grep -q '^## SUMMARY' "$OUT_FILE"; then
    awk '/^## SUMMARY/{found=1} found' "$OUT_FILE"
else
    printf 'No SUMMARY block found — read the full report.\n'
fi

cat <<EOF

Full report (detail rows, personal data): $OUT_FILE

Next:
  D1 hits  -> data-repair.md  (read the WARNING before re-finalizing anything)
  D2 hits  -> data-repair.md  (fix the form BEFORE restoring any price)
  D3 hits  -> a link inside a NAME or a javascript:/data: calendar URL is an
              incident, not a backlog item
  D4 count -> outstanding-decisions.md §1

Findings are not failures: this script exits 0 whether or not it found anything.
EOF
