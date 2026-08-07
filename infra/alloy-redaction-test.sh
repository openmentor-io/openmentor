#!/usr/bin/env bash
# ============================================================================
# Alloy PII/secret redaction stage (C11)
# ============================================================================
# loki.process "redact_pii" is the last thing that touches a log line before it
# leaves the host for Grafana Cloud. It exists because C11 found a real user's
# address in production Loki, written by a line that had already been reviewed
# twice — so there is one rule that does not depend on any Go code being right.
#
# It is also, for that reason, the most dangerous component in the file: Alloy is
# a live pipeline, and a component that fails to load takes ALL log shipping down
# with it. Nothing in this repo validated config.alloy before this test — no
# alloy fmt, no parse, nothing — so this is also the first structural check the
# file has ever had.
#
# Cases:
#   1. every log path into loki.write goes through redact_pii — the file-tail
#      pipeline AND the database-observability pipeline, which is the one no
#      application-level rule can reach
#   2. redact_pii forwards to loki.write, so the choke point is not a dead end
#      that silently swallows every line
#   3. redact_pii contains ONLY stage.replace, and every expression captures a
#      group. A stage.drop or stage.match could discard logs; a stage.replace can
#      only rewrite bytes, which caps the worst failure mode at an ugly line
#      instead of an empty dashboard. And stage.replace substitutes the GROUPS,
#      so an expression without one would replace the whole match and take the
#      surrounding JSON with it
#   4. the real expressions, extracted from the file, match the leaks they claim
#      to and leave alone the fields an operator needs — including an address the
#      Go layer already masked, which must not be mangled twice
#   5. after every replacement the line is still valid JSON, because the
#      query-time parser and every dashboard depend on that
#   6. the config PARSES and every component reference resolves, checked with the
#      same pinned Alloy image docker-compose.yml runs. This is the check that
#      catches the failure that stops all shipping: `alloy validate` rejects a
#      forward_to pointing at a component that does not exist
#
# The expressions are extracted from config.alloy and executed here, so this
# cannot pass against a stage the file does not contain. They are run through
# Python's re rather than grep -E because Alloy uses RE2: `(?i)` and `(?:…)` are
# not POSIX ERE, and grep quietly does the wrong thing with both.
# api/pkg/redact/alloy_config_test.go runs the same strings through Go's own RE2
# engine, which is the one Alloy actually uses.
#
# Usage: ./alloy-redaction-test.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

ALLOY=alloy/config.alloy
COMPOSE=docker-compose.yml

command -v python3 >/dev/null || { echo "error: python3 is required" >&2; exit 1; }

FAILURES=0
ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILURES=$((FAILURES + 1)); }
warn() { printf '  WARN %s\n' "$1"; }

# A named component's block, from its header line to the first lone closing brace
# at column 0.
component_block() {
    awk -v head="$1" '
        index($0, head) == 1 { on = 1 }
        on { print }
        on && /^}$/ { exit }
    ' "$ALLOY"
}

# The `expression` values inside redact_pii, one per line, exactly as written in
# the file (river escaping intact — the Python helper unescapes them).
redaction_expressions() {
    component_block 'loki.process "redact_pii" {' \
        | sed -n 's/^ *expression *= *"\(.*\)"$/\1/p'
}

redaction_replacements() {
    component_block 'loki.process "redact_pii" {' \
        | sed -n 's/^ *replace *= *"\(.*\)"$/\1/p'
}

echo "== Alloy PII redaction (C11): $ALLOY"

# --------------------------------------------------------------------------
# 1. every log path into Loki goes through redact_pii
# --------------------------------------------------------------------------
if ! grep -q '^loki\.process "redact_pii" {' "$ALLOY"; then
    bad "loki.process \"redact_pii\" is missing: nothing redacts logs on the way out"
    echo
    echo "FAILED"
    exit 1
fi
ok "loki.process \"redact_pii\" is declared"

for producer in \
    'loki.process "parse_json_logs" {' \
    'loki.relabel "database_observability_postgres_openmentor" {'
do
    name=${producer% \{}
    forwards=$(component_block "$producer" | sed -n 's/^ *forward_to *= *//p' | head -1)
    case "$forwards" in
        *loki.process.redact_pii.receiver*)
            ok "$name forwards to redact_pii" ;;
        *loki.write.grafana_cloud.receiver*)
            bad "$name still writes straight to Loki, bypassing redaction: $forwards" ;;
        *)
            bad "$name has an unrecognized forward_to: ${forwards:-<none found>}" ;;
    esac
done

