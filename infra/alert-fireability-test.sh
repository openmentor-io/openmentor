#!/usr/bin/env bash
# ============================================================================
# Every alert rule must be able to fire (SECURITY H13)
# ============================================================================
# Four of the fourteen rules in ../grafana/alerting/alert-rules.yaml were
# permanently green for reasons no reader of the file could see:
#
#   * ContainerHighMemory compared a raw byte count against 1073741824 (1 GiB)
#     while the LARGEST mem_limit in docker-compose.yml is 768m — the kernel
#     OOM-killed the container long before the rule could trigger, so the page
#     was structurally unreachable.
#   * ContainerHighCPU compared `rate(...) * 100` against 90, which is 90% of
#     ONE core while calling itself "CPU above 90%". machine_cpu_cores is 4 on
#     this VM, so it fired at 22.5% of the machine.
#   * DBErrorRate and DBLatencyP95 aggregated db_client_* across the API and the
#     worker with no service selector, so the service doing less database work
#     could fail completely without moving the combined number.
#
# A threshold's reachability is a property of the file plus docker-compose.yml,
# so it is checkable here — no Grafana, no network. Cases:
#
#   1. the file parses and every rule has the fields Grafana requires
#   2. no rule compares a container_memory_* byte count against a threshold
#      above the largest mem_limit in docker-compose.yml — the regression that
#      made ContainerHighMemory unreachable
#   3. ContainerHighMemory's per-container divisors ARE the mem_limits in
#      docker-compose.yml, every long-running container is covered exactly once,
#      and the ratio threshold is a percentage
#   4. ContainerHighCPU normalises by machine_cpu_cores, so its threshold means
#      what its summary says
#   5. every rule reading db_client_* groups by service_name
#   6. no rule keys a template on a label its own expression does not produce
#   7. noDataState: OK is only used where an empty result really is healthy
#
# Case 3 is what stops the hardcoded divisors rotting: changing a mem_limit in
# docker-compose.yml without touching the rule fails `make check`. They are
# hardcoded because Grafana Cloud Adaptive Metrics has
# container_spec_memory_limit_bytes aggregated on this tenant and strips `name`
# from it, so the natural `/ on (name) group_left ()` form errors out.
#
# Usage: ./alert-fireability-test.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

RULES=../grafana/alerting/alert-rules.yaml
COMPOSE=docker-compose.yml

command -v jq >/dev/null || { echo "error: jq is required" >&2; exit 1; }
command -v yq >/dev/null || { echo "error: yq is required" >&2; exit 1; }

FAILURES=0
ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILURES=$((FAILURES + 1)); }

RJ=$(mktemp)
trap 'rm -f "$RJ"' EXIT
yq -o=json '.' "$RULES" > "$RJ"

rule_json() { jq -c --arg u "$1" '.groups[0].rules[] | select(.uid == $u)' "$RJ"; }
# The rule's own YAML text, comments included — yq drops those, and some
# assertions below are about what the file says next to a setting.
rule_block() {
    awk -v head="      - uid: $1" '
        index($0, head) == 1 && length($0) == length(head) { on = 1; print; next }
        on && /^      - uid: / { exit }
        on { print }
    ' "$RULES"
}
# Every PromQL expression in a rule (the datasource queries, not the expressions)
rule_exprs() { rule_json "$1" | jq -r '.data[].model.expr // empty'; }
rule_thresholds() {
    rule_json "$1" | jq -r '.data[].model.conditions[]?.evaluator.params[]? // empty'
}

# container_name -> mem_limit in bytes, straight out of compose. Awk rather than
# yq because mem_limit is a docker shorthand ("768m") that yq keeps as a string
# either way, and the container_name lives in the same service block.
compose_limits() {
    awk '
        /^  [a-z][a-z0-9_-]*:/ { svc = $1; sub(":", "", svc); cname = ""; lim = "" }
        /^    container_name:/ { cname = $2 }
        /^    mem_limit:/     { lim = $2 }
        cname != "" && lim != "" { print cname, lim; cname = ""; lim = "" }
    ' "$COMPOSE" |
    while read -r name lim; do
        case "$lim" in
            *m) printf '%s %s\n' "$name" $(( ${lim%m} * 1024 * 1024 )) ;;
            *g) printf '%s %s\n' "$name" $(( ${lim%g} * 1024 * 1024 * 1024 )) ;;
            *)  printf '%s %s\n' "$name" "$lim" ;;
        esac
    done
}

