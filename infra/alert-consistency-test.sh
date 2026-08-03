#!/usr/bin/env bash
# ============================================================================
# Backup-freshness alert consistency (SECURITY P8)
# ============================================================================
# DatabaseBackupStale spans four files that nothing else ties together, and
# every seam has already produced a silent-failure bug:
#
#   postgres-backup/backup.sh   publishes the gauges
#   alloy/config.alloy          labels them on the way to Grafana Cloud
#   ../grafana/alerting/alert-rules.yaml   evaluates them
#   ../grafana/dashboards/om-database-infra.json   is what a human reads
#
# Cases:
#   1. every openmentor_db_backup_* series the rule reads is really published
#   2. the rule groups by a label Alloy actually attaches to those gauges, so
#      each deployment gets its own alert instance instead of one global max()
#   3. the freshness threshold comes from the sidecar's own configured window,
#      not a hardcoded duration that drifts from BACKUP_MAX_AGE_HOURS
#   4. every sys.env() Alloy reads is passed to the container, so a label does
#      not silently fall back to its default
#   5. the backup panels carry 2 and 3 too: a panel that maxes globally or
#      draws a fixed 26h line contradicts the alert it exists to illustrate
#
# The alert rules are DESIRED state (the stack has no Grafana-managed rules —
# ../grafana/README.md); that half of this test guards the file an operator
# applies. The dashboard is Git-Synced hourly, so case 5 guards live Grafana.
#
# Usage: ./alert-consistency-test.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

RULES=../grafana/alerting/alert-rules.yaml
DASHBOARD=../grafana/dashboards/om-database-infra.json
ALLOY=alloy/config.alloy
SIDECAR=postgres-backup/backup.sh
COMPOSE=docker-compose.yml
ALLOWLIST=env-allowlist.txt

command -v jq >/dev/null || { echo "error: jq is required" >&2; exit 1; }

FAILURES=0
ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

