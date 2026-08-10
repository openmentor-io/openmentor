#!/usr/bin/env bash
# ============================================================================
# Metric keep-list consistency (D86)
# ============================================================================
# The prometheus.relabel components in alloy/config.alloy decide which series
# reach Grafana Cloud, and the dashboards/alerts/SLOs in ../grafana decide
# which series get read. Nothing else ties the two together: `alloy validate`
# proves the config parses, not that a panel's series survives the allowlist —
# which is exactly how the Host Memory panel briefly lost machine_memory_bytes.
#
# Cases:
#   1. every container_*/machine_* name a grafana/ expression reads matches
#      the cadvisor keep regex, anchored, the way relabel matches it
#   2. the cadvisor drop rule keeps the root-cgroup (id="/") and named-container
#      series and drops the unnamed cgroup slices — representative label tuples
#      joined with ";" exactly as prometheus.relabel joins source_labels
#   3. the scrape meta series survive both keep-lists (`up` feeds ServiceDown
#      for the cadvisor and alloy targets), and known-noisy families do not
#   4. every pg_stat_statements_* name a grafana/ expression reads survives the
#      postgres drop rule, and the rule still bites on rows_total
#   5. self-test: every regex this file extracts is non-empty, so renaming a
#      component fails this test instead of emptying it
#
# The regexes under test are EXTRACTED from config.alloy, never restated here;
# evaluation is an anchored ERE over the joined source labels, which is the
# defined relabel semantics. Editing a rule out from under a consumer therefore
# fails this test, not a dashboard in production.
#
# Usage: ./metrics-keeplist-test.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

ALLOY=alloy/config.alloy
GRAFANA=../grafana

fail=0
ok()  { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; fail=1; }

# Anchored full match, as relabel applies a rule's regex.
matches() { printf '%s' "$2" | grep -qE "^($1)\$"; }

