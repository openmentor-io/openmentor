#!/usr/bin/env bash
# ============================================================================
# Tests for backup.sh's failure-visibility logic (SECURITY P8)
# ============================================================================
# Runs the real backup.sh against temp directories with pg_dump stubbed out, so
# no Postgres, no S3 and no Docker are needed. Covers the parts whose whole
# purpose is to be correct when nobody is watching:
#
#   1. a fresh volume opens a grace window WITHOUT claiming a backup succeeded
#   2. the grace window is measured from the first start and never reset
#   3. an expired grace window with no successful dump is unhealthy
#   4. a real success is reported and published
#   5. an unwritable metrics dir does not turn a successful dump into a failure
#
# Usage: ./backup-test.sh   (SH=<shell> to test another /bin/sh implementation)
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

SH="${SH:-sh}"
SUBJECT="$PWD/backup.sh"
ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT

STUBS="$ROOT/stubs"
mkdir -p "$STUBS"

# pg_dump stub: write something at the path after -f, like the real thing
cat > "$STUBS/pg_dump" <<'STUB'
#!/bin/sh
prev=
for arg in "$@"; do
    [ "$prev" = "-f" ] && { echo "fake dump" > "$arg"; exit 0; }
    prev="$arg"
done
exit 1
STUB
# sleep stub: fails, so `daemon` unwinds under `set -e` right after the setup
# we want to inspect instead of blocking the test for a day
printf '#!/bin/sh\nexit 1\n' > "$STUBS/sleep"
chmod +x "$STUBS/pg_dump" "$STUBS/sleep"

FAILURES=0

ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

case_start() { printf '%s\n' "$1"; }

# run <mode> <backup-dir> <metrics-dir> -> stdout+stderr in $OUT, status in $STATUS
run() {
    local mode="$1" backup_dir="$2" metrics_dir="$3"
    set +e
    OUT=$(PATH="$STUBS:$PATH" \
        POSTGRES_PASSWORD=stub \
        BACKUP_DIR="$backup_dir" \
        BACKUP_METRICS_DIR="$metrics_dir" \
        BACKUP_TIME=03:30 \
        "$SH" "$SUBJECT" "$mode" 2>&1)
    STATUS=$?
    set -e
}

assert_status() {
    if [ "$STATUS" = "$1" ]; then
        ok "exit $1"
    else
        bad "expected exit $1, got $STATUS. Output:$(printf '\n    %s' "$OUT")"
    fi
}

assert_contains() {
    case "$OUT" in
        *"$1"*) ok "output mentions '$1'" ;;
        *) bad "output should mention '$1'. Output:$(printf '\n    %s' "$OUT")" ;;
    esac
}

assert_missing_file() {
    if [ ! -e "$1" ]; then
        ok "$(basename "$1") not created"
    else
        bad "$(basename "$1") should not exist, contains: $(cat "$1")"
    fi
}

assert_file() {
    if [ -f "$1" ]; then
        ok "$(basename "$1") written"
    else
        bad "$(basename "$1") missing"
    fi
}

assert_gauge() {
    local file="$1" metric="$2" want="$3" got
    got=$(awk -v m="$metric" '$1 == m { print $2 }' "$file")
    if [ "$got" = "$want" ]; then
        ok "$metric = $want"
    else
        bad "$metric: expected $want, got '${got:-<absent>}'"
    fi
}

assert_gauge_positive() {
    local file="$1" metric="$2" got
    got=$(awk -v m="$metric" '$1 == m { print $2 }' "$file")
    if [ -n "$got" ] && [ "$got" -gt 0 ] 2>/dev/null; then
        ok "$metric > 0"
    else
        bad "$metric: expected a positive epoch, got '${got:-<absent>}'"
    fi
}

# Sanity: the sleep stub must actually shadow /bin/sleep, or `daemon` blocks
if PATH="$STUBS:$PATH" "$SH" -c 'sleep 99' 2>/dev/null; then
    echo "error: $SH does not resolve 'sleep' via PATH; re-run with SH=<other shell>" >&2
    exit 1
fi

# --- 1. fresh volume: grace window, but no fake success --------------------
case_start "fresh volume: daemon start opens a grace window without a fake success"
B="$ROOT/1/backups"; M="$ROOT/1/metrics"; mkdir -p "$B" "$M"
run daemon "$B" "$M"
assert_contains "first start against"
assert_missing_file "$B/.last_success"
assert_file "$B/.first_start"
assert_gauge "$M/openmentor_db_backup.prom" openmentor_db_backup_last_success_timestamp_seconds 0
assert_gauge_positive "$M/openmentor_db_backup.prom" openmentor_db_backup_first_start_timestamp_seconds
run healthcheck "$B" "$M"
assert_status 0
assert_contains "grace window"

# --- 2. the window is measured from the first start, not from each start ----
case_start "restart does not reset the grace window"
FIRST=$(cat "$B/.first_start" 2>/dev/null || echo "<absent>")
run daemon "$B" "$M"
if [ "$(cat "$B/.first_start" 2>/dev/null || true)" = "$FIRST" ]; then
    ok ".first_start unchanged"
else
    bad ".first_start was rewritten: $FIRST -> $(cat "$B/.first_start" 2>/dev/null || true)"
fi
assert_missing_file "$B/.last_success"

# --- 3. expired grace window with nothing successful is unhealthy -----------
case_start "grace window expired with no successful dump: unhealthy"
echo $(( $(date -u +%s) - 27 * 3600 )) > "$B/.first_start"
run healthcheck "$B" "$M"
assert_status 1
assert_contains "no backup has ever succeeded"

# --- 4. a real dump is reported and published ------------------------------
case_start "successful dump marks, logs and publishes"
B="$ROOT/4/backups"; M="$ROOT/4/metrics"; mkdir -p "$B" "$M"
run once "$B" "$M"
assert_status 0
assert_contains "SUCCESS db="
assert_gauge_positive "$M/openmentor_db_backup.prom" openmentor_db_backup_last_success_timestamp_seconds
run healthcheck "$B" "$M"
assert_status 0
assert_contains "last successful backup"

# --- 5. metrics are best-effort: a dump that worked reports success ---------
case_start "unwritable metrics dir does not fail a successful dump"
B="$ROOT/5/backups"; M="$ROOT/5/metrics"
# A directory where the temp file goes: the write fails for any uid, unlike a
# chmod that root ignores.
mkdir -p "$B" "$M/openmentor_db_backup.prom.tmp"
run once "$B" "$M"
assert_status 0
assert_contains "SUCCESS db="
assert_contains "WARNING: cannot write"
assert_file "$B/.last_success"

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "OK: all backup.sh cases passed"
else
    echo "$FAILURES assertion(s) failed"
    exit 1
fi
