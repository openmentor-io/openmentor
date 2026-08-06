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
# H9 — serialized deploys and a rollback target that was actually verified. All
# three writers (this VM's .env is edited by deploy-remote.sh's callers and by
# rollback.sh) used a single `cp .env .env.backup` slot, so two overlapping
# deploys destroyed each other's rollback target:
#
#   9. the lock/snapshot block is one block in both scripts, heredoc-safe
#  10. snapshot_env / promote_env_lastgood / rollback_env_source behave (mode
#      600, pruning, and the lastgood > newest snapshot > legacy preference)
#  11. two concurrent holders of the lock serialize (skipped where flock does
#      not exist, i.e. macOS; CI is Linux and always runs it)
#  12. no writer still uses the single .env.backup slot, every writer takes the
#      lock, and the deploy workflow has the concurrency group
#  13. the converge asserts, under its own lock and before pulling, that .env
#      still carries the tags its caller set — the lock is dropped between the
#      caller's .env write and the converge, and a writer that slips into that
#      window used to get ITS tags deployed under this run's banner
#
# Usage: ./deploy-transition-test.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

BEGIN='# --- P10 .env.runtime transition'
END='# --- end P10 .env.runtime transition'
GATE_BEGIN='# --- P8 backup sidecar health gate'
GATE_END='# --- end P8 backup sidecar health gate'
LOCK_BEGIN='# --- H9 deploy serialization'
LOCK_END='# --- end H9 deploy serialization'
TAG_BEGIN='# --- H9 tag assertion'
TAG_END='# --- end H9 tag assertion'

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

# --- 9. the H9 lock block is one block in two files ------------------------
# Same technique as case 5: push rollback.sh's escaped copy through a heredoc and
# require the result to equal deploy-remote.sh's copy byte for byte.
echo "the deploy lock block is identical in both scripts once the heredoc runs"
extract_block deploy-remote.sh "$LOCK_BEGIN" "$LOCK_END" > "$ROOT/lock-deploy"
extract_block rollback.sh "$LOCK_BEGIN" "$LOCK_END" > "$ROOT/lock-rollback-raw"
if [ ! -s "$ROOT/lock-deploy" ] || [ ! -s "$ROOT/lock-rollback-raw" ]; then
    bad "H9 lock markers not found in both scripts"
else
    {
        echo 'cat <<REMOTE_SCRIPT'
        cat "$ROOT/lock-rollback-raw"
        echo 'REMOTE_SCRIPT'
    } > "$ROOT/lock-heredoc.sh"
    (cd "$ROOT" && bash lock-heredoc.sh) > "$ROOT/lock-expanded"
    if cmp -s "$ROOT/lock-deploy" "$ROOT/lock-expanded"; then
        ok "rollback.sh's escaped copy expands to deploy-remote.sh's block"
    else
        bad "the two lock blocks differ after the heredoc:$(printf '\n    %s' "$(diff "$ROOT/lock-deploy" "$ROOT/lock-expanded")")"
    fi
fi

# --- 10. what the .env helpers actually do ---------------------------------
echo "the .env helpers snapshot, promote and pick a restore source correctly"
LOCKFNS="$ROOT/lockfns.sh"
cp "$ROOT/lock-deploy" "$LOCKFNS"
ENVDIR="$ROOT/envdir"
mkdir -p "$ENVDIR"
echo "POSTGRES_PASSWORD=not-a-real-secret" > "$ENVDIR/.env"

# snapshot_env: one timestamped copy at mode 600, and never the shared slot
# shellcheck source=/dev/null # the block under test, extracted above
(cd "$ENVDIR" && . "$LOCKFNS" && snapshot_env) > "$ROOT/snap.out" 2>&1
SNAPS=$(find "$ENVDIR" -maxdepth 1 -name '.env.backup.*' | wc -l | tr -d ' ')
if [ "$SNAPS" = "1" ]; then
    ok "snapshot_env wrote one .env.backup.<epoch>"
else
    bad "expected 1 timestamped snapshot, found $SNAPS"
fi
if [ -e "$ENVDIR/.env.backup" ]; then
    bad "snapshot_env still writes the single .env.backup slot that two deploys overwrite for each other"