# Nothing else may reach loki.write directly. This is what catches a NEW log
# source being added later and wired past the choke point.
direct=$(grep -c 'loki\.write\.grafana_cloud\.receiver' "$ALLOY" | tr -d ' ')
if [ "$direct" = "1" ]; then
    ok "loki.write has exactly one producer (redact_pii)"
else
    bad "loki.write.grafana_cloud.receiver is referenced $direct times, want 1 — a source bypasses redact_pii"
    grep -n 'loki\.write\.grafana_cloud\.receiver' "$ALLOY" | sed 's/^/       /'
fi

# --------------------------------------------------------------------------
# 2. redact_pii is not a dead end
# --------------------------------------------------------------------------
if component_block 'loki.process "redact_pii" {' | grep -q 'forward_to *= *\[loki\.write\.grafana_cloud\.receiver\]'; then
    ok "redact_pii forwards to loki.write"
else
    bad "redact_pii does not forward to loki.write: every log line would be dropped"
fi

# --------------------------------------------------------------------------
# 3. only stage.replace, and every expression captures a group
# --------------------------------------------------------------------------
stages=$(component_block 'loki.process "redact_pii" {' | sed -n 's/^ *\(stage\.[a-z_]*\) *{.*/\1/p' | sort -u)
unexpected=$(printf '%s\n' "$stages" | grep -v '^stage\.replace$' || true)
if [ -z "$unexpected" ]; then
    ok "redact_pii contains only stage.replace (cannot drop a log line)"
else
    bad "redact_pii contains a stage that can discard or reroute logs: $(printf '%s' "$unexpected" | tr '\n' ' ')"
fi

count=$(redaction_expressions | wc -l | tr -d ' ')
if [ "$count" -ge 3 ]; then
    ok "redact_pii has $count replace expressions"
else
    bad "redact_pii has only $count replace expressions, want at least 3 (dsn, credentials, email)"
fi
if [ "$count" != "$(redaction_replacements | wc -l | tr -d ' ')" ]; then
    bad "every stage.replace must declare a replace value"
fi

# The marker has to be loud and distinct from the Go masks, so a line that
# reaches it is visibly a bug rather than mistaken for a correctly-masked one.
# It must also be JSON-string-safe.
while IFS= read -r replacement; do
    case "$replacement" in
        '[REDACTED_EMAIL]'|'[REDACTED_SECRET]')
            ok "replacement marker $replacement is distinct from the Go masks" ;;
        *)
            bad "replacement marker '$replacement' is not one of the documented markers" ;;
    esac
    case "$replacement" in
        *'"'*|*'\'*) bad "replacement marker '$replacement' would break the JSON line" ;;
    esac
done <<EOF
$(redaction_replacements)
EOF

# --------------------------------------------------------------------------
# 4 + 5. behavior, run through a real regex engine
# --------------------------------------------------------------------------
# Every sample address is in an RFC 2606 example domain: no fixture, failure
# message or CI log in this repo carries an address that could be a person's.
if redaction_expressions | python3 alloy-redaction-behavior.py; then
    ok "expressions redact the leaks and leave operator fields intact (see detail above)"
else
    bad "expression behavior check failed (detail above)"
fi

# --------------------------------------------------------------------------
# 6. the config parses and every reference resolves
# --------------------------------------------------------------------------
# The same image docker-compose.yml pins, so this validates against the version
# that actually runs. Best-effort on purpose: `make check` must not start failing
# because a Docker Hub pull was rate-limited, and the checks above are the hard
# gate. Locally and in CI this normally runs.
image=$(sed -n 's/^ *image: *\(grafana\/alloy:[^ ]*\) *$/\1/p' "$COMPOSE" | head -1)
if [ -z "$image" ]; then
    bad "could not read the pinned Alloy image out of $COMPOSE"
elif ! command -v docker >/dev/null || ! docker info >/dev/null 2>&1; then
    warn "docker unavailable: skipped 'alloy validate' against $image"
else
    if validate_out=$(docker run --rm --entrypoint /bin/alloy \
        -v "$PWD/$ALLOY:/tmp/config.alloy:ro" "$image" validate /tmp/config.alloy 2>&1); then
        ok "$image validate: config parses and every component reference resolves"
    else
        case "$validate_out" in
            *"validation failed"*|*"Error:"*)
                bad "$image rejected the config — Alloy would fail to load and ALL log shipping stops"
                printf '%s\n' "$validate_out" | sed 's/^/       /' ;;
            *)
                warn "could not run 'alloy validate' (image pull or runtime problem), skipped:"
                printf '%s\n' "$validate_out" | tail -3 | sed 's/^/       /' ;;
        esac
    fi
fi

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "PASSED"
else
    echo "FAILED ($FAILURES)"
    exit 1
fi
