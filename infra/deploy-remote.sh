#!/bin/bash
set -e

# ============================================================================
# OpenMentor Remote Deployment Script (runs ON the production VM)
# ============================================================================
# The single canonical source of the remote deploy logic, consumed by BOTH
# deploy paths — edit it once, both pick it up:
#
#   • infra/deploy.sh (step 8):
#       ssh ... "bash -s" -- "$UP_FLAGS" "$RESTART_ALLOY" \
#           "$REBUILD_BACKUP_SIDECAR" < infra/deploy-remote.sh
#   • .github/workflows/deploy.yml ("Deploy on VM" step): same invocation.
#
# Both callers pipe their LOCAL checked-out copy over ssh stdin — execution
# never depends on the rsynced copy on the VM being fresh (app-only deploys
# don't sync infra/).
#
# Positional arguments:
#   $1  UP_FLAGS                extra flags for `docker compose up -d`
#                               ("" or "--remove-orphans")
#   $2  RESTART_ALLOY           "1" → restart alloy (its bind-mounted
#                               config.alloy changed in the sync)
#   $3  REBUILD_BACKUP_SIDECAR  "1" → rebuild the postgres-backup image
#                               (its build context changed in the sync)
#
# Preconditions (arranged by the caller BEFORE piping this script):
#   • /opt/openmentor/infra/.env contains the desired FRONTEND_IMAGE_TAG /
#     BACKEND_IMAGE_TAG (deploy.sh uploads a fresh .env; the workflow edits
#     the tag lines of the existing one in place),
#   • .env.backup holds the PREVIOUS .env (previous image tags) — the
#     auto-rollback below restores it if health checks fail,
#   • the VM's docker is already logged in to ECR (short-lived token minted
#     on the calling machine and piped over ssh stdin — the VM needs no aws
#     CLI and no AWS credentials at all; tokens last 12h, each deploy
#     re-authenticates).
#
# Exit codes:
#   0  deploy converged and all health checks passed
#   1  deploy failed (auto-rollback to .env.backup attempted when health
#      checks fail; its outcome is logged)
# ============================================================================

UP_FLAGS="$1"
RESTART_ALLOY="$2"
REBUILD_BACKUP_SIDECAR="$3"

echo "🚀 Starting deployment on production VM..."

# The monorepo's infra/ directory is synced to /opt/openmentor/infra (by the
# infra target of deploy.sh or the deploy workflow); compose runs from there.
cd /opt/openmentor/infra

# Read image tags from the prepared .env file
FRONTEND_IMAGE_TAG=$(grep "^FRONTEND_IMAGE_TAG=" .env | cut -d'=' -f2)
BACKEND_IMAGE_TAG=$(grep "^BACKEND_IMAGE_TAG=" .env | cut -d'=' -f2)
echo "Deploying with:"
echo "  • Frontend image tag: $FRONTEND_IMAGE_TAG"
echo "  • Backend image tag: $BACKEND_IMAGE_TAG"

# Regenerate .env.runtime: what the containers read via compose `env_file`.
# It is .env WITHOUT the image-tag lines, so a tag-only deploy changes only
# the retagged service's compose config (convergence recreates nothing else).
regen_env_runtime() {
    grep -vE '^(FRONTEND_IMAGE_TAG|BACKEND_IMAGE_TAG)=' .env > .env.runtime
    chmod 600 .env.runtime
}
regen_env_runtime

# Ensure the Postgres data volume exists (idempotent). It is declared
# `external` in docker-compose.yml so `docker compose down -v` can never
# delete the production database. Postgres image pin bumps are safe for the
# same reason: the container is recreated, the data volume persists (minor
# versions only — major upgrades follow docs/runbooks/postgres-backup-restore.md).
echo "🗄️  Ensuring Postgres data volume exists..."
docker volume create openmentor-postgres-data

# Rebuild the backup sidecar image if its build context changed in the sync
# (it is BUILT on the VM from ./postgres-backup — `up -d` alone would keep
# running the stale image)
if [ "$REBUILD_BACKUP_SIDECAR" = "1" ] || ! docker image inspect openmentor-postgres-backup:local >/dev/null 2>&1; then
    echo "🔨 postgres-backup/ changed — rebuilding sidecar image..."
    docker compose build postgres-backup
fi

# Pull new images
echo "📦 Pulling new images..."
# --ignore-buildable: postgres-backup is built on the VM, not pulled
docker compose pull --ignore-buildable

# Converge: compose recreates only services whose image/definition changed
echo "🔄 Converging services (docker compose up -d $UP_FLAGS)..."
# shellcheck disable=SC2086 # UP_FLAGS is intentionally word-split ("" or "--remove-orphans")
docker compose up -d $UP_FLAGS

