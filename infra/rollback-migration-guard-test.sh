#!/usr/bin/env bash
# ============================================================================
# rollback.sh's migration-boundary guard, and the expand/contract policy (H10)
# ============================================================================
# `migrate` shares BACKEND_IMAGE_TAG with backend and worker, and the migrations
# are baked into that image. A backend tag older than the applied schema version
# therefore makes golang-migrate's versionExists() fail ("no migration found for
# version N"), migrate exits 1, service_completed_successfully is never
# satisfied, and rollback.sh's set -e aborts with the BAD version still live.
# rollback.sh now refuses before it edits .env. That refusal is only worth
# anything if it actually triggers, so cases 2-8 run the shipped block with
# `docker` stubbed — no daemon, no VM, no registry.
#
# Cases:
#   1. the guard block survives rollback.sh's unquoted here-document and the
#      result parses under bash 3.2 (macOS /bin/bash), with no `case` and no
#      backtick — the two things that silently truncate that here-document
#   2. schema version within the target image's range -> allowed
#   3. crossing a CONTRACT boundary -> refuses, names the migration and the
#      runbook section, and writes no .env.backup
#   4. crossing an EXPAND-only boundary -> still refuses (the migrate gate fails
#      either way) but says so, because the remedy is different
#   5. unreadable schema_migrations -> warns, does NOT refuse: postgres being
#      down is exactly when a rollback must not be blocked
#   6. dirty schema_migrations -> refuses (golang-migrate will not run at all)
#   7. an unpullable target tag -> refuses before touching anything
#   8. a frontend-only rollback with applied contract migrations -> warns and
#      proceeds; a backend tag that is not changing -> no check at all
#
# Policy assertions on ../api/migrations (cases 9-11): every migration declares
# its phase, ships a .down.sql, and anything with destructive SQL is marked
# contract. The guard reads those `-- phase:` markers out of the image at
# rollback time, so an unmarked migration degrades it to "unmarked" guesswork.
#
# Usage: ./rollback-migration-guard-test.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

MIGRATIONS=../api/migrations
BEGIN='# --- H10 migration boundary guard'
END='# --- end H10 migration boundary guard'

ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT

FAILURES=0
ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

# --- 1. the block reaches the VM intact ------------------------------------
echo "the guard block survives rollback.sh's unquoted here-document"
awk -v b="$BEGIN" -v e="$END" '
    index($0, b) == 1 { on = 1 }
    on               { print }
    on && index($0, e) == 1 { exit }
' rollback.sh > "$ROOT/guard-raw"

if [ ! -s "$ROOT/guard-raw" ]; then
    echo "  FAIL guard block markers not found in rollback.sh" >&2
    exit 1
fi

if grep -q '`' "$ROOT/guard-raw"; then
    bad "the block contains a backtick, which the unquoted here-document would execute locally"
else
    ok "no backticks"
fi
# Comments are exempt (bash skips them); code is not.
if grep -vE '^[[:space:]]*#' "$ROOT/guard-raw" | grep -qE '(^|[[:space:]])case[[:space:]]'; then
    bad "the block uses 'case': bash 3.2 counts parens to end the enclosing \$( ), so a case pattern's ')' truncates rollback.sh"
else
    ok "no 'case' in code"
fi

# Render it exactly as rollback.sh does, then run bash over the result.
render_guard() {
    /bin/bash -c 'cat <<REMOTE_GUARD
'"$(cat "$ROOT/guard-raw")"'
REMOTE_GUARD'
}
render_guard > "$ROOT/guard-rendered"
if grep -q '\\\$' "$ROOT/guard-rendered"; then
    bad "the rendered block still contains an escaped \$, so an expansion was double-escaped"
else
    ok "every expansion unescapes to exactly one \$"
fi
if /bin/bash -n "$ROOT/guard-rendered" 2>"$ROOT/parse-err"; then
    ok "the rendered block parses under $(/bin/bash --version | head -1 | grep -oE 'version [0-9.]+')"
else
    bad "the rendered block does not parse: $(cat "$ROOT/parse-err")"
fi
if /bin/bash -n rollback.sh; then
    ok "rollback.sh itself still parses"
else
    bad "rollback.sh no longer parses"
fi

# --- the harness: run the shipped guard with docker stubbed ----------------
# A fixture directory of *.up.sql files, one per "NNNNNN:phase" argument.
make_migrations() {
    dir=$1; shift
    mkdir -p "$dir"
    for spec in "$@"; do
        printf -- '-- phase: %s\n-- fixture\n' "${spec#*:}" > "$dir/${spec%%:*}_fixture.up.sql"
    done
}

