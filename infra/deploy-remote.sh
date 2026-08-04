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
#   2  deploy converged and every application health check passed, but the
#      postgres-backup sidecar reports UNHEALTHY (stale dumps). Nothing was
#      rolled back — reverting working images cannot make a pg_dump run — yet
#      the run is not reported green either. See the P8 block below.
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

# SECURITY (P10): .env.runtime is gone. It was a second full copy of every
# production secret on disk, handed wholesale to six containers via compose
# `env_file`. Services now declare explicit `environment:` allowlists in
# docker-compose.yml (enforced by check-service-env.sh) and .env alone drives
# compose interpolation. Image tags still never reach container env, so a
# tag-only deploy still recreates only the retagged service.
#
# The block below is byte-identical to the one in rollback.sh and is extracted
# from both by deploy-transition-test.sh; keep them in sync.
# --- P10 .env.runtime transition (mirrored in deploy-remote.sh + rollback.sh) --
# NOTE: rollback.sh embeds this block in an UNQUOTED here-document, so it must
# stay free of backticks and shell variables. deploy-transition-test.sh checks.
#
# Whether .env.runtime may be deleted depends on the compose file THIS VM has,
# not on the one in the checkout being deployed: the pre-P10 file gives six
# services an "env_file: .env.runtime" entry, compose defaults
# env_file.required to true, and a default "./deploy.sh" (frontend backend)
# does not sync infra/. Deleting it first would therefore abort the pull/up
# halfway on a VM that is still one deploy behind. Regenerate it while it is
# still referenced; it is removed by the first deploy that carries the new
# compose file.
sync_env_runtime() {
    if grep -qE '^[[:space:]]*(- |env_file:[[:space:]]*)\.env\.runtime' docker-compose.yml; then
        # .env minus the image-tag lines, so a tag-only deploy still changes
        # only the retagged service's compose config.
        grep -vE '^(FRONTEND_IMAGE_TAG|BACKEND_IMAGE_TAG)=' .env > .env.runtime
        chmod 600 .env.runtime
        echo "⚠️  This VM still runs the pre-P10 docker-compose.yml (env_file: .env.runtime)."
        echo "   Regenerated it so this deploy converges. Finish the upgrade with"
        echo "   './deploy.sh infra' (or 'all') to ship the per-service allowlists;"
        echo "   that deploy is the one that deletes the shared secret file."
    else
        rm -f .env.runtime
    fi
}
sync_env_runtime
# --- end P10 .env.runtime transition ----------------------------------------

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

