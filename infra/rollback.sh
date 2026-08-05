#!/bin/bash
set -e

# ============================================================================
# OpenMentor Production Rollback Script
# ============================================================================
# Quickly roll production back to previously deployed image tags. Tags are
# per-service (FRONTEND_IMAGE_TAG / BACKEND_IMAGE_TAG in the VM's .env);
# the backend tag covers backend + worker + migrate (one image).
#
#   ./rollback.sh <tag>                          # roll BOTH images to <tag>
#   ./rollback.sh --frontend <tag>               # roll only the frontend
#   ./rollback.sh --backend <tag>                # roll only backend/worker/migrate
#   ./rollback.sh --frontend <t1> --backend <t2> # independent tags
#
# Options:
#   --yes, -y    skip the confirmation prompt
#
# The script edits the tags in /opt/openmentor/infra/.env on the VM (snapshotting
# the previous one as .env.backup.<epoch>), pulls, re-converges with
# `docker compose up -d` and runs the same health checks as deploy.sh. It holds
# the same .deploy.lock a deploy holds, and promotes .env.lastgood when the
# rolled-back version passes those checks — see the header of deploy-remote.sh.
# ============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE_INFRA_DIR="/opt/openmentor/infra"

usage() {
    echo "Usage: $0 [<tag>] [--frontend <tag>] [--backend <tag>] [--yes]"
    echo ""
    echo "  <tag>               Roll BOTH frontend and backend to <tag>"
    echo "  --frontend <tag>    Roll only the frontend image"
    echo "  --backend <tag>     Roll only the backend image (backend/worker/migrate)"
    echo "  --yes, -y           Skip the confirmation prompt"
    echo ""
    echo "Services without a tag argument keep their current tag."
}

# Load production environment (same file deploy.sh uses)
if [ ! -f "$SCRIPT_DIR/.env.production" ]; then
    echo -e "${RED}❌ Error: .env.production file not found${NC}"
    exit 1
fi

# shellcheck source=/dev/null # operator-supplied, never committed
source "$SCRIPT_DIR/.env.production"

# Validate required variables
if [ -z "$VM_SSH_HOST" ] || [ -z "$VM_SSH_USER" ]; then
    echo -e "${RED}❌ Error: Missing required variables in .env.production${NC}"
    exit 1
fi

# --------------------------------------------------------------------------
# Parse arguments
# --------------------------------------------------------------------------
FRONTEND_TARGET_TAG=""
BACKEND_TARGET_TAG=""
SKIP_CONFIRM=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --frontend)
            if [ -z "$2" ]; then echo -e "${RED}❌ --frontend requires a tag${NC}"; exit 1; fi
            FRONTEND_TARGET_TAG="$2"; shift 2 ;;
        --backend)
            if [ -z "$2" ]; then echo -e "${RED}❌ --backend requires a tag${NC}"; exit 1; fi
            BACKEND_TARGET_TAG="$2"; shift 2 ;;
        --yes|-y)
            SKIP_CONFIRM=true; shift ;;
        -h|--help)
            usage; exit 0 ;;
        -*)
            echo -e "${RED}Unknown option: $1${NC}"; usage; exit 1 ;;
        *)
            # Positional tag: applies to both services
            FRONTEND_TARGET_TAG="$1"
            BACKEND_TARGET_TAG="$1"
            shift ;;
    esac
done

echo -e "${YELLOW}🔄 OpenMentor Production Rollback${NC}"
echo "================================"
echo ""

# Interactive fallback when no tag was given at all
if [ -z "$FRONTEND_TARGET_TAG" ] && [ -z "$BACKEND_TARGET_TAG" ]; then
    read -rp "$(echo -e "${BLUE}Enter image tag to rollback BOTH services to (commit SHA):${NC}")" TARGET_TAG
    echo ""
    if [ -z "$TARGET_TAG" ]; then
        echo -e "${RED}❌ Error: No target tag specified${NC}"
        exit 1
    fi
    FRONTEND_TARGET_TAG="$TARGET_TAG"
    BACKEND_TARGET_TAG="$TARGET_TAG"
fi

echo "Rollback plan:"
echo "  • Frontend tag: ${FRONTEND_TARGET_TAG:-<keep current>}"
echo "  • Backend tag:  ${BACKEND_TARGET_TAG:-<keep current>} (backend + worker + migrate)"
echo "VM: $VM_SSH_USER@$VM_SSH_HOST ($REMOTE_INFRA_DIR)"
echo ""

# Confirmation
if [ "$SKIP_CONFIRM" = false ]; then
    read -rp "$(echo -e "${RED}⚠️ Are you sure you want to rollback? (yes/no):${NC}")"
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        echo -e "${YELLOW}Rollback cancelled${NC}"
        exit 0
    fi