LIMITS=$(compose_limits)
[ -n "$LIMITS" ] || { echo "error: no container_name/mem_limit pairs found in $COMPOSE" >&2; exit 1; }
MAX_LIMIT=$(printf '%s\n' "$LIMITS" | awk '{ if ($2 > m) m = $2 } END { print m }')

# --- 1. the file is loadable and structurally complete ---------------------
echo "every rule has the fields the provisioning API requires"
UIDS=$(jq -r '.groups[0].rules[].uid' "$RJ")
[ -n "$UIDS" ] || { echo "  FAIL no rules parsed out of $RULES" >&2; exit 1; }
for u in $UIDS; do
    MISSING=$(rule_json "$u" | jq -r '
        [ if .title then empty else "title" end,
          if .condition then empty else "condition" end,
          if .noDataState then empty else "noDataState" end,
          if .execErrState then empty else "execErrState" end,
          if (.data | length) > 0 then empty else "data" end,
          if .annotations.summary then empty else "annotations.summary" end ] | join(", ")')
    if [ -z "$MISSING" ]; then
        ok "$u is complete"
    else
        bad "$u is missing: $MISSING"
    fi
    # The condition must name a refId the rule actually defines, or Grafana
    # accepts the rule and then errors on every evaluation.
    COND=$(rule_json "$u" | jq -r '.condition')
    if rule_json "$u" | jq -e --arg c "$COND" '[.data[].refId] | index($c)' >/dev/null; then
        ok "$u's condition '$COND' names one of its own queries"
    else
        bad "$u's condition is '$COND', which is not a refId in its data[] — it would error on every evaluation"
    fi
done

# --- 2. no unreachable absolute memory threshold ---------------------------
# The exact H13 regression: a byte threshold above every mem_limit means the
# kernel kills the container first, so the alert is decoration.
echo "no rule compares container memory bytes against more than the largest mem_limit ($MAX_LIMIT)"
MEM_ABS_OK=1
for u in $UIDS; do
    EXPRS=$(rule_exprs "$u")
    # Only bare byte comparisons matter; a ratio expression divides by a limit.
    printf '%s' "$EXPRS" | grep -q 'container_memory_' || continue
    if printf '%s' "$EXPRS" | grep -qE 'container_memory_[a-z_]+_bytes\{[^}]*\}[[:space:]]*/'; then
        continue    # a ratio; case 3 checks its divisors
    fi
    for t in $(rule_thresholds "$u"); do
        # Integer compare; thresholds in this file are whole numbers.
        if [ "${t%%.*}" -gt "$MAX_LIMIT" ] 2>/dev/null; then
            bad "$u compares container memory against $t bytes, above every mem_limit in $COMPOSE (max $MAX_LIMIT) — the kernel OOM-kills first, so it can never fire"
            MEM_ABS_OK=0
        fi
    done
done
[ "$MEM_ABS_OK" -eq 1 ] && ok "no unreachable absolute byte threshold"

# --- 3. the memory ratio's divisors are the real mem_limits ----------------
echo "ContainerHighMemory divides each container by its own mem_limit"
MEM_EXPR=$(rule_exprs om-container-high-memory)
if [ -z "$MEM_EXPR" ]; then
    bad "om-container-high-memory not found in $RULES"
else
    MEM_THRESH=$(rule_thresholds om-container-high-memory)
    if [ "${MEM_THRESH%%.*}" -le 100 ] 2>/dev/null && [ "${MEM_THRESH%%.*}" -gt 0 ] 2>/dev/null; then
        ok "its threshold ($MEM_THRESH) is a reachable percentage"
    else
        bad "its threshold is '$MEM_THRESH', which is not a percentage — the rule reads as a ratio but is compared like bytes"
    fi
    if ! printf '%s' "$MEM_EXPR" | grep -q 'container_memory_working_set_bytes'; then
        bad "it does not read container_memory_working_set_bytes; container_memory_usage_bytes includes reclaimable page cache, so it drifts from what the OOM killer accounts"
    else
        ok "it reads the working set, which is what the OOM killer accounts"
    fi

    # migrate is a one-shot that runs to completion and exits; a 15m window over
    # it is meaningless. It must be exempted in writing, not by omission.
    EXEMPT=$( { grep -oE '^ +# openmentor-[a-z-]+ is deliberately absent' "$RULES" || true; } |
        sed 's/.*# //;s/ is deliberately absent//')

    # The `or` union, one branch per line, and every container name any branch
    # names. PromQL regex matchers are fully anchored, so the alternatives are
    # exact names — matching on substrings would invent overlaps the query does
    # not have ("openmentor-postgres" vs "openmentor-postgres-backup").
    BRANCHES=$(printf '%s' "$MEM_EXPR" | tr '\n' ' ' | sed 's/ or /\n/g')
    NAMED=$(printf '%s\n' "$BRANCHES" | grep -oE 'name=~?"[^"]+"' |
        sed 's/.*"\(.*\)"/\1/' | tr '|' '\n' | sort -u)

    # `LIMITS` as name:bytes words, so the loop below leaves stdin free for the
    # nested read over $BRANCHES.
    for entry in $(printf '%s\n' "$LIMITS" | tr ' ' ':'); do
        cname=${entry%%:*}
        limit=${entry##*:}
        if printf '%s\n' "$EXEMPT" | grep -qx "$cname"; then
            if printf '%s\n' "$NAMED" | grep -qx "$cname"; then
                bad "$cname is documented as deliberately absent from om-container-high-memory but its expression names it"
            else
                ok "$cname is exempt, in writing"
            fi
            continue
        fi
        # Match on the matcher's ALTERNATIVES, exactly. PromQL regex matchers are
        # fully anchored, so name=~"openmentor-postgres" does not match
        # openmentor-postgres-backup — a substring test here would invent an
        # overlap that the query does not have.
        MATCHED=0
        DIVISOR=""
        while IFS= read -r br; do
            [ -n "$br" ] || continue
            if printf '%s' "$br" | grep -oE 'name=~?"[^"]+"' | head -1 |
                sed 's/.*"\(.*\)"/\1/' | tr '|' '\n' | grep -qx "$cname"; then
                MATCHED=$((MATCHED + 1))
                DIVISOR=$(printf '%s' "$br" | grep -oE '/ [0-9]+' | tail -1 | tr -d '/ ')
            fi
        done <<BRANCH_EOF
$BRANCHES
BRANCH_EOF
        if [ "$MATCHED" -eq 0 ]; then
            bad "$cname (mem_limit $limit) is in $COMPOSE but no branch of om-container-high-memory names it, so it can reach its limit and be OOM-killed with no alert"
        elif [ "$MATCHED" -ne 1 ]; then
            bad "$cname is named by $MATCHED branches of om-container-high-memory; overlapping branches make its percentage ambiguous"
        elif [ "$DIVISOR" = "$limit" ]; then
            ok "$cname is divided by $DIVISOR, its mem_limit"
        else
            bad "$cname is divided by '${DIVISOR:-<none>}' but its mem_limit in $COMPOSE is $limit — the percentage the page reports would be wrong"
        fi
    done
fi

# --- 4. the CPU rule's denominator is the machine, not one core ------------
echo "ContainerHighCPU normalises by the VM's core count"
CPU_EXPR=$(rule_exprs om-container-high-cpu)
if [ -z "$CPU_EXPR" ]; then
    bad "om-container-high-cpu not found in $RULES"
elif printf '%s' "$CPU_EXPR" | grep -q 'machine_cpu_cores'; then
    ok "divides by machine_cpu_cores, so its threshold is a share of the VM"
    CPU_THRESH=$(rule_thresholds om-container-high-cpu)
    if [ "${CPU_THRESH%%.*}" -le 100 ] 2>/dev/null; then
        ok "its threshold ($CPU_THRESH) is a reachable percentage of the VM"
    else
        bad "its threshold is '$CPU_THRESH', which is more than the whole machine"
    fi
else
    bad "om-container-high-cpu does not divide by machine_cpu_cores: 'rate(...) * 100 > N' is N% of ONE core, so on this 4-core VM a '90%' rule fires at 22.5% of the machine and silently changes meaning when the VM is resized"
fi

# --- 5. db_client_* rules are scoped by service -----------------------------
# The API and the worker share these metric names. The API does far more
# database work, so an unscoped ratio lets the worker fail completely without
# moving the number, and an unscoped histogram_quantile is a p95 of neither.
echo "every rule reading db_client_* separates the API from the worker"
DB_SCOPE_OK=1
for u in $UIDS; do
    EXPRS=$(rule_exprs "$u")
    printf '%s' "$EXPRS" | grep -q 'db_client_' || continue
    if printf '%s' "$EXPRS" | grep -qE 'by \([^)]*service_name'; then
        ok "$u groups by service_name"
    else
        bad "$u aggregates db_client_* with no service_name grouping: the busier service's volume hides the other one entirely"
        DB_SCOPE_OK=0
    fi
done
[ "$DB_SCOPE_OK" -eq 1 ] || true

# --- 6. templates only reference labels the expression produces ------------
# {{ $labels.name }} on a rule whose query drops `name` renders empty, which is
# how an alert arrives naming no container at all.
echo "no annotation templates a label its own expression cannot produce"
for u in $UIDS; do
    ANN=$(rule_json "$u" | jq -r '[.annotations.summary, .annotations.description] | join(" ")')
    EXPRS=$(rule_exprs "$u" | tr '\n' ' ')
    # shellcheck disable=SC2016 # matching the literal Go-template text, not expanding it
    for lbl in $( { printf '%s' "$ANN" | grep -oE '\$labels\.[a-z_]+' || true; } |
        sed 's/.*\.//' | sort -u); do
        # `up`/`pg_up` style bare-metric rules keep every scrape label, so any
        # label is legitimately available there.
        if ! printf '%s' "$EXPRS" | grep -qE 'by \(|max by|sum by'; then
            ok "$u templates \$labels.$lbl from an unaggregated query"
        elif printf '%s' "$EXPRS" | grep -qE "by \([^)]*\<$lbl\>"; then
            ok "$u keeps '$lbl' through its aggregation"
        else
            bad "$u templates \$labels.$lbl but its aggregation drops that label, so the notification would name nothing"
        fi
    done
done

# --- 7. noDataState: OK must mean "empty is healthy" -----------------------
# OK on a rule whose series should always exist is how a dead pipeline reads as
# green. The container rules are the legitimate case (a stopped container must
# not raise a resource warning, and ServiceDown covers cAdvisor vanishing); the
# absence rule is the other (its absent() query returning nothing IS healthy).
echo "noDataState: OK is only on rules where an empty result is genuinely healthy"
for u in $UIDS; do
    ND=$(rule_json "$u" | jq -r '.noDataState')
    [ "$ND" = "OK" ] || continue
    if rule_json "$u" | jq -e '.data[].model.expr? // "" | test("absent\\(")' >/dev/null; then
        ok "$u is absent()-based, so an empty result is the healthy case"
    elif rule_block "$u" | grep -q '^ *# nodata-ok:'; then
        # Written escape hatch: a rule whose series should exist, where something
        # else already pages for the series vanishing. The reason has to be in
        # the file, next to the setting it justifies.
        ok "$u carries a written '# nodata-ok:' justification"
    elif printf '%s' "$(rule_exprs "$u")" | grep -qE 'container_(cpu|memory)_'; then
        ok "$u is a container resource rule: a stopped container must not warn, and ServiceDown covers cAdvisor"
    elif printf '%s' "$(rule_exprs "$u")" | grep -qE 'increase\(|rate\('; then
        ok "$u counts failures over a window, so no series means none happened"
    else
        bad "$u has noDataState: OK on a query whose series should always exist — an empty result would read as healthy. Use Alerting, or add the reason here"
    fi
done

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "OK: every rule can reach its threshold, and every threshold means what its text says"
else
    echo "$FAILURES assertion(s) failed"
    exit 1
fi
