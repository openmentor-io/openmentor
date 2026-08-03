#!/usr/bin/env bash
# ============================================================================
# Tests the logic deploy-remote.sh and rollback.sh share (SECURITY P10 + P8)
# ============================================================================
# Both scripts run ON the VM against whatever docker-compose.yml is already
# there — and a default `./deploy.sh` (frontend backend) never syncs infra/.
# They also carry two blocks verbatim, which is exactly how one copy drifts.
#
# P10 — the .env.runtime transition. Compose defaults `env_file.required` to
# true, so deleting .env.runtime while the VM's compose file still declares it
# aborts `pull`/`up` halfway through a live deploy.
#
#   1. both scripts carry the byte-identical transition block
#   2. the block survives rollback.sh's unquoted heredoc unexpanded
#   3. pre-P10 compose file on the VM -> .env.runtime regenerated (600, no
#      image-tag lines) and the upgrade order printed
#   4. current compose file -> .env.runtime deleted
#
# P8 — the backup sidecar health gate. Docker keeps an UNHEALTHY container in
# state `running`, so the old `.State.Status`-only test reported a sidecar whose
# nightly dumps had silently stopped as passing. Cases 6-7 run the real gate
# with `docker` stubbed, so no Docker daemon and no container are needed:
#
#   5. both scripts carry the same gate block, rollback.sh's escaped for its
#      heredoc, and both read .State.Health.Status
#   6. running + unhealthy is reported as a FAILURE (return 2); running +
#      healthy, running + starting (start_period) and an absent .State.Health
#      are not; not-running/restarting/missing is a failure (return 1)
#   7. the call sites route 2 away from the auto-rollback and into a non-zero
#      exit, and 1 into the pre-existing rollback path
#   8. rollback.sh's here-document still parses under bash 3.2 (macOS
#      /bin/bash): it ends the enclosing $( ) at the first unbalanced ')', so a
#      `case` pattern in the remote script breaks the whole file
#
# Usage: ./deploy-transition-test.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

BEGIN='# --- P10 .env.runtime transition'
END='# --- end P10 .env.runtime transition'
GATE_BEGIN='# --- P8 backup sidecar health gate'
GATE_END='# --- end P8 backup sidecar health gate'

# The block between markers $2/$3 as literally written in $1, markers included
extract_block() {
    awk -v b="$2" -v e="$3" '
        index($0, b) == 1 { on = 1 }
        on               { print }
        on && index($0, e) == 1 { exit }
    ' "$1"
}

extract() { extract_block "$1" "$BEGIN" "$END"; }

ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT

FAILURES=0
ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

# --- 1. one block, two copies ----------------------------------------------
echo "the transition block is identical in deploy-remote.sh and rollback.sh"
extract deploy-remote.sh > "$ROOT/from-deploy"
extract rollback.sh > "$ROOT/from-rollback"
if [ ! -s "$ROOT/from-deploy" ] || [ ! -s "$ROOT/from-rollback" ]; then
    bad "block markers not found in both scripts"
elif cmp -s "$ROOT/from-deploy" "$ROOT/from-rollback"; then
    ok "blocks match"
else
    bad "blocks diverged:$(printf '\n    %s' "$(diff "$ROOT/from-deploy" "$ROOT/from-rollback")")"
fi
BLOCK=$(cat "$ROOT/from-deploy")

# --- 2. heredoc-safe -------------------------------------------------------
# rollback.sh embeds the block in an UNQUOTED here-document, so a `$VAR` or a
# backtick in it would expand on the operator's machine and silently ship a
# mangled script to the VM.
echo "the block passes through an unquoted here-document unchanged"
printf '%s\n' "$BLOCK" > "$ROOT/block"
{
    echo 'cat <<REMOTE_SCRIPT'
    cat "$ROOT/block"
    echo 'REMOTE_SCRIPT'
} > "$ROOT/heredoc.sh"
# Run from the scratch dir: an accidental command substitution must not be
# able to fire a repo script.
(cd "$ROOT" && bash heredoc.sh) > "$ROOT/expanded"
if cmp -s "$ROOT/block" "$ROOT/expanded"; then
    ok "no shell expansion inside the heredoc"
else
    bad "the heredoc expanded part of the block:$(printf '\n    %s' "$(diff "$ROOT/block" "$ROOT/expanded")")"
fi

# run_block <dir> -> stdout+stderr in $OUT
run_block() {
    set +e
    OUT=$(cd "$1" && bash -c "$BLOCK" 2>&1)
    local status=$?
    set -e
    [ "$status" -eq 0 ] || bad "block exited $status in $1: $OUT"
}