# relabel_regex <component> <action>: the regex of the first rule carrying that
# action inside prometheus.relabel "<component>".
relabel_regex() {
  awk -v comp="$1" -v act="$2" '
    $0 ~ "^prometheus\\.relabel \"" comp "\" \\{" { inblock = 1 }
    inblock && /^\}/ { exit }
    !inblock { next }
    /^[ \t]*rule \{/ { r = "" }
    /^[ \t]*regex[ \t]*=/ {
      line = $0
      sub(/^[ \t]*regex[ \t]*=[ \t]*"/, "", line)
      sub(/"[ \t]*$/, "", line)
      r = line
    }
    $0 ~ "action[ \t]*=[ \t]*\"" act "\"" && r != "" { print r; exit }
  ' "$ALLOY"
}

# Metric names of a given shape that appear in an EXPRESSION in grafana/ —
# dashboard "expr" fields, alert-rule model exprs, and the SLO queries.
# Deliberately not the whole files: descriptions and comments may name a
# metric without reading it.
referenced() {
  {
    grep -h '"expr"' "$GRAFANA"/dashboards/*.json
    grep -h 'expr' "$GRAFANA"/alerting/*.yaml
    cat "$GRAFANA"/slo/*.yaml
  } | grep -ohE "$1" | sort -u
}

CADVISOR_KEEP=$(relabel_regex cadvisor keep)
CADVISOR_DROP=$(relabel_regex cadvisor drop)
SELF_KEEP=$(relabel_regex alloy_self keep)
PG_DROP=$(relabel_regex postgres_openmentor drop)

echo "== extraction self-test: the components this file reasons about exist"
[ -n "$CADVISOR_KEEP" ] && ok 'cadvisor keep regex extracted' || bad 'cadvisor keep regex not found — component renamed or rule restructured'
[ -n "$CADVISOR_DROP" ] && ok 'cadvisor drop regex extracted' || bad 'cadvisor drop regex not found — component renamed or rule restructured'
[ -n "$SELF_KEEP" ]     && ok 'alloy_self keep regex extracted' || bad 'alloy_self keep regex not found — component renamed or rule restructured'
[ -n "$PG_DROP" ]       && ok 'postgres_openmentor drop regex extracted' || bad 'postgres_openmentor drop regex not found — component renamed or rule restructured'
[ "$fail" -eq 0 ] || { echo 'FAILED: extraction broke; every later case would be vacuous'; exit 1; }

echo "== case 1: every container_*/machine_* series grafana/ reads survives the cadvisor keep-list"
found_any=""
while IFS= read -r name; do
  [ -n "$name" ] || continue
  found_any=yes
  if matches "$CADVISOR_KEEP" "$name"; then
    ok "$name is kept"
  else
    bad "$name is read by grafana/ but dropped by the cadvisor keep-list"
  fi
done <<EOF
$(referenced '(container|machine)_[a-z_]+')
EOF
[ -n "$found_any" ] && ok 'expression scan found cadvisor-family metrics (scan is not vacuous)' || bad 'expression scan found NO container_*/machine_* names — the scan itself is broken'

echo "== case 2: the unnamed-cgroup drop rule spares the series grafana/ reads"
# Tuples are __name__;name;id, joined as prometheus.relabel joins source_labels.
for tuple in \
  'container_fs_usage_bytes;;/' \
  'container_memory_usage_bytes;;/' \
  'container_cpu_usage_seconds_total;openmentor-backend;/system.slice/docker-0123abc.scope' \
  'machine_memory_bytes;;' \
  'up;;'
do
  if matches "$CADVISOR_DROP" "$tuple"; then
    bad "drop rule discards '$tuple', which a dashboard or alert reads"
  else
    ok "drop rule spares '$tuple'"
  fi
done
for tuple in \
  'container_cpu_usage_seconds_total;;/system.slice' \
  'container_memory_working_set_bytes;;/system.slice/chronyd.service'
do
  if matches "$CADVISOR_DROP" "$tuple"; then
    ok "drop rule discards unnamed slice '$tuple'"
  else
    bad "drop rule no longer discards '$tuple' — the unnamed-cgroup filter stopped biting"
  fi
done

echo "== case 3: scrape meta survives both keep-lists, noisy families do not"
for name in up scrape_duration_seconds; do
  matches "$CADVISOR_KEEP" "$name" && ok "cadvisor keep-list keeps $name" || bad "cadvisor keep-list drops $name — ServiceDown loses the cadvisor target"
  matches "$SELF_KEEP" "$name"     && ok "alloy_self keep-list keeps $name" || bad "alloy_self keep-list drops $name — ServiceDown loses the alloy target"
done
for name in prometheus_remote_storage_samples_failed_total loki_write_dropped_entries_total; do
  matches "$SELF_KEEP" "$name" && ok "alloy_self keep-list keeps $name" || bad "alloy_self keep-list drops $name — a dead delivery pipeline becomes invisible"
done
for name in container_tasks_state container_last_seen container_network_receive_bytes_total; do
  matches "$CADVISOR_KEEP" "$name" && bad "cadvisor keep-list re-admits $name (nothing reads it)" || ok "cadvisor keep-list still drops $name"
done
for name in alloy_component_graph_connection otelcol_processor_batch_batch_send_size_bytes_bucket prometheus_target_interval_length_seconds; do
  matches "$SELF_KEEP" "$name" && bad "alloy_self keep-list re-admits $name (nothing reads it)" || ok "alloy_self keep-list still drops $name"
done

echo "== case 4: every pg_stat_statements_* series grafana/ reads survives the postgres drop rule"
found_any=""
while IFS= read -r name; do
  [ -n "$name" ] || continue
  found_any=yes
  if matches "$PG_DROP" "$name"; then
    bad "$name is read by grafana/ but dropped by the postgres rule"
  else
    ok "$name is kept"
  fi
done <<EOF
$(referenced 'pg_stat_statements_[a-z_]+')
EOF
[ -n "$found_any" ] && ok 'expression scan found pg_stat_statements metrics (scan is not vacuous)' || bad 'expression scan found NO pg_stat_statements names — the scan itself is broken'
matches "$PG_DROP" pg_stat_statements_rows_total && ok 'drop rule still bites on pg_stat_statements_rows_total' || bad 'drop rule no longer drops pg_stat_statements_rows_total — the filter is dead code'

if [ "$fail" -ne 0 ]; then
  echo 'FAILED: a keep-list and its consumers disagree — see FAIL lines above'
  exit 1
fi
echo 'OK: every series grafana/ reads survives the Alloy keep-lists, and the filters still bite'