STUB_BIN="$ROOT/bin"
mkdir -p "$STUB_BIN"
cat > "$STUB_BIN/docker" <<'STUB'
#!/usr/bin/env bash
# Stands in for the VM's docker. Behaviour comes from STUB_* in the environment,
# so each case drives the real guard down a different path.
case "$1" in
  exec)   printf '%s\n' "$STUB_SCHEMA_ROW"; [ -n "$STUB_SCHEMA_ROW" ] || exit 1 ;;
  pull)   exit "${STUB_PULL_RC:-0}" ;;
  create)
      # $2 is the image ref; the tag decides which fixture tree it carries.
      case "$2" in
        *:"$STUB_CURRENT_TAG") echo "cid-current" ;;
        *)                     echo "cid-target" ;;
      esac
      exit "${STUB_CREATE_RC:-0}" ;;
  cp)
      src=$2; dst=$3
      case "$src" in
        cid-current:*) from=$STUB_DIR_CURRENT ;;
        *)             from=$STUB_DIR_TARGET ;;
      esac
      [ -d "$from" ] || exit 1
      mkdir -p "$dst"
      cp "$from"/* "$dst"/ 2>/dev/null || true
      ;;
  rm)     exit 0 ;;
  *)      exit 0 ;;
esac
STUB
chmod +x "$STUB_BIN/docker"

# Runs the shipped guard in a scratch "VM" directory. Prints its output; returns
# the guard's exit status.
run_guard() {
    RUN=$ROOT/run.$RANDOM
    mkdir -p "$RUN"
    {
        printf 'FRONTEND_TARGET_TAG=%s\n' "${G_FE_TARGET:-}"
        printf 'BACKEND_TARGET_TAG=%s\n' "${G_BE_TARGET:-}"
        printf 'CURRENT_FRONTEND_TAG=%s\n' "${G_FE_CURRENT:-old-fe}"
        printf 'CURRENT_BACKEND_TAG=%s\n' "$STUB_CURRENT_TAG"
        cat "$ROOT/guard-rendered"
        # The guard exits 1 on refusal; reaching here means it allowed the rollback.
        printf 'echo GUARD_ALLOWED\n'
    } > "$RUN/guard.sh"
    printf 'POSTGRES_USER=openmentor\nPOSTGRES_DB=openmentor\nECR_REGISTRY=reg.example\n' > "$RUN/.env"
    ( cd "$RUN" && PATH="$STUB_BIN:$PATH" /bin/bash ./guard.sh 2>&1 )
}

# Fixtures: the running image carries 1..9 with 9 as the boundary migration; the
# target carries 1..8. Case 4 re-marks 9 as expand.
make_migrations "$ROOT/cur-contract" 000001:expand 000005:expand 000008:expand 000009:contract
make_migrations "$ROOT/cur-expand"   000001:expand 000005:expand 000008:expand 000009:expand
make_migrations "$ROOT/tgt-8"        000001:expand 000005:expand 000008:expand
make_migrations "$ROOT/tgt-9"        000001:expand 000005:expand 000008:expand 000009:contract

export STUB_CURRENT_TAG=cur1234
export STUB_DIR_CURRENT="$ROOT/cur-contract"
export STUB_DIR_TARGET="$ROOT/tgt-9"
export STUB_SCHEMA_ROW='9|f'
export STUB_PULL_RC=0

# --- 2. inside the target's range -> allowed -------------------------------
echo "a rollback that stays inside the target image's migration range is allowed"
OUT=$(G_BE_TARGET=tgt1234 run_guard) && RC=0 || RC=$?
if [ "$RC" -eq 0 ] && printf '%s' "$OUT" | grep -q GUARD_ALLOWED; then
    ok "allowed, and it says why: $(printf '%s' "$OUT" | grep -o 'Migration boundary OK')"
else
    bad "a same-version rollback was refused (rc=$RC): $OUT"
fi

# --- 3. contract boundary -> refuse ---------------------------------------
echo "crossing a contract migration is refused with an actionable message"
export STUB_DIR_TARGET="$ROOT/tgt-8"
OUT=$(G_BE_TARGET=tgt1234 run_guard) && RC=0 || RC=$?
if [ "$RC" -eq 0 ]; then
    bad "a cross-boundary rollback was ALLOWED — this is the H10 bug: $OUT"
elif printf '%s' "$OUT" | grep -q GUARD_ALLOWED; then
    bad "the guard printed a refusal but did not stop the script"
else
    ok "refused before .env was touched"
    for want in 'REFUSING' '000009_fixture' 'Contract-phase' \
                'Rolling back across a migration boundary' 'no migration found for version'; do
        if printf '%s' "$OUT" | grep -qF "$want"; then
            ok "the message contains '$want'"
        else
            bad "the refusal never mentions '$want', so the operator has nowhere to go: $OUT"
        fi
    done
fi

# --- 4. expand-only boundary -> refuse, but a different remedy -------------
# Still a refusal: versionExists() fails on ANY orphaned version, expand or not.
echo "crossing expand-only migrations is refused too, and named as expand-only"
export STUB_DIR_CURRENT="$ROOT/cur-expand"
OUT=$(G_BE_TARGET=tgt1234 run_guard) && RC=0 || RC=$?
if [ "$RC" -eq 0 ]; then
    bad "an expand-only crossing was allowed, but golang-migrate fails on it just the same"
elif printf '%s' "$OUT" | grep -q 'expand-phase' &&
     ! printf '%s' "$OUT" | grep -q 'Contract-phase migrations in the way'; then
    ok "refused, and told apart from the contract case"
else
    bad "an expand-only crossing was not described as expand-only: $OUT"
fi
export STUB_DIR_CURRENT="$ROOT/cur-contract"

# --- 5. unreadable schema_migrations -> warn, never refuse -----------------
echo "an unreadable schema_migrations warns instead of blocking the rollback"
OUT=$(STUB_SCHEMA_ROW='' G_BE_TARGET=tgt1234 run_guard) && RC=0 || RC=$?
if [ "$RC" -eq 0 ] && printf '%s' "$OUT" | grep -q 'UNVERIFIED'; then
    ok "proceeds with an explicit UNVERIFIED warning"
else
    bad "postgres being unreachable blocked the rollback (rc=$RC) — that is when one is needed most: $OUT"
fi

# --- 6. dirty schema -> refuse --------------------------------------------
echo "a dirty schema_migrations is refused"
OUT=$(STUB_SCHEMA_ROW='9|t' G_BE_TARGET=tgt1234 run_guard) && RC=0 || RC=$?
if [ "$RC" -ne 0 ] && printf '%s' "$OUT" | grep -q 'DIRTY'; then
    ok "refused: golang-migrate will not run against a dirty version at all"
else
    bad "a dirty database was not refused (rc=$RC): $OUT"
fi

# --- 7. unpullable target tag -> refuse -----------------------------------
echo "a target tag that cannot be pulled is refused before anything changes"
export STUB_DIR_TARGET="$ROOT/tgt-9"
OUT=$(STUB_PULL_RC=1 G_BE_TARGET=gone999 run_guard) && RC=0 || RC=$?
if [ "$RC" -ne 0 ] && printf '%s' "$OUT" | grep -q 'cannot pull'; then
    ok "refused with the registry as the named cause"
else
    bad "an unpullable tag was accepted (rc=$RC): $OUT"
fi

# --- 8. frontend-only, and a no-op backend tag ----------------------------
echo "a frontend-only rollback warns about applied contract migrations, and proceeds"
OUT=$(G_FE_TARGET=oldfe99 G_BE_TARGET='' run_guard) && RC=0 || RC=$?
if [ "$RC" -eq 0 ] &&
   printf '%s' "$OUT" | grep -q 'applied contract migrations' &&
   printf '%s' "$OUT" | grep -q '000009_fixture'; then
    ok "warns, names the migration, and does not block"
elif [ "$RC" -ne 0 ]; then
    bad "a frontend-only rollback was refused; its failure mode is wrong content, not a broken converge: $OUT"
else
    bad "a frontend-only rollback said nothing about the applied contract migration: $OUT"
fi

echo "a backend tag that is not changing runs no boundary check"
OUT=$(G_BE_TARGET="$STUB_CURRENT_TAG" G_FE_TARGET='' run_guard) && RC=0 || RC=$?
if [ "$RC" -eq 0 ] && ! printf '%s' "$OUT" | grep -q 'Checking the migration boundary'; then
    ok "no check, no registry round-trip"
else
    bad "an unchanged backend tag still ran the boundary check (rc=$RC): $OUT"
fi

# --- 9-11. the expand/contract policy the guard reads ---------------------
echo "every migration declares a phase and ships a down-migration"
for up in "$MIGRATIONS"/*.up.sql; do
    name=$(basename "$up" .up.sql)
    phase=$( { grep -m1 '^-- phase:' "$up" || true; } | sed 's/^-- phase: *//')
    if [ "$phase" = "expand" ] || [ "$phase" = "contract" ]; then
        ok "$name is phase '$phase'"
    else
        bad "$name has no '-- phase: expand|contract' header (got '${phase:-<none>}'): rollback.sh reads this out of the image to tell an operator whether a boundary is safe to cross"
    fi
    if [ -f "$MIGRATIONS/$name.down.sql" ]; then
        ok "$name has a .down.sql"
    else
        bad "$name has no .down.sql, so the documented manual rollback procedure has nothing to apply"
    fi
done

# A tripwire, not a proof: an up-migration that removes or renames cannot be
# expand, and mismarking one is how the guard would wave through a rollback that
# breaks the older image.
echo "no destructive up-migration is marked expand"
DESTRUCTIVE='DROP[[:space:]]+(TABLE|COLUMN|INDEX)|RENAME[[:space:]]+(TO|COLUMN)|DELETE[[:space:]]+FROM|SET[[:space:]]+NOT[[:space:]]+NULL'
for up in "$MIGRATIONS"/*.up.sql; do
    name=$(basename "$up" .up.sql)
    if ! grep -qiE "$DESTRUCTIVE" "$up"; then continue; fi
    if grep -q '^-- phase: contract' "$up"; then
        ok "$name removes or renames and is marked contract"
    elif grep -q '^-- phase-exempt:' "$up"; then
        ok "$name is marked expand with a written exemption"
    else
        bad "$name contains destructive SQL but is marked expand — an older image would not find what it reads. Mark it contract, or add '-- phase-exempt: <why>'"
    fi
done

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "OK: the migration-boundary guard refuses what it must and the phase policy holds"
else
    echo "$FAILURES assertion(s) failed"
    exit 1
fi