fi

echo -e "${BLUE}🔄 Executing rollback...${NC}"

# Create rollback script
ROLLBACK_SCRIPT=$(cat <<REMOTE_SCRIPT
#!/bin/bash
set -e

FRONTEND_TARGET_TAG="$FRONTEND_TARGET_TAG"
BACKEND_TARGET_TAG="$BACKEND_TARGET_TAG"

# The monorepo's infra/ directory lives at $REMOTE_INFRA_DIR on the VM
cd $REMOTE_INFRA_DIR

# The block below is byte-identical to the one in deploy-remote.sh (modulo this
# file's heredoc escaping) and is extracted from both by
# deploy-transition-test.sh; keep them in sync.
# --- H9 deploy serialization (mirrored in deploy-remote.sh + rollback.sh) ----
# NOTE: rollback.sh embeds this block in an UNQUOTED here-document, so every
# expansion is backslash-escaped there. deploy-transition-test.sh pushes that
# copy through a heredoc and requires the result to equal this one byte for
# byte, so keep the block free of backticks and of any other backslash.
#
# NOTE: if/elif, not case, and every paren in CODE balanced — bash 3.2 (still
# /bin/bash on macOS, where rollback.sh runs) finds the end of the \$( ) around
# its here-document by counting parens in the body.
#
# WHY: three writers converge the same compose project and rewrite the same
# .env — this script (driven by CI and by deploy.sh) and rollback.sh. The
# deploy workflow's concurrency group serializes CI against itself only; an
# operator running deploy.sh or rollback.sh from a workstation is invisible to
# it. Two overlapping runs interleaved a pull/up with the other's .env edit,
# and each left the other's UNVERIFIED tags as the rollback target.
#
# The lock is fd 9 on a file in this directory, released whenever the shell
# exits — so a killed deploy cannot wedge the next one. A missing flock is fatal
# on purpose: warn-and-continue would silently restore the unserialized
# behaviour this exists to remove.
acquire_deploy_lock() {
    if ! command -v flock >/dev/null 2>&1; then
        echo "❌ flock (util-linux) is not installed on this VM — refusing to deploy unserialized."
        exit 1
    fi
    exec 9>>.deploy.lock
    chmod 600 .deploy.lock 2>/dev/null || true
    if ! flock -w "\${DEPLOY_LOCK_WAIT:-900}" 9; then
        echo "❌ Timed out waiting for the deploy lock (.deploy.lock in this directory):"
        echo "   another deploy or rollback is converging this VM. Nothing was changed."
        exit 1
    fi
    echo "🔒 Holding the deploy lock"
}

# Snapshot .env before changing it: timestamped, never a single slot, so no
# writer can destroy another's. Pruned to the 5 newest — each file is a copy of
# every production secret.
snapshot_env() {
    if [ -f .env ]; then
        snapshot=".env.backup.\$(date +%s)"
        cp .env "\$snapshot"
        chmod 600 "\$snapshot"
        # shellcheck disable=SC2012 # fixed prefix, no user-supplied names
        ls -1t .env.backup.* 2>/dev/null | tail -n +6 | xargs -r rm -f
        echo "   Snapshotted the current .env as \$snapshot"
    fi
}

# Promote the running .env to the verified rollback target. Called ONLY after
# every application health check has passed — that is the whole point: a tag
# that was merely attempted must never become something to roll back TO.
promote_env_lastgood() {
    cp .env .env.lastgood
    chmod 600 .env.lastgood
    echo "   .env.lastgood updated (this version is now the rollback target)"
}

# Where an auto-rollback restores from, most trustworthy first: the last
# verified .env, then the newest snapshot, then the pre-H9 single slot (which
# only the first deploy after this change can still need). Prints nothing when
# there is no candidate at all.
rollback_env_source() {
    if [ -f .env.lastgood ]; then
        echo .env.lastgood
        return 0
    fi
    # shellcheck disable=SC2012 # fixed prefix, no user-supplied names
    newest=\$(ls -1t .env.backup.* 2>/dev/null | head -1)
    if [ -n "\$newest" ]; then
        echo "\$newest"
        return 0
    fi
    if [ -f .env.backup ]; then
        echo .env.backup
    fi
}
# --- end H9 deploy serialization --------------------------------------------

# Serialize against a deploy converging this same VM (H9).
acquire_deploy_lock

# Replace-or-append a KEY=value in .env
set_env_tag() {
    local key="\$1" value="\$2"
    if grep -q "^\${key}=" .env; then
        sed -i "s|^\${key}=.*|\${key}=\${value}|" .env
    else
        echo "\${key}=\${value}" >> .env
    fi
}

# Save current state for reference
CURRENT_FRONTEND_TAG=\$(grep "^FRONTEND_IMAGE_TAG=" .env 2>/dev/null | cut -d'=' -f2 || echo "unknown")
CURRENT_BACKEND_TAG=\$(grep "^BACKEND_IMAGE_TAG=" .env 2>/dev/null | cut -d'=' -f2 || echo "unknown")
echo "Current tags: frontend=\$CURRENT_FRONTEND_TAG backend=\$CURRENT_BACKEND_TAG"
# H9: a timestamped snapshot, not the single .env.backup slot a concurrent
# deploy would overwrite. The verified rollback target is .env.lastgood, updated
# at the bottom of this script when the rolled-back version checks out.
snapshot_env

# Update the per-service image tags in .env (compose reads them from there)
if [ -n "\$FRONTEND_TARGET_TAG" ]; then
    set_env_tag FRONTEND_IMAGE_TAG "\$FRONTEND_TARGET_TAG"
    echo "🔄 Rolling frontend back to: \$FRONTEND_TARGET_TAG"
fi
if [ -n "\$BACKEND_TARGET_TAG" ]; then
    set_env_tag BACKEND_IMAGE_TAG "\$BACKEND_TARGET_TAG"
    echo "🔄 Rolling backend back to: \$BACKEND_TARGET_TAG"
fi

# SECURITY (P10): .env.runtime is gone - services declare explicit per-service
# environment allowlists in docker-compose.yml.
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

# Registry login happens below via a token minted on THIS machine and
# piped over ssh stdin — the VM has no aws CLI and no AWS credentials.

# Ensure the Postgres data volume exists (idempotent; declared external in
# docker-compose.yml so compose never deletes it)
echo "🗄️  Ensuring Postgres data volume exists..."
docker volume create openmentor-postgres-data

# Pull images with target tags
echo "📦 Pulling images..."
docker compose pull --ignore-buildable

# Converge: only services whose tag changed are recreated
echo "🔄 Restarting services..."
docker compose up -d

# Wait for startup
echo "⏳ Waiting for services to start..."
sleep 20

# The block below is byte-identical to the one in deploy-remote.sh (modulo the
# heredoc escaping this file needs) and is extracted from both by
# deploy-transition-test.sh; keep them in sync.
# --- P8 backup sidecar health gate (mirrored in deploy-remote.sh + rollback.sh) --
# NOTE: rollback.sh embeds this block in an UNQUOTED here-document, so every
# expansion is backslash-escaped there. deploy-transition-test.sh pushes that
# copy through a heredoc and requires the result to equal this one byte for
# byte, so keep the block free of backticks and of any other backslash.
#
# NOTE: if/elif, not case, and every paren in CODE balanced. bash 3.2 — still
# /bin/bash on macOS, where rollback.sh is run — finds the end of the \$( ) around
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
    backup_status=\$(docker inspect -f '{{.State.Status}}' openmentor-postgres-backup 2>/dev/null || true)
    if [ "\$backup_status" != "running" ]; then
        echo "❌ Postgres-backup health check FAILED (container status: \${backup_status:-<absent>})"
        return 1
    fi
    # The {{if}} guard is not cosmetic: .State.Health is absent on a container
    # with no healthcheck, and the bare .State.Health.Status template errors out
    # there rather than printing an empty string.
    backup_health=\$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' openmentor-postgres-backup 2>/dev/null || true)
    if [ "\$backup_health" = "healthy" ]; then
        echo "✅ Postgres-backup health check passed (running, healthy)"
    elif [ "\$backup_health" = "starting" ]; then
        # A just-recreated sidecar is legitimately "starting" for the whole
        # start_period; failing here would make every sidecar rebuild look like a
        # backup outage.
        echo "✅ Postgres-backup health check passed (running, healthcheck still in start_period)"
    elif [ "\$backup_health" = "unhealthy" ]; then
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

# Verify health
echo "🏥 Verifying health..."
HEALTH_OK=1
# Tracked apart from HEALTH_OK: stale dumps are not a failed rollback.
BACKUP_UNHEALTHY=0

if ! docker exec openmentor-frontend curl -f http://localhost:3000/api/healthcheck 2>/dev/null; then
    echo "❌ Frontend health check failed"
    HEALTH_OK=0
fi

if ! docker exec openmentor-backend curl -f http://localhost:8081/api/healthcheck 2>/dev/null; then
    echo "❌ Backend health check failed"
    HEALTH_OK=0
fi

if ! docker exec openmentor-worker curl -f http://localhost:8090/healthz 2>/dev/null; then
    echo "❌ Worker health check failed"
    HEALTH_OK=0
fi

POSTGRES_USER_ENV=\$(grep "^POSTGRES_USER=" .env | cut -d'=' -f2)
POSTGRES_DB_ENV=\$(grep "^POSTGRES_DB=" .env | cut -d'=' -f2)
if ! docker exec openmentor-postgres pg_isready -U "\${POSTGRES_USER_ENV:-openmentor}" -d "\${POSTGRES_DB_ENV:-openmentor}" 2>/dev/null; then
    echo "❌ Postgres health check failed"
    HEALTH_OK=0
fi

BACKUP_RC=0
check_backup_sidecar || BACKUP_RC=\$?
if [ "\$BACKUP_RC" -eq 1 ]; then
    HEALTH_OK=0
elif [ "\$BACKUP_RC" -eq 2 ]; then
    # Stale dumps predate this rollback and are not fixed by it. Reported, and
    # they still leave the run non-zero, but they must not be dressed up as a
    # failed rollback: during an incident that is how an operator is pushed into
    # drastic action over an aging dump.
    BACKUP_UNHEALTHY=1
fi

if [ \$HEALTH_OK -eq 1 ]; then
    # H9: the rolled-back version just passed every application health check, so
    # it becomes the verified target a later failed deploy reverts to. Without
    # this, the next auto-rollback would aim at the version this rollback was
    # run to escape.
    promote_env_lastgood
    if [ \$BACKUP_UNHEALTHY -eq 1 ]; then
        echo "✅ Rollback successful — images rolled back, app health checks passed"
        echo "❌ ...but the postgres-backup sidecar is UNHEALTHY (see above). Exiting 2:"
        echo "   the rollback itself needs no further action, the stale backups do."
        exit 2
    fi
    echo "✅ Rollback successful!"
    exit 0
else
    echo "❌ Rollback health checks failed!"
    echo "The .env this rollback replaced is the newest .env.backup.* in $REMOTE_INFRA_DIR;"
    echo ".env.lastgood still points at the last version that passed its health checks."
    exit 1
fi
REMOTE_SCRIPT
)