# --- 3. VM still on the pre-P10 compose file -------------------------------
echo "a VM whose compose file still declares env_file keeps a regenerated .env.runtime"
OLD="$ROOT/old-vm"
mkdir -p "$OLD"
# The shape main's docker-compose.yml has (six services, one env_file each)
cat > "$OLD/docker-compose.yml" <<'YAML'
services:
  frontend:
    image: example:latest
    env_file:
      # Runtime env WITHOUT the image-tag lines
      - .env.runtime
YAML
cat > "$OLD/.env" <<'ENVFILE'
POSTGRES_PASSWORD=not-a-real-secret
FRONTEND_IMAGE_TAG=abc1234
BACKEND_IMAGE_TAG=abc1234
ENVFILE
run_block "$OLD"
if [ -f "$OLD/.env.runtime" ]; then
    ok ".env.runtime kept"
else
    bad ".env.runtime deleted while the VM's compose file still requires it — this deploy would abort at 'compose pull'"
fi
if grep -qE '^(FRONTEND|BACKEND)_IMAGE_TAG=' "$OLD/.env.runtime" 2>/dev/null; then
    bad ".env.runtime contains image-tag lines, so every service would be recreated on a tag-only deploy"
else
    ok "image-tag lines stripped"
fi
# shellcheck disable=SC2012 # fixed filename; ls is the portable way to read the mode
MODE=$(ls -l "$OLD/.env.runtime" 2>/dev/null | cut -c1-10)
if [ "$MODE" = "-rw-------" ]; then
    ok "mode 600"
else
    bad "expected mode 600, got '${MODE:-<no file>}'"
fi
case "$OUT" in
    *"./deploy.sh infra"*) ok "prints the upgrade order" ;;
    *) bad "the operator is not told to run './deploy.sh infra'. Output:$(printf '\n    %s' "$OUT")" ;;
esac

# --- 4. VM on the current compose file -------------------------------------
echo "a VM carrying the current compose file loses the shared secret file"
NEW="$ROOT/new-vm"
mkdir -p "$NEW"
cp docker-compose.yml "$NEW/docker-compose.yml"
cp "$OLD/.env" "$NEW/.env"
echo "stale=secret" > "$NEW/.env.runtime"
run_block "$NEW"
if [ -e "$NEW/.env.runtime" ]; then
    bad ".env.runtime survived against the current docker-compose.yml (does it still reference the file?)"
else
    ok ".env.runtime removed"
fi

# --- 5. the P8 gate block is one block in two files ------------------------
# rollback.sh's copy has to be escaped for its unquoted heredoc, so instead of
# comparing the two literally, push its copy THROUGH a heredoc: that proves both
# that the escaping is right and that the logic is the same.
echo "the backup health gate is identical in both scripts once the heredoc runs"
extract_block deploy-remote.sh "$GATE_BEGIN" "$GATE_END" > "$ROOT/gate-deploy"
extract_block rollback.sh "$GATE_BEGIN" "$GATE_END" > "$ROOT/gate-rollback-raw"
if [ ! -s "$ROOT/gate-deploy" ] || [ ! -s "$ROOT/gate-rollback-raw" ]; then
    bad "P8 gate markers not found in both scripts"
else
    {
        echo 'cat <<REMOTE_SCRIPT'
        cat "$ROOT/gate-rollback-raw"
        echo 'REMOTE_SCRIPT'
    } > "$ROOT/gate-heredoc.sh"
    # From the scratch dir: an unescaped $(...) must not be able to fire a repo
    # script (the block contains `docker inspect` command substitutions).
    (cd "$ROOT" && bash gate-heredoc.sh) > "$ROOT/gate-expanded"
    if cmp -s "$ROOT/gate-deploy" "$ROOT/gate-expanded"; then
        ok "rollback.sh's escaped copy expands to deploy-remote.sh's block"
    else
        bad "the two gate blocks differ after the heredoc:$(printf '\n    %s' "$(diff "$ROOT/gate-deploy" "$ROOT/gate-expanded")")"
    fi
fi
# Per file, not just "the two agree": reverting BOTH copies to the old
# .State.Status-only test would keep them identical and slip past case 5.
# Matches the template itself, so a mention in a comment cannot satisfy it.
for f in deploy-remote.sh rollback.sh; do
    if grep -qF '{{.State.Health.Status}}' "$f"; then
        ok "$f inspects {{.State.Health.Status}}"
    else
        bad "$f does not inspect {{.State.Health.Status}} — docker keeps an unhealthy container in state 'running', so a stale backup would pass this gate again"
    fi
