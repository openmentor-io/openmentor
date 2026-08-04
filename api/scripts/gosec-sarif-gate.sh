#!/usr/bin/env bash
# ============================================================================
# Fail on gosec HIGH findings that are not in the reviewed baseline (H6)
# ============================================================================
# Reads a gosec SARIF report and compares its HIGH-severity results against
# api/gosec-baseline.txt. Exit 1 on anything not baselined, and on a baseline
# entry that no longer matches anything (a stale exemption is a hole).
#
# WHY a baseline instead of zero-tolerance: the repo has three reviewed HIGH
# findings today (see the baseline file). Zero-tolerance would mean this gate is
# red from its first run and gets ignored — which is exactly how the old
# `-no-fail` + `continue-on-error` pair became invisible.
#
# WHY rule+file and not rule+file+line: line numbers move on every edit above
# the finding, and a gate that goes red on unrelated refactors is a gate people
# route around.
#
# Portable to bash 3.2 (macOS /bin/bash): no mapfile, no associative arrays.
#
# Usage: ./scripts/gosec-sarif-gate.sh [path/to/results.sarif]
#        make security-sarif-gate SARIF=../results.sarif
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")/.."

SARIF=${1:-results.sarif}
BASELINE=gosec-baseline.txt

if [ ! -f "$SARIF" ]; then
    echo "❌ no SARIF report at '$SARIF' — did gosec run? (it writes one even with -no-fail)"
    exit 1
fi
if [ ! -f "$BASELINE" ]; then
    echo "❌ missing $BASELINE"
    exit 1
fi
command -v jq >/dev/null || { echo "❌ jq is required"; exit 1; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# gosec's severity lives in the rule's properties.tags ("HIGH"/"MEDIUM"/"LOW"),
# not in a SARIF security-severity property, and its `level` is "error" for
# MEDIUM too — so tags is the only usable severity signal here.
jq -r '
    .runs[0] as $run
    | ($run.tool.driver.rules
       | map(select(.properties.tags // [] | index("HIGH")) | .id)) as $high
    | $run.results // []
    | map(select(.ruleId as $r | $high | index($r)))
    | map(.ruleId + " " + .locations[0].physicalLocation.artifactLocation.uri
          + "\t(line " + (.locations[0].physicalLocation.region.startLine | tostring) + ") "
          + .message.text)
    | sort | unique | .[]
' "$SARIF" > "$WORK/high"

# "<RULE> <path>" per baseline line, comments and blanks stripped
sed -e 's/#.*//' -e 's/[[:space:]]*$//' "$BASELINE" | grep -v '^[[:space:]]*$' > "$WORK/base" || true
: > "$WORK/seen"

rc=0
while IFS= read -r finding; do
    [ -n "$finding" ] || continue
    key=${finding%%$'\t'*}
    detail=${finding#*$'\t'}
    if grep -Fxq "$key" "$WORK/base"; then
        echo "  baselined  $key $detail"
        echo "$key" >> "$WORK/seen"
    else
        echo "::error::new gosec HIGH finding: $key $detail"
        rc=1
    fi
done < "$WORK/high"

while IFS= read -r b; do
    [ -n "$b" ] || continue
    if ! grep -Fxq "$b" "$WORK/seen"; then
        echo "::error::stale baseline entry '$b' in $BASELINE matches no finding — the code moved or was fixed. Remove the line; leaving it exempts a file that may grow a real finding later."
        rc=1
    fi
done < "$WORK/base"

if [ "$rc" -eq 0 ]; then
    echo "✅ no gosec HIGH findings outside $BASELINE"
else
    echo "❌ gosec HIGH gate failed. Fix the finding, or — if it is a false positive —"
    echo "   add a '#nosec Gxxx -- why' comment at the site (gosec ignores //nolint)"
    echo "   and remove the baseline line."
fi
exit "$rc"