# The block below is byte-identical to the one in rollback.sh (modulo that
# file's heredoc escaping) and is extracted from both by
# deploy-transition-test.sh; keep them in sync.
# --- P8 backup sidecar health gate (mirrored in deploy-remote.sh + rollback.sh) --
# NOTE: rollback.sh embeds this block in an UNQUOTED here-document, so every
# expansion is backslash-escaped there. deploy-transition-test.sh pushes that
# copy through a heredoc and requires the result to equal this one byte for
# byte, so keep the block free of backticks and of any other backslash.
#
# NOTE: if/elif, not case, and every paren in CODE balanced. bash 3.2 — still
# /bin/bash on macOS, where rollback.sh is run — finds the end of the $( ) around
# its here-document by counting parens in the body, so one unbalanced ")" from a
# case pattern closes the substitution early and rollback.sh stops parsing
# entirely. Comments are exempt (bash skips them); code is not.
#
# WHY not .State.Status alone, which is all this check used to read: docker keeps
# an UNHEALTHY container in state "running", so that test passed for a sidecar
# whose nightly dump had silently stopped — the exact silent failure P8 exists to
# remove. .State.Health carries the compose healthcheck's verdict.
#
# Prints its own verdict and returns:
#   0  healthy, still inside start_period, or no healthcheck state to read
#   1  not running — "restarting", "exited", or no such container
#   2  running but UNHEALTHY, i.e. the dumps are stale
# Callers keep 2 out of the auto-rollback path on purpose (see the call site).
check_backup_sidecar() {
    backup_status=$(docker inspect -f '{{.State.Status}}' openmentor-postgres-backup 2>/dev/null || true)
    if [ "$backup_status" != "running" ]; then
        echo "❌ Postgres-backup health check FAILED (container status: ${backup_status:-<absent>})"
        return 1
    fi
    # The {{if}} guard is not cosmetic: .State.Health is absent on a container
    # with no healthcheck, and the bare .State.Health.Status template errors out
    # there rather than printing an empty string.
    backup_health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' openmentor-postgres-backup 2>/dev/null || true)
    if [ "$backup_health" = "healthy" ]; then
        echo "✅ Postgres-backup health check passed (running, healthy)"
    elif [ "$backup_health" = "starting" ]; then
        # A just-recreated sidecar is legitimately "starting" for the whole
        # start_period; failing here would make every sidecar rebuild look like a
        # backup outage.
        echo "✅ Postgres-backup health check passed (running, healthcheck still in start_period)"
    elif [ "$backup_health" = "unhealthy" ]; then
        echo "❌ Postgres-backup is UNHEALTHY: the newest successful pg_dump is past"
        echo "   BACKUP_MAX_AGE_HOURS, or no dump has ever succeeded on this volume."
        echo "   The documented 24h RPO is not being met. Look for a FAILURE line in"
        echo "   'docker logs openmentor-postgres-backup' and follow"
        echo "   docs/runbooks/postgres-backup-restore.md."
        return 2
    else
        # Absent .State.Health: a VM one deploy behind, whose compose file or
        # sidecar image predates the healthcheck. Absence must not hard-fail, or
        # this check would break the very transition that ships it.
        echo "⚠️  Postgres-backup is running but reports no healthcheck state"
        echo "   (.State.Health absent — this VM's compose file or sidecar image"
        echo "   predates it). Backup freshness is UNVERIFIED by this deploy;"
        echo "   './deploy.sh infra' ships the healthcheck. Not failing the gate."
    fi
    return 0
}
# --- end P8 backup sidecar health gate --------------------------------------

# Verify health checks
echo "🏥 Checking health endpoints..."
HEALTH_CHECK_FAILED=0
# Tracked apart from HEALTH_CHECK_FAILED because it must NOT trigger the
# auto-rollback below; see the call site.
BACKUP_UNHEALTHY=0

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

# Check backup sidecar. It has no HTTP endpoint; its compose healthcheck
# (backup.sh healthcheck) is the signal, and the two failure modes get different
# treatment.
BACKUP_RC=0
check_backup_sidecar || BACKUP_RC=$?
if [ "$BACKUP_RC" -eq 1 ]; then
    # Not running at all can be THIS deploy's fault (a broken sidecar rebuild),
    # so it keeps the pre-existing behaviour: roll back.
    HEALTH_CHECK_FAILED=1
elif [ "$BACKUP_RC" -eq 2 ]; then
    # Stale dumps are not a reason to revert working application images — a
    # rollback cannot make a pg_dump run, and reverting halfway through a live
    # deploy is worse than the problem. But they must not be reported green
    # either, which is exactly what the .State.Status-only test did. Recorded
    # here and turned into exit 2 after convergence.
    BACKUP_UNHEALTHY=1
fi

# Rollback if health checks failed
if [ $HEALTH_CHECK_FAILED -eq 1 ]; then
    echo "🔄 ROLLING BACK to previous version..."

    # Restore backup .env file (previous image tags, written by the caller
    # before it touched .env)
    if [ -f .env.backup ]; then
        cp .env.backup .env
        # Re-derive the legacy file from the restored .env on a not-yet-upgraded VM
        sync_env_runtime
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

# The deploy itself is done. A stale backup sidecar still has to leave this run
# non-zero — with its own code, so the caller can say which of the two happened
# instead of printing "deployment failed" over a deploy that worked.
if [ "$BACKUP_UNHEALTHY" -eq 1 ]; then
    echo "✅ Deployment complete — services converged and every app health check passed"
    echo "❌ ...but the postgres-backup sidecar is UNHEALTHY (see above). Exiting 2 so"
    echo "   this run is not reported green. Nothing was rolled back: the code that is"
    echo "   running is fine, the backups are not."
    exit 2
fi

echo "✅ Deployment complete!"
exit 0