done

# --- 6. the gate's verdict per docker state --------------------------------
echo "the gate fails on running+unhealthy and passes on starting / no-healthcheck"
STUBS="$ROOT/stubs"
mkdir -p "$STUBS"
# `docker inspect -f <fmt> <name>` only; answers from the FAKE_* env so the real
# gate logic runs with no daemon and no container.
cat > "$STUBS/docker" <<'STUB'
#!/bin/sh
fmt=""
prev=""
for arg in "$@"; do
    [ "$prev" = "-f" ] && fmt="$arg"
    prev="$arg"
done
# A missing container is a non-zero exit with nothing on stdout, for any format
[ "${FAKE_EXISTS:-1}" = "0" ] && exit 1
case "$fmt" in
    # {{if .State.Health}} yields an empty string when the container has no
    # healthcheck; FAKE_HEALTH="" reproduces that
    *State.Health*) printf '%s\n' "$FAKE_HEALTH" ;;
    *State.Status*) printf '%s\n' "$FAKE_STATUS" ;;
    *) exit 1 ;;
esac
STUB
chmod +x "$STUBS/docker"

# The gate plus the invocation both call sites use: `set -e` on, and the return
# taken through `|| RC=$?` so a non-zero return is a verdict, not an abort.
cp "$ROOT/gate-deploy" "$ROOT/gate.sh"
cat >> "$ROOT/gate.sh" <<'DRIVER'
set -e
BACKUP_RC=0
check_backup_sidecar || BACKUP_RC=$?
exit $BACKUP_RC
DRIVER

# run_gate <status> <health> [exists] -> output in $OUT, return code in $RC
run_gate() {
    set +e
    OUT=$(FAKE_STATUS="$1" FAKE_HEALTH="$2" FAKE_EXISTS="${3:-1}" \
        PATH="$STUBS:$PATH" bash "$ROOT/gate.sh" 2>&1)
    RC=$?
    set -e
}

# assert_gate <label> <status> <health> <exists> <want-rc> <want-substring>
assert_gate() {
    local label="$1" want_rc="$5" want_text="$6"
    run_gate "$2" "$3" "$4"
    if [ "$RC" != "$want_rc" ]; then
        bad "$label: expected return $want_rc, got $RC. Output:$(printf '\n    %s' "$OUT")"
        return
    fi
    case "$OUT" in
        *"$want_text"*) ok "$label -> return $RC, says '$want_text'" ;;
        *) bad "$label: returned $RC but the output does not mention '$want_text':$(printf '\n    %s' "$OUT")" ;;
    esac
}

# THE regression this exists for: unhealthy while still `running`.
assert_gate "running + unhealthy"        running unhealthy 1 2 "UNHEALTHY"
assert_gate "running + healthy"          running healthy   1 0 "passed"
assert_gate "running + starting"         running starting  1 0 "start_period"
assert_gate "running + no .State.Health" running ""        1 0 "UNVERIFIED"
assert_gate "restarting"                 restarting ""     1 1 "restarting"
assert_gate "exited"                     exited ""         1 1 "exited"
assert_gate "no such container"          "" ""             0 1 "<absent>"

