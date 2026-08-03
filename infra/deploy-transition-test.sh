#!/usr/bin/env bash
# ============================================================================
# Tests the P10 .env.runtime transition (SECURITY P10)
# ============================================================================
# deploy-remote.sh and rollback.sh both run ON the VM against whatever
# docker-compose.yml is already there — and a default `./deploy.sh`
# (frontend backend) never syncs infra/. Since compose defaults
# `env_file.required` to true, deleting .env.runtime while the VM's compose
# file still declares it aborts `pull`/`up` halfway through a live deploy.
#
# Cases:
#   1. both scripts carry the byte-identical transition block
#   2. the block survives rollback.sh's unquoted heredoc unexpanded
#   3. pre-P10 compose file on the VM -> .env.runtime regenerated (600, no
#      image-tag lines) and the upgrade order printed
#   4. current compose file -> .env.runtime deleted
#
# Usage: ./deploy-transition-test.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

BEGIN='# --- P10 .env.runtime transition'
END='# --- end P10 .env.runtime transition'

# The block as literally written in $1, markers included
extract() {
    awk -v b="$BEGIN" -v e="$END" '
        index($0, b) == 1 { on = 1 }
        on               { print }
        on && index($0, e) == 1 { exit }
    ' "$1"
}

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

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "OK: all .env.runtime transition cases passed"
else
    echo "$FAILURES assertion(s) failed"
    exit 1
fi
