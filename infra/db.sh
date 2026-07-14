#!/bin/bash
set -euo pipefail

# db.sh — access the production Postgres (container on the VM, no public port).
#
#   ./db.sh                        interactive psql shell
#   ./db.sh -c "SELECT ..."        run one query (results to stdout)
#   ./db.sh < queries.sql          run a SQL file
#   ./db.sh tunnel [LOCAL_PORT]    SSH tunnel for GUI clients (default port 5433);
#                                  connect to localhost:<port>, db/user openmentor,
#                                  password = POSTGRES_PASSWORD in .env.production.
#                                  Ctrl-C to close.
#
# Reads VM_SSH_HOST/VM_SSH_USER (and optional VM_SSH_KEY_FILE) from
# .env.production next to this script. SSH-agent (1Password) friendly.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/.env.production"
[ -f "$ENV_FILE" ] || { echo "❌ $ENV_FILE not found"; exit 1; }

VM_SSH_HOST=$(grep '^VM_SSH_HOST=' "$ENV_FILE" | cut -d= -f2)
VM_SSH_USER=$(grep '^VM_SSH_USER=' "$ENV_FILE" | cut -d= -f2)
VM_SSH_KEY_FILE=$(grep '^VM_SSH_KEY_FILE=' "$ENV_FILE" | cut -d= -f2- || true)
[ -n "$VM_SSH_HOST" ] && [ -n "$VM_SSH_USER" ] || { echo "❌ VM_SSH_HOST/VM_SSH_USER missing in .env.production"; exit 1; }

SSH_OPTS=(-o StrictHostKeyChecking=no)
[ -n "${VM_SSH_KEY_FILE:-}" ] && SSH_OPTS=(-i "$VM_SSH_KEY_FILE" "${SSH_OPTS[@]}")
VM="$VM_SSH_USER@$VM_SSH_HOST"
PSQL="docker exec -i openmentor-postgres psql -U openmentor openmentor"

case "${1:-shell}" in
    tunnel)
        LOCAL_PORT="${2:-5433}"
        # The container publishes no host port, so tunnel to its docker-network
        # IP (resolved fresh each time; stable while the container runs).
        PG_IP=$(ssh "${SSH_OPTS[@]}" "$VM" \
            "docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' openmentor-postgres")
        [ -n "$PG_IP" ] || { echo "❌ could not resolve the postgres container IP"; exit 1; }
        echo "🔌 Tunnel: localhost:$LOCAL_PORT → openmentor-postgres ($PG_IP:5432)"
        echo "   DSN: postgres://openmentor:<POSTGRES_PASSWORD from .env.production>@localhost:$LOCAL_PORT/openmentor"
        echo "   Ctrl-C to close."
        exec ssh "${SSH_OPTS[@]}" -N -L "$LOCAL_PORT:$PG_IP:5432" "$VM"
        ;;
    -c)
        [ -n "${2:-}" ] || { echo "❌ -c needs a query"; exit 1; }
        exec ssh "${SSH_OPTS[@]}" "$VM" "$PSQL -c $(printf '%q' "$2")"
        ;;
    shell)
        if [ -t 0 ]; then
            # interactive: allocate a TTY end-to-end
            exec ssh -t "${SSH_OPTS[@]}" "$VM" "${PSQL/exec -i/exec -it}"
        else
            # stdin is a file/pipe: stream it into psql
            exec ssh "${SSH_OPTS[@]}" "$VM" "$PSQL"
        fi
        ;;
    -h|--help) sed -n '4,14p' "$0"; exit 0 ;;
    *) echo "❌ unknown argument: $1 (see --help)"; exit 1 ;;
esac