# The om-db-backup-stale rule, up to the next rule in the group
RULE=$(awk '/^      - uid: om-db-backup-stale$/ { on = 1; print; next }
            on && /^      - uid: / { exit }
            on { print }' "$RULES")
[ -n "$RULE" ] || { echo "error: om-db-backup-stale not found in $RULES" >&2; exit 1; }

# --- 1. the rule only reads gauges the sidecar publishes -------------------
echo "the alert reads only backup gauges the sidecar publishes"
# `|| true`: an empty match must produce a FAIL below, not kill the script
PUBLISHED=$( { grep -oE '^        echo "openmentor_db_backup_[a-z_]+' "$SIDECAR" || true; } |
    sed 's/.*"//' | sort -u)
for metric in $( { printf '%s\n' "$RULE" | grep -oE 'openmentor_db_backup_[a-z_]+' || true; } | sort -u); do
    if printf '%s\n' "$PUBLISHED" | grep -qx "$metric"; then
        ok "$metric is published by $SIDECAR"
    else
        bad "$metric is queried by the alert but never written by $SIDECAR"
    fi
done

# --- 2. per-deployment evaluation ------------------------------------------
# A global max() over gauges from several VMs reports only the newest backup
# anywhere, so one healthy pipeline hides every stale one.
echo "the alert evaluates freshness per deployment, using a label Alloy sets"
GROUP_LABELS=$( { printf '%s\n' "$RULE" | grep -oE 'max by \([a-z_]+\)' || true; } |
    sed 's/.*(//;s/)//' | sort -u)
if [ -z "$GROUP_LABELS" ]; then
    bad "the expression uses a global max(): a fresh backup on any one deployment would keep this rule Normal for all of them"
fi
# Labels discovery.relabel "backup_metrics" stamps onto the scrape target
ALLOY_LABELS=$( { awk '/^discovery\.relabel "backup_metrics"/ { on = 1 }
                     on && /^}/ { exit }
                     on' "$ALLOY" | grep -oE 'target_label[[:space:]]*=[[:space:]]*"[a-z_]+"' || true; } |
    sed 's/.*"\([a-z_]*\)"/\1/' | sort -u)
for label in $GROUP_LABELS; do
    if printf '%s\n' "$ALLOY_LABELS" | grep -qx "$label"; then
        ok "'$label' is stamped on the backup gauges by $ALLOY"
    else
        bad "the alert groups by '$label', which $ALLOY does not put on these gauges — every deployment would collapse into one instance"
    fi
done

# --- 3. the threshold is the configured window, not a copy of it ------------
echo "the freshness threshold is derived from BACKUP_MAX_AGE_HOURS"
if printf '%s\n' "$RULE" | grep -q 'openmentor_db_backup_max_age_seconds'; then
    ok "compares against the published window"
else
    bad "the rule does not read openmentor_db_backup_max_age_seconds, so raising BACKUP_MAX_AGE_HOURS would leave it paging at the old threshold"
fi
PARAMS=$( { printf '%s\n' "$RULE" | grep -oE 'params: \[[0-9.]+\]' || true; } | sed 's/params: \[//;s/\]//')
if [ "$PARAMS" = "0" ]; then
    ok "threshold is 'seconds past the window > 0'"
else
    bad "threshold param is '$PARAMS', i.e. a hardcoded duration that drifts from BACKUP_MAX_AGE_HOURS"
fi

# --- 4. every sys.env() Alloy reads actually reaches the container ----------
# HOSTNAME is Docker's own; everything else must be declared for the service
# (compose) and allowed for it (the P10 allowlist), or the label quietly falls
# back to its default.
echo "every variable $ALLOY reads is passed to the alloy container"
ALLOY_ALLOWED=$(awk '/^\[alloy\]/ { on = 1; next } /^\[/ { on = 0 }
                     on && /^[A-Z]/ { print $1 }' "$ALLOWLIST")
ALLOY_DECLARED=$( { awk '/^  alloy:/ { on = 1; next } on && /^  [a-z]/ { exit } on' "$COMPOSE" |
    grep -oE '^      - [A-Z][A-Z0-9_]*' || true; } | sed 's/.*- //' | sort -u)
MISSING=0
for key in $( { grep -oE 'sys\.env\("[A-Za-z_][A-Za-z0-9_]*"\)' "$ALLOY" || true; } |
    sed 's/.*("//;s/")//' | sort -u); do
    [ "$key" = "HOSTNAME" ] && continue
    if ! printf '%s\n' "$ALLOY_ALLOWED" | grep -qx "$key"; then
        bad "$key is read by $ALLOY but not in [alloy] of $ALLOWLIST"
        MISSING=1
    elif ! printf '%s\n' "$ALLOY_DECLARED" | grep -qx "$key"; then
        bad "$key is read by $ALLOY but the alloy service in $COMPOSE does not pass it, so it silently falls back to its default"
        MISSING=1
    fi
done
[ "$MISSING" -eq 1 ] || ok "all sys.env() keys are declared and allowlisted"

# --- 5. the dashboard panels say the same thing as the rule -----------------
# Nobody reads the rule during an incident; they read this row. A global max()
# there hides a stale production dump behind a fresh staging one just as it
# would in the rule, and a fixed threshold silently stops matching the alert
# the first time BACKUP_MAX_AGE_HOURS moves.
echo "the backup panels evaluate per deployment, against the published window"
PANEL_EXPRS=$(jq -r '.panels[] | .targets[]?.expr? // empty
                     | select(test("openmentor_db_backup_"))' "$DASHBOARD")
if [ -z "$PANEL_EXPRS" ]; then
    bad "no panel in $DASHBOARD queries the backup gauges, so this check would pass vacuously"
fi
PANEL_SCOPE_OK=1
while IFS= read -r expr; do
    [ -n "$expr" ] || continue
    if printf '%s\n' "$expr" | grep -qE 'max[[:space:]]*\('; then
        bad "panel query aggregates globally, so one healthy deployment hides the rest: $expr"
        PANEL_SCOPE_OK=0
    fi
    for label in $( { printf '%s\n' "$expr" | grep -oE 'max by \([a-z_]+\)' || true; } |
        sed 's/.*(//;s/)//' | sort -u); do
        if ! printf '%s\n' "$GROUP_LABELS" | grep -qx "$label"; then
            bad "panel groups by '$label', the rule by '$(printf '%s' "$GROUP_LABELS" | tr '\n' ' ')' — they would disagree about scope"
            PANEL_SCOPE_OK=0
        fi
    done
done <<PANEL_EOF
$PANEL_EXPRS
PANEL_EOF
[ "$PANEL_SCOPE_OK" -eq 1 ] && [ -n "$PANEL_EXPRS" ] &&
    ok "every backup panel query groups by the label the rule groups by"
if printf '%s\n' "$PANEL_EXPRS" | grep -q 'openmentor_db_backup_max_age_seconds'; then
    ok "the panels read the published window, so raising BACKUP_MAX_AGE_HOURS moves them too"
else
    bad "no panel reads openmentor_db_backup_max_age_seconds: the row would keep showing the old window after BACKUP_MAX_AGE_HOURS changes"
fi
# Steps must be offsets from that window (<= 0, e.g. red at "0s past it"), never
# an absolute age like 93600.
FIXED_STEPS=$(jq -r '[.panels[]
                      | select([.targets[]?.expr? // ""] | join(" ") | test("openmentor_db_backup_"))
                      | .fieldConfig.defaults.thresholds.steps[]?.value
                      | select(. != null) | select(. > 0)] | map(tostring) | join(", ")' "$DASHBOARD")
if [ -n "$FIXED_STEPS" ]; then
    bad "backup panel threshold(s) at ${FIXED_STEPS}s are absolute ages, not offsets from the published window"
else
    ok "no backup panel threshold hardcodes a duration"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "OK: the backup alert, its panels, the Alloy labels and the sidecar gauges agree"
else
    echo "$FAILURES assertion(s) failed"
    exit 1
fi