# Log the VM's docker into ECR first: token minted locally, piped over
# ssh stdin (the VM has no aws CLI / credentials). Needs a local aws
# identity with ECR read access.
if ! aws ecr get-login-password --region "$AWS_REGION" | \
    ssh ${VM_SSH_KEY_FILE:+-i "$VM_SSH_KEY_FILE"} -o StrictHostKeyChecking=accept-new \
    "$VM_SSH_USER@$VM_SSH_HOST" \
    "docker login --username AWS --password-stdin '$ECR_REGISTRY'"; then
    echo -e "${RED}❌ ECR login on the VM failed${NC}"
    exit 1
fi

# Execute on remote
ROLLBACK_EXIT_CODE=0
ssh ${VM_SSH_KEY_FILE:+-i "$VM_SSH_KEY_FILE"} -o StrictHostKeyChecking=accept-new \
    "$VM_SSH_USER@$VM_SSH_HOST" \
    bash <<< "$ROLLBACK_SCRIPT" || ROLLBACK_EXIT_CODE=$?

if [ $ROLLBACK_EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}✅ Rollback completed successfully!${NC}"
    echo ""
    echo "Rolled back to: frontend=${FRONTEND_TARGET_TAG:-<unchanged>} backend=${BACKEND_TARGET_TAG:-<unchanged>}"
    if [ -n "$DOMAIN" ]; then
        echo "Verify at: https://$DOMAIN"
    fi
elif [ $ROLLBACK_EXIT_CODE -eq 2 ]; then
    # Same code deploy-remote.sh uses: the images are back and the app is
    # healthy, the postgres-backup sidecar is not. Reported separately on
    # purpose — "manual intervention may be required" over an aging dump would
    # aim the operator at the wrong thing mid-incident.
    echo -e "${GREEN}✅ Rollback completed successfully!${NC}"
    echo ""
    echo "Rolled back to: frontend=${FRONTEND_TARGET_TAG:-<unchanged>} backend=${BACKEND_TARGET_TAG:-<unchanged>}"
    if [ -n "$DOMAIN" ]; then
        echo "Verify at: https://$DOMAIN"
    fi
    echo ""
    echo -e "${RED}❌ Separately: postgres-backup reports UNHEALTHY — the newest successful${NC}"
    echo -e "${RED}   pg_dump is past BACKUP_MAX_AGE_HOURS. Pre-existing, not caused by this${NC}"
    echo -e "${RED}   rollback. Follow ../docs/runbooks/postgres-backup-restore.md${NC}"
    exit 2
else
    echo -e "${RED}❌ Rollback failed!${NC}"
    echo -e "${YELLOW}💡 Manual intervention may be required${NC}"
    exit 1
fi