# --- 7. the call sites route the two failure codes differently -------------
# Return 2 must NOT enter the auto-rollback: reverting working application
# images cannot make a pg_dump run, and a deploy that aborts halfway is worse
# than the stale dump. It must still leave the run non-zero.
echo "return 2 leaves the run non-zero without triggering the rollback"
# The body of the BACKUP_RC branch for return code $2 in $1, comments stripped
arm() {
    # Terminators spelled out rather than with \> : BSD awk (macOS) has no
    # word-boundary escape, and a never-matching exit rule silently prints to EOF.
    awk -v code="$2" '
        $0 ~ ("BACKUP_RC.* -eq " code) { grab = 1; next }
        grab && ($0 ~ /^[[:space:]]*elif[[:space:]]/ ||
                 $0 ~ /^[[:space:]]*else[[:space:]]*$/ ||
                 $0 ~ /^[[:space:]]*fi[[:space:]]*$/) { exit }
        grab && $0 !~ /^[[:space:]]*#/ { print }
    ' "$1"
}
# The body of the guard that turns BACKUP_UNHEALTHY into an exit status
unhealthy_guard() {
    awk '
        /BACKUP_UNHEALTHY.* -eq 1/ { grab = 1 }
        grab { print }
        grab && /^[[:space:]]*fi[[:space:]]*$/ { exit }
    ' "$1"
}
for f in deploy-remote.sh rollback.sh; do
    if grep -q 'check_backup_sidecar || BACKUP_RC=' "$f"; then
        ok "$f calls the gate through '|| BACKUP_RC='"
    else
        bad "$f does not invoke check_backup_sidecar via '|| BACKUP_RC=' — under 'set -e' a non-zero return would abort the script instead of being a verdict"
    fi
    ARM2=$(arm "$f" 2)
    if printf '%s\n' "$ARM2" | grep -q 'BACKUP_UNHEALTHY=1'; then
        ok "$f: return 2 sets BACKUP_UNHEALTHY"
    else
        bad "$f: the return-2 branch does not set BACKUP_UNHEALTHY, so a stale backup is invisible again:$(printf '\n    %s' "$ARM2")"
    fi
    if printf '%s\n' "$ARM2" | grep -qE 'HEALTH_CHECK_FAILED|HEALTH_OK'; then
        bad "$f: the return-2 branch touches the rollback flag — a stale dump would revert working images:$(printf '\n    %s' "$ARM2")"
    else
        ok "$f: return 2 stays out of the rollback path"
    fi
    if unhealthy_guard "$f" | grep -q 'exit 2'; then
        ok "$f: BACKUP_UNHEALTHY ends the run with exit 2"
    else
        bad "$f: no 'BACKUP_UNHEALTHY -eq 1' guard exits 2, so the gate still reports a stale backup as green"
    fi
    ARM1=$(arm "$f" 1)
    if printf '%s\n' "$ARM1" | grep -qE 'HEALTH_CHECK_FAILED=1|HEALTH_OK=0'; then
        ok "$f: return 1 keeps the pre-existing rollback behaviour"
    else
        bad "$f: the return-1 branch no longer fails the deploy on a sidecar that is not running:$(printf '\n    %s' "$ARM1")"
    fi
done
# deploy.sh and the CI workflow are the only consumers of that exit code.
if grep -q 'DEPLOY_EXIT_CODE -eq 2' deploy.sh; then
    ok "deploy.sh distinguishes exit 2 from a failed deploy"
else
    bad "deploy.sh does not handle deploy-remote.sh's exit 2, so a stale backup would be reported as 'Deployment failed'"
fi

# --- 8. rollback.sh's here-document parses under bash 3.2 -------------------
# bash 3.2 is still /bin/bash on macOS, which is where an operator runs
# rollback.sh, and it finds the end of the $( ) wrapping the here-document by
# COUNTING PARENS in the body: one unbalanced ')' — a `case` pattern, above all —
# closes the substitution early and the whole file stops parsing. `bash -n` only
# catches it under 3.2, and CI runs bash 5, so count the parens directly instead.
# Comments are exempt: bash does skip them (verified against 3.2.57).
echo "the remote here-document has balanced parens in code, so bash 3.2 parses it"
HEREDOC_BODY=$(awk '/^ROLLBACK_SCRIPT=\$\(cat <<REMOTE_SCRIPT$/ { on = 1; next }
                    on && /^REMOTE_SCRIPT$/ { exit }
                    on && $0 !~ /^[[:space:]]*#/' rollback.sh)
if [ -z "$HEREDOC_BODY" ]; then
    bad "could not find rollback.sh's REMOTE_SCRIPT here-document — did its shape change?"
else
    OPENS=$(printf '%s' "$HEREDOC_BODY" | tr -cd '(' | wc -c | tr -d ' ')
    CLOSES=$(printf '%s' "$HEREDOC_BODY" | tr -cd ')' | wc -c | tr -d ' ')
    if [ "$OPENS" = "$CLOSES" ]; then
        ok "parens balanced in the here-document body ($OPENS open, $CLOSES close)"
    else
        bad "unbalanced parens in rollback.sh's here-document ($OPENS open, $CLOSES close) — bash 3.2 will end the \$( ) at the stray ')' and refuse to parse the file. A 'case' pattern is the usual cause; use if/elif."
    fi
fi
# And the belt to that braces: the file must parse under whatever bash is here.
for f in rollback.sh deploy-remote.sh deploy.sh; do
    if bash -n "$f" 2>/dev/null; then
        ok "$f parses ($(bash --version | head -1 | grep -oE 'version [0-9.]+'))"
    else
        bad "$f does not parse: $(bash -n "$f" 2>&1 | head -2)"
    fi
done

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "OK: the shared deploy-script blocks (P10 transition, P8 backup gate) all passed"
else
    echo "$FAILURES assertion(s) failed"
    exit 1
fi