# Post-up guard: verify every running project container is attached to the
# compose network. Docker can (rarely - seen with a port conflict during a
# delayed image-pull start) bring a container up with no network endpoint;
# in-container healthchecks still pass while inter-service DNS fails.
# Self-heal once with a force-recreate.
for svc in $(docker compose ps --services 2>/dev/null); do
    cid=$(docker compose ps -q "$svc" 2>/dev/null | head -1)
    [ -n "$cid" ] || continue
    running=$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null)
    [ "$running" = "true" ] || continue
    nets=$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$cid")
    if [ -z "${nets// /}" ]; then
        echo "⚠️  '$svc' is running but detached from the network - force-recreating..."
        docker compose up -d --force-recreate "$svc"
        nets=$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$(docker compose ps -q "$svc" | head -1)")
        if [ -z "${nets// /}" ]; then
            echo "❌ '$svc' still has no network after recreate - aborting."
            exit 1
        fi
        echo "   '$svc' reattached ($nets)"
    fi
done

# Bind-mount trap: compose does NOT react to changes in bind-mounted config
# files. Restart exactly the services whose file config changed in the sync.
if [ "$RESTART_ALLOY" = "1" ]; then
    echo "↻ alloy/config.alloy changed — restarting alloy..."
    docker compose restart alloy
fi

# Wait for containers to start
echo "⏳ Waiting for containers to start..."
sleep 20

# Check service status
echo "📊 Service status:"
docker compose ps

# Verify health checks
echo "🏥 Checking health endpoints..."
HEALTH_CHECK_FAILED=0

# Check frontend health
if ! docker exec openmentor-frontend curl -f http://localhost:3000/api/healthcheck 2>/dev/null; then
    echo "❌ Frontend health check FAILED"
    HEALTH_CHECK_FAILED=1
else
    echo "✅ Frontend health check passed"
fi

# Check backend health
if ! docker exec openmentor-backend curl -f http://localhost:8081/api/healthcheck 2>/dev/null; then
    echo "❌ Backend health check FAILED"
    HEALTH_CHECK_FAILED=1
else
    echo "✅ Backend health check passed"
fi

# Check worker health
if ! docker exec openmentor-worker curl -f http://localhost:8090/healthz 2>/dev/null; then
    echo "❌ Worker health check FAILED"
    HEALTH_CHECK_FAILED=1
else
    echo "✅ Worker health check passed"
fi

# Check postgres health (credentials from the prepared .env)
POSTGRES_USER_ENV=$(grep "^POSTGRES_USER=" .env | cut -d'=' -f2)
POSTGRES_DB_ENV=$(grep "^POSTGRES_DB=" .env | cut -d'=' -f2)
if ! docker exec openmentor-postgres pg_isready -U "${POSTGRES_USER_ENV:-openmentor}" -d "${POSTGRES_DB_ENV:-openmentor}" 2>/dev/null; then
    echo "❌ Postgres health check FAILED"
    HEALTH_CHECK_FAILED=1
else
    echo "✅ Postgres health check passed"
fi

# Check backup sidecar (no HTTP endpoint - it must simply be running)
if [ "$(docker inspect -f '{{.State.Status}}' openmentor-postgres-backup 2>/dev/null)" != "running" ]; then
    echo "❌ Postgres-backup health check FAILED (container not running)"
    HEALTH_CHECK_FAILED=1
else
    echo "✅ Postgres-backup health check passed"
fi

# Rollback if health checks failed
if [ $HEALTH_CHECK_FAILED -eq 1 ]; then
    echo "🔄 ROLLING BACK to previous version..."

    # Restore backup .env file (previous image tags, written by the caller
    # before it touched .env)
    if [ -f .env.backup ]; then
        cp .env.backup .env
        regen_env_runtime
        echo "Restored previous .env file"
    else
        echo "❌ No backup .env file found, cannot rollback!"
        exit 1
    fi

    # --ignore-buildable: postgres-backup is built on the VM, not pulled
    docker compose pull --ignore-buildable
    docker compose up -d
    sleep 10

    # Verify rollback succeeded
    if docker exec openmentor-frontend curl -f http://localhost:3000/api/healthcheck 2>/dev/null && \
       docker exec openmentor-backend curl -f http://localhost:8081/api/healthcheck 2>/dev/null && \
       docker exec openmentor-worker curl -f http://localhost:8090/healthz 2>/dev/null && \
       docker exec openmentor-postgres pg_isready -U "${POSTGRES_USER_ENV:-openmentor}" -d "${POSTGRES_DB_ENV:-openmentor}" 2>/dev/null; then
        echo "✅ Rollback successful"
    else
        echo "❌ Rollback FAILED - manual intervention required!"
    fi

    exit 1
fi

echo "✅ Deployment complete!"
exit 0