else
    ok "the single .env.backup slot is not written"
fi
# shellcheck disable=SC2012 # fixed prefix in a scratch dir; ls reads the mode
SNAPMODE=$(ls -l "$ENVDIR"/.env.backup.* | cut -c1-10)
if [ "$SNAPMODE" = "-rw-------" ]; then
    ok "snapshot mode 600"
else
    bad "snapshot mode is '$SNAPMODE', expected -rw------- (it holds every production secret)"
fi

# Pruning: 8 snapshots in, 5 kept
for i in 1 2 3 4 5 6 7; do touch "$ENVDIR/.env.backup.$((1700000000 + i))"; done
# shellcheck source=/dev/null
(cd "$ENVDIR" && . "$LOCKFNS" && snapshot_env) >/dev/null 2>&1
KEPT=$(find "$ENVDIR" -maxdepth 1 -name '.env.backup.*' | wc -l | tr -d ' ')
if [ "$KEPT" = "5" ]; then
    ok "snapshots pruned to the 5 newest"
else
    bad "expected 5 snapshots after pruning, found $KEPT — copies of every production secret accumulate"
fi

# rollback_env_source: preference order, and nothing when there is no candidate
PICKDIR="$ROOT/pickdir"
mkdir -p "$PICKDIR"
echo x > "$PICKDIR/.env"
# shellcheck source=/dev/null
PICKED=$(cd "$PICKDIR" && . "$LOCKFNS" && rollback_env_source)
if [ -z "$PICKED" ]; then
    ok "no candidate -> prints nothing (the caller refuses to roll back)"
else
    bad "picked '$PICKED' with no backup present"
fi
echo legacy > "$PICKDIR/.env.backup"
# shellcheck source=/dev/null
PICKED=$(cd "$PICKDIR" && . "$LOCKFNS" && rollback_env_source)
if [ "$PICKED" = ".env.backup" ]; then
    ok "falls back to the pre-H9 slot"
else
    bad "expected .env.backup, got '$PICKED'"
fi
touch "$PICKDIR/.env.backup.1700000001"
sleep 1
touch "$PICKDIR/.env.backup.1700000002"
# shellcheck source=/dev/null
PICKED=$(cd "$PICKDIR" && . "$LOCKFNS" && rollback_env_source)
if [ "$PICKED" = ".env.backup.1700000002" ]; then
    ok "prefers the newest snapshot over the legacy slot"
else
    bad "expected the newest snapshot, got '$PICKED'"
fi
echo verified > "$PICKDIR/.env.lastgood"
# shellcheck source=/dev/null
PICKED=$(cd "$PICKDIR" && . "$LOCKFNS" && rollback_env_source)
if [ "$PICKED" = ".env.lastgood" ]; then
    ok "prefers .env.lastgood — the only candidate whose health checks passed"
else
    bad "expected .env.lastgood, got '$PICKED': a rollback would target an UNVERIFIED .env"
fi

# promote_env_lastgood
# shellcheck source=/dev/null
(cd "$PICKDIR" && . "$LOCKFNS" && promote_env_lastgood) >/dev/null 2>&1
if [ "$(cat "$PICKDIR/.env.lastgood")" = "x" ]; then
    ok "promote_env_lastgood copies the running .env"
else
    bad "promote_env_lastgood did not copy .env"
fi

# --- 11. the lock actually excludes a second writer ------------------------
echo "two writers cannot hold the deploy lock at once"
if command -v flock >/dev/null 2>&1; then
    LOCKDIR="$ROOT/lockdir"
    mkdir -p "$LOCKDIR"
    # Holder: takes the lock and keeps it for 5s
    # shellcheck source=/dev/null
    (cd "$LOCKDIR" && . "$LOCKFNS" && acquire_deploy_lock >/dev/null && sleep 5) &
    HOLDER=$!
    sleep 1
    set +e
    SECOND=$( (cd "$LOCKDIR" && DEPLOY_LOCK_WAIT=1 bash -c ". '$LOCKFNS'; acquire_deploy_lock") 2>&1 )
    SECOND_RC=$?
    set -e
    wait "$HOLDER" || true
    if [ "$SECOND_RC" -ne 0 ] && printf '%s' "$SECOND" | grep -q 'Timed out'; then
        ok "the second writer waits, times out and exits $SECOND_RC without touching anything"
    else
        bad "a second writer got the lock (rc=$SECOND_RC): deploys are NOT serialized. Output:$(printf '\n    %s' "$SECOND")"
    fi
    # And it succeeds once the holder is gone
    set +e
    THIRD=$( (cd "$LOCKDIR" && DEPLOY_LOCK_WAIT=1 bash -c ". '$LOCKFNS'; acquire_deploy_lock") 2>&1 )
    THIRD_RC=$?
    set -e
    if [ "$THIRD_RC" -eq 0 ]; then
        ok "the lock is released when the holder's shell exits"
    else
        bad "the lock was not released (rc=$THIRD_RC): a crashed deploy wedges every later one. Output:$(printf '\n    %s' "$THIRD")"
    fi
