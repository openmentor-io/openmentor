#!/bin/bash
set -euo pipefail

# migrate-mentors.sh — one-command runner for the getmentor.dev ->
# openmentor.io mentor migration (see migrate-mentors.js for what it does).
#
#   ./migrate-mentors.sh --slug ivan-petrov-42 --dry-run
#   ./migrate-mentors.sh --csv slugs.csv
#
# All arguments are forwarded to migrate-mentors.js. This wrapper:
#   1. reads VM ssh access, POSTGRES_PASSWORD and WORKER_AUTH_TOKEN from
#      ../.env.production (same file db.sh and deploy.sh use)
#   2. opens an SSH tunnel to the production Postgres container
#   3. runs the Node script with TARGET_DATABASE_URL pointing at the tunnel
#      (plus migration/.env for SOURCE_* and S3 settings, see README.md)
#   4. closes the tunnel on exit
#
# Prereqs: the worker image with the profile-migrated template must already
# be deployed (infra/deploy.sh backend), and `npm install` must have been
# run in this directory.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env.production"
[ -f "$ENV_FILE" ] || { echo "❌ $ENV_FILE not found"; exit 1; }

env_get() { grep "^$1=" "$ENV_FILE" | head -1 | cut -d= -f2-; }

VM_SSH_HOST=$(env_get VM_SSH_HOST)
VM_SSH_USER=$(env_get VM_SSH_USER)
VM_SSH_KEY_FILE=$(env_get VM_SSH_KEY_FILE || true)
POSTGRES_PASSWORD=$(env_get POSTGRES_PASSWORD)
WORKER_AUTH_TOKEN=$(env_get WORKER_AUTH_TOKEN)

[ -n "$VM_SSH_HOST" ] && [ -n "$VM_SSH_USER" ] || { echo "❌ VM_SSH_HOST/VM_SSH_USER missing in .env.production"; exit 1; }
[ -n "$POSTGRES_PASSWORD" ] || { echo "❌ POSTGRES_PASSWORD missing in .env.production"; exit 1; }

SSH_OPTS=(-o StrictHostKeyChecking=no)
[ -n "${VM_SSH_KEY_FILE:-}" ] && SSH_OPTS=(-i "$VM_SSH_KEY_FILE" "${SSH_OPTS[@]}")
VM="$VM_SSH_USER@$VM_SSH_HOST"

TUNNEL_PORT="${MIGRATE_TUNNEL_PORT:-5434}"

# Resolve the postgres container's docker-network IP (it publishes no host
# port) — same mechanics as db.sh tunnel.
PG_IP=$(ssh "${SSH_OPTS[@]}" "$VM" \
    "docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' openmentor-postgres")
[ -n "$PG_IP" ] || { echo "❌ could not resolve the postgres container IP"; exit 1; }

echo "🔌 Opening DB tunnel: localhost:$TUNNEL_PORT → openmentor-postgres ($PG_IP:5432)"
ssh "${SSH_OPTS[@]}" -N -L "$TUNNEL_PORT:$PG_IP:5432" "$VM" &
TUNNEL_PID=$!
trap 'kill "$TUNNEL_PID" 2>/dev/null || true' EXIT

# Wait for the tunnel to accept connections
for _ in $(seq 1 20); do
    if nc -z localhost "$TUNNEL_PORT" 2>/dev/null; then break; fi
    sleep 0.5
done
nc -z localhost "$TUNNEL_PORT" 2>/dev/null || { echo "❌ tunnel did not come up on port $TUNNEL_PORT"; exit 1; }

export TARGET_DATABASE_URL="postgres://openmentor@localhost:$TUNNEL_PORT/openmentor"
export PGPASSWORD="$POSTGRES_PASSWORD"
export VM_SSH_HOST VM_SSH_USER VM_SSH_KEY_FILE WORKER_AUTH_TOKEN

cd "$SCRIPT_DIR"
NODE_ARGS=()
[ -f "$SCRIPT_DIR/.env" ] && NODE_ARGS+=(--env-file="$SCRIPT_DIR/.env")

node "${NODE_ARGS[@]}" migrate-mentors.js "$@"
