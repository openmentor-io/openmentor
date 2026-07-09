#!/bin/bash
# Convert Grafana alert rules JSON to Prometheus YAML format
# Usage: ./scripts/json-to-prometheus-yaml.sh dist/alerts/alerts.json dist/alerts/prometheus-rules.yaml

set -e

INPUT_FILE="${1:-dist/alerts/alerts.json}"
OUTPUT_FILE="${2:-dist/alerts/prometheus-rules.yaml}"

if [ ! -f "$INPUT_FILE" ]; then
    echo "Error: Input file not found: $INPUT_FILE"
    exit 1
fi

# Check for jq
if ! command -v jq &> /dev/null; then
    echo "Error: jq is required but not installed."
    echo "Install with: brew install jq"
    exit 1
fi

# Check for yq
if ! command -v yq &> /dev/null; then
    echo "Error: yq is required but not installed."
    echo "Install with: brew install yq"
    exit 1
fi

echo "Converting $INPUT_FILE to $OUTPUT_FILE..."

# Convert JSON to Prometheus YAML format using jq
jq '
{
  groups: [
    .groups[] | {
      name: .name,
      interval: .interval,
      rules: [
        .rules[] | {
          alert: (.title | gsub(" "; "") | gsub("[^a-zA-Z0-9]"; "")),
          expr: (.data[0].model.expr | gsub("\n"; " ") | gsub("\\s+"; " ") | ltrimstr(" ") | rtrimstr(" ")),
          "for": .["for"],
          labels: .labels,
          annotations: (.annotations | del(.runbook_url) | with_entries(select(.value != "")))
        }
      ]
    }
  ]
}' "$INPUT_FILE" | yq -P '.' > "$OUTPUT_FILE"

# Add header comment
HEADER="# OpenMentor Prometheus Alert Rules
# Auto-generated from alerts.jsonnet - DO NOT EDIT MANUALLY
# Import via Grafana UI: Alerting > Alert rules > Import > Prometheus YAML file
#
# Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
"

# Prepend header to file
echo "$HEADER" | cat - "$OUTPUT_FILE" > "$OUTPUT_FILE.tmp" && mv "$OUTPUT_FILE.tmp" "$OUTPUT_FILE"

echo "Done! Created $OUTPUT_FILE"