else
    echo "  skip flock is not installed here (macOS); CI runs this case on Linux"
fi
# The fatal branch is asserted textually, because making `command -v flock` fail
# would mean running the block with a PATH that has no coreutils either.
if grep -q 'refusing to deploy unserialized' "$ROOT/lock-deploy"; then
    ok "a missing flock is fatal, not a warning"
else
    bad "the block tolerates a missing flock — deploys would silently go back to being unserialized"
fi

# --- 12. every writer of the VM's .env uses the new scheme -----------------
# deploy-remote.sh's two callers edit .env themselves, before it runs, so the
# scheme has to hold in all three places or the interleaving comes back.
echo "all three .env writers take the lock and snapshot under a timestamp"
WORKFLOW=../.github/workflows/deploy.yml
for f in deploy.sh rollback.sh "$WORKFLOW"; do
    if grep -qE '^[^#]*cp[[:space:]]+\.env[[:space:]]+\.env\.backup[[:space:]]*$' "$f"; then
        bad "$f still copies .env to the single .env.backup slot — a concurrent writer overwrites it"
    else
        ok "$f does not use the single .env.backup slot"
    fi
    # ".env.backup.<something expanded>": $(date +%s) inline, or a $ts set above
    if grep -qE '[.]env[.]backup[.]["]?[$]' "$f" || grep -q 'snapshot_env' "$f"; then
        ok "$f snapshots .env under a timestamp"
    else
        bad "$f does not snapshot .env under a timestamp"
    fi
    if grep -q 'flock -w' "$f"; then
        ok "$f takes the deploy lock"
    else
        bad "$f mutates the VM's .env without taking the deploy lock"
    fi
done
if grep -q 'group: production-deploy' "$WORKFLOW"; then
    ok "the deploy workflow serializes its own runs (concurrency group)"
else
    bad "$WORKFLOW has no 'production-deploy' concurrency group — two CI deploys can still interleave"
fi
if grep -q 'cancel-in-progress: false' "$WORKFLOW"; then
    ok "queued rather than cancelled (a cancelled converge leaves the VM half-deployed)"
else
    bad "$WORKFLOW cancels in-progress deploys, which can abort a converge halfway"
fi
# The verified target must be written where the health checks passed, and
# nowhere else.
if grep -qx 'promote_env_lastgood' deploy-remote.sh; then
    ok "deploy-remote.sh promotes .env.lastgood after its health checks"
else
    bad "deploy-remote.sh never promotes .env.lastgood, so the auto-rollback target is unverified again"
fi
if grep -qE 'ROLLBACK_ENV=.*rollback_env_source' deploy-remote.sh; then
    ok "deploy-remote.sh's auto-rollback restores the verified .env"
else
    bad "deploy-remote.sh's auto-rollback does not use rollback_env_source"
fi

# --- 13. the converge refuses to deploy a lost update ----------------------
# The lock is released when the caller's tag-writing ssh session exits and
# retaken by deploy-remote.sh, so the deploy is two critical sections. A writer
# in the gap (a workstation deploy.sh rebuilding .env from tags it read before
# the edit) used to have its tags pulled and converged while THIS run reported,
# health-verified and greened its own. The assertion turns that into a failure.
echo "the converge asserts .env still carries the tags this run set"
extract_block deploy-remote.sh "$TAG_BEGIN" "$TAG_END" > "$ROOT/tagcheck.sh"
if [ ! -s "$ROOT/tagcheck.sh" ]; then
    bad "no tag assertion block in deploy-remote.sh — the converge deploys whatever .env happens to say, including another writer's tags"
