#!/usr/bin/env bash
# ============================================================================
# The one definition of "shellcheck this repository"
# ============================================================================
# Both callers run THIS script, so the file list and the severity exist once:
#
#   • infra/Makefile      `make shellcheck` (part of `make check`) -> infra
#   • checks.yml          'Scripts: shellcheck'                    -> outside-infra
#
# The split is only about who runs which half: infra/'s scripts are gated by the
# infra filter through `make check`, everything else by the scripts/runbooks
# filter, and neither half is linted twice.
#
# Files come from `git ls-files '*.sh'`, not from a hand-written glob, so a new
# script is covered the day it is added.
#
# --severity=warning, not the default "style": the three long deploy scripts
# carry ~40 pre-existing style notes (SC2181, SC2269 and friends), and gating on
# those today would mean a mechanical rewrite of deploy.sh inside an unrelated
# change. Warnings are the class that hides real bugs — quoting, word splitting,
# traps that expand at the wrong time.
#
# Usage: scripts/shellcheck-all.sh [infra|outside-infra|all]   (default: all)
# ============================================================================
set -euo pipefail

SEVERITY=${SHELLCHECK_SEVERITY:-warning}
SCOPE=${1:-all}

command -v shellcheck >/dev/null || {
    echo "❌ shellcheck is not installed (brew install shellcheck / apt-get install shellcheck)"
    exit 1
}

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "❌ not inside a git work tree — the file list comes from 'git ls-files'"
    exit 1
}
cd "$ROOT"

# `|| true`: grep exits 1 on an empty selection, which is not an error here.
case "$SCOPE" in
    infra)         FILES=$(git ls-files '*.sh' | { grep '^infra/' || true; }) ;;
    outside-infra) FILES=$(git ls-files '*.sh' | { grep -v '^infra/' || true; }) ;;
    all)           FILES=$(git ls-files '*.sh') ;;
    *)
        echo "❌ unknown scope '$SCOPE' (expected: infra, outside-infra, all)"
        exit 1
        ;;
esac

if [ -z "$FILES" ]; then
    echo "no shell scripts in scope '$SCOPE' — nothing to check"
    exit 0
fi

echo "Running shellcheck (severity=$SEVERITY) over scope '$SCOPE':"
# shellcheck disable=SC2086 # deliberate word splitting over the file list
printf '  %s\n' $FILES

# shellcheck disable=SC2086 # deliberate word splitting over the file list
shellcheck --severity="$SEVERITY" $FILES