else
    # run_tagcheck <expected-fe> <expected-be> <env-fe> <env-be>
    run_tagcheck() {
        set +e
        OUT=$(EXPECTED_FRONTEND_TAG="$1" EXPECTED_BACKEND_TAG="$2" \
              FRONTEND_IMAGE_TAG="$3" BACKEND_IMAGE_TAG="$4" \
              bash "$ROOT/tagcheck.sh" 2>&1)
        RC=$?
        set -e
    }
    # assert_tagcheck <label> <e-fe> <e-be> <fe> <be> <want-rc> <want-substring>
    assert_tagcheck() {
        local label="$1" want_rc="$6" want_text="$7"
        run_tagcheck "$2" "$3" "$4" "$5"
        if [ "$RC" != "$want_rc" ]; then
            bad "$label: expected exit $want_rc, got $RC. Output:$(printf '\n    %s' "$OUT")"
            return
        fi
        case "$OUT" in
            *"$want_text"*) ok "$label -> exit $RC, says '$want_text'" ;;
            *) bad "$label: exited $RC but the output does not mention '$want_text':$(printf '\n    %s' "$OUT")" ;;
        esac
    }
    assert_tagcheck "both tags intact" new1 new2 new1 new2 0 "still carries"
    # THE regression: the workstation's swap put the old frontend tag back
    assert_tagcheck "frontend tag reverted" new1 new2 old1 new2 1 "FRONTEND_IMAGE_TAG in .env is 'old1'"
    assert_tagcheck "backend tag reverted"  new1 new2 new1 old2 1 "BACKEND_IMAGE_TAG in .env is 'old2'"
    # A single-service deploy claims one tag; the other service's tag is
    # whatever the VM already had, so it must not be asserted.
    assert_tagcheck "unclaimed service ignored" "" new2 anything new2 0 "still carries"
fi
# Wiring: the assertion is worthless unless it runs under the lock, before the
# pull, and unless both callers actually pass what they are deploying.
ASSERT_LINE=$(grep -n '^assert_expected_tags$' deploy-remote.sh | head -1 | cut -d: -f1)
LOCK_LINE=$(grep -n '^acquire_deploy_lock$' deploy-remote.sh | head -1 | cut -d: -f1)
PULL_LINE=$(grep -n 'docker compose pull' deploy-remote.sh | head -1 | cut -d: -f1)
if [ -n "$ASSERT_LINE" ] && [ -n "$LOCK_LINE" ] && [ -n "$PULL_LINE" ] &&
   [ "$ASSERT_LINE" -gt "$LOCK_LINE" ] && [ "$ASSERT_LINE" -lt "$PULL_LINE" ]; then
    ok "the assertion runs after the lock and before the first pull"
else
    bad "deploy-remote.sh does not call assert_expected_tags between acquire_deploy_lock and 'docker compose pull' (lock=$LOCK_LINE assert=$ASSERT_LINE pull=$PULL_LINE) — checking after the pull would already have deployed the other writer's tags"
fi
if grep -q 'REBUILD_BACKUP_SIDECAR" "\$FRONTEND_IMAGE_TAG" "\$BACKEND_IMAGE_TAG"' deploy.sh; then
    ok "deploy.sh tells the converge which tags it swapped in"
else
    bad "deploy.sh does not pass its two image tags to deploy-remote.sh, so its own lost updates stay silent"
fi
if grep -q 'EXPECTED_FRONTEND_TAG" "\$EXPECTED_BACKEND_TAG"' "$WORKFLOW"; then
    ok "the deploy workflow tells the converge which tags it edited in"
else
    bad "$WORKFLOW does not pass EXPECTED_FRONTEND_TAG/EXPECTED_BACKEND_TAG to deploy-remote.sh, so a run can still green a deploy of somebody else's tags"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "OK: the shared deploy-script blocks (P10 transition, P8 backup gate, H9 lock + tag assertion) all passed"
else
    echo "$FAILURES assertion(s) failed"
    exit 1
fi
