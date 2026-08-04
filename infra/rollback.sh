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
# The script edits the tags in /opt/openmentor/infra/.env on the VM (keeping
# a .env.backup), pulls, re-converges with `docker compose up -d` and runs
# the same health checks as deploy.sh.
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

# --- H10 migration boundary guard (rollback.sh only) ------------------------
# NOTE: this block lives in an UNQUOTED here-document, so every expansion is
# backslash-escaped and it stays free of backticks. bash 3.2 — still /bin/bash
# on macOS, where rollback.sh is run — finds the end of the enclosing \$( ) by
# counting parens in the body, so no "case" here and every paren in CODE must
# balance. rollback-migration-guard-test.sh pushes this block back through a
# here-document and runs it against stubbed docker, so it tests the shipped text.
#
# WHY: migrate shares BACKEND_IMAGE_TAG with backend and worker (one image) and
# the migrations are baked in at /app/migrations. Point that tag at a build
# older than the applied schema version and golang-migrate's versionExists()
# cannot find the current version in the image's source: it fails with "no
# migration found for version N", migrate exits 1,
# depends_on: service_completed_successfully is never satisfied, and this
# script's set -e aborts — leaving production on the version being rolled back
# FROM. Refusing before .env is touched is the only outcome that leaves the VM
# where it was.
#
# This guard never applies a down-migration. 000009_modernise_tags.down.sql
# cannot restore the mentor associations its up-migration cascaded away, so that
# call belongs to a human with the runbook open.
GUARD_DIR=\$(mktemp -d)
POSTGRES_USER_GUARD=\$(grep "^POSTGRES_USER=" .env | cut -d'=' -f2)
POSTGRES_DB_GUARD=\$(grep "^POSTGRES_DB=" .env | cut -d'=' -f2)
ECR_REGISTRY_GUARD=\$(grep "^ECR_REGISTRY=" .env | cut -d'=' -f2)

SCHEMA_ROW=\$(docker exec openmentor-postgres psql \
    -U "\${POSTGRES_USER_GUARD:-openmentor}" -d "\${POSTGRES_DB_GUARD:-openmentor}" \
    -t -A -F'|' -c "SELECT version, dirty FROM schema_migrations" 2>/dev/null | head -1)
SCHEMA_VERSION=\${SCHEMA_ROW%%|*}
SCHEMA_DIRTY=\${SCHEMA_ROW##*|}

# The image's /app/migrations, read with "docker cp" from a container that is
# CREATED and never started: the runtime image ships no shell we want to depend
# on for this, and docker cp reads a created container's filesystem fine.
# The trailing /. is what makes docker cp write the CONTENTS into \$2.
migrations_from_image() {
    mkdir -p "\$2"
    GUARD_CID=\$(docker create "\$1" 2>/dev/null) || return 1
    GUARD_CP_RC=0
    docker cp "\$GUARD_CID:/app/migrations/." "\$2" >/dev/null 2>&1 || GUARD_CP_RC=1
    docker rm -f "\$GUARD_CID" >/dev/null 2>&1 || true
    return \$GUARD_CP_RC
}

# Highest migration version a directory of *.up.sql files carries, leading
# zeros stripped so [ -gt ] compares it as a decimal.
max_migration_in() {
    ls "\$1" 2>/dev/null | grep '\.up\.sql' | cut -c1-6 | sort -n | tail -1 | sed 's/^0*//'
}

# Contract-phase migrations that are already APPLIED. Read from the running
# backend's image, which by definition contains every version the database is at.
applied_contract_migrations() {
    for up in "\$GUARD_DIR/current"/*.up.sql; do
        if [ ! -f "\$up" ]; then continue; fi
        GUARD_V=\$(basename "\$up" | cut -c1-6 | sed 's/^0*//')
        if [ -z "\$GUARD_V" ]; then continue; fi
        if [ "\$GUARD_V" -gt "\${SCHEMA_VERSION:-0}" ]; then continue; fi
        if grep -q '^-- phase: contract' "\$up"; then
            printf ' %s' "\$(basename "\$up" .up.sql)"
        fi
    done
    return 0
}

migrations_from_image "\${ECR_REGISTRY_GUARD}/openmentor-backend:\${CURRENT_BACKEND_TAG}" \
    "\$GUARD_DIR/current" || true

check_migration_boundary() {
    if [ -z "\$BACKEND_TARGET_TAG" ] || [ "\$BACKEND_TARGET_TAG" = "\$CURRENT_BACKEND_TAG" ]; then
        return 0
    fi
    if [ -z "\$SCHEMA_VERSION" ]; then
        echo "⚠️  Could not read schema_migrations from openmentor-postgres, so the"
        echo "   migration boundary is UNVERIFIED. Expected when postgres is down —"
        echo "   which is also when you least want a rollback blocked, so this is not"
        echo "   a refusal. If the converge then dies at the migrate gate with 'no"
        echo "   migration found for version N', that was the boundary: follow"
        echo "   DEPLOYMENT.md 'Rolling back across a migration boundary'."
        return 0
    fi
    if [ "\$SCHEMA_DIRTY" = "t" ]; then
        echo "❌ REFUSING: schema_migrations reports the database DIRTY at version \$SCHEMA_VERSION."
        echo "   golang-migrate refuses to run at all in that state ('Dirty database"
        echo "   version \$SCHEMA_VERSION'), so this rollback would die at the migrate gate"
        echo "   whatever tag it carries. Resolve the half-applied migration first:"
        echo "   infra/DEPLOYMENT.md, 'Rolling back across a migration boundary'."
        return 1
    fi

    TARGET_IMAGE="\${ECR_REGISTRY_GUARD}/openmentor-backend:\${BACKEND_TARGET_TAG}"
    echo "🔎 Checking the migration boundary (schema is at version \$SCHEMA_VERSION)..."
    # This fires on ANY pull failure, not just a missing tag, and the old message
    # only named the tag — which sends an operator hunting for a tag that exists
    # while the actual fault is ECR or the network. docker's own error strings
    # are not a reliable discriminator (an expired ECR token and a deleted tag
    # both surface as "denied"/"repository does not exist"), so instead of
    # guessing we (a) print what docker actually said and (b) use the one signal
    # that IS conclusive: a tag sitting in the local cache provably exists.
    if ! GUARD_PULL_ERR=\$(docker pull "\$TARGET_IMAGE" 2>&1); then
        echo "❌ REFUSING: cannot pull \$TARGET_IMAGE. Nothing has been changed."
        echo "   docker said: \$(printf '%s\n' "\$GUARD_PULL_ERR" | tail -1)"
        if docker image inspect "\$TARGET_IMAGE" >/dev/null 2>&1; then
            echo "   That tag IS in this VM's local image cache, so it EXISTS."
            echo "   The registry or the network is what is unreachable — do not go"
            echo "   looking for a missing tag. Check ECR reachability and the token"
            echo "   (deploy-remote.sh/rollback.sh pipe a fresh one in over ssh stdin;"
            echo "   the VM itself has no aws CLI and no AWS credentials)."
        else
            echo "   It is also not in the local cache, so this is one of two things"
            echo "   and docker's message cannot reliably tell them apart:"
            echo "     * the tag is gone — ECR lifecycle keeps only the last ~20 images; or"
            echo "     * ECR is unreachable / the pushed token has expired."
            echo "   From a workstation, 'aws ecr describe-images --repository-name"
            echo "   openmentor-backend' settles the first; pulling any known-good tag"
            echo "   settles the second."
        fi
        echo "   Either way the rollback could not have completed: the converge below"
        echo "   runs 'docker compose pull' under set -e and needs the very registry"
        echo "   access this probe just failed. That compose pull is the real gate —"
        echo "   this check only moves the failure to before .env is rewritten."
        return 1
    fi
    if ! migrations_from_image "\$TARGET_IMAGE" "\$GUARD_DIR/target"; then
        echo "⚠️  Could not read /app/migrations out of \$TARGET_IMAGE, so the boundary"
        echo "   is UNVERIFIED. Continuing; see DEPLOYMENT.md if migrate then fails."
        return 0
    fi
    TARGET_MAX=\$(max_migration_in "\$GUARD_DIR/target")
    if [ -z "\$TARGET_MAX" ]; then
        echo "⚠️  \$TARGET_IMAGE ships no *.up.sql under /app/migrations — boundary"
        echo "   UNVERIFIED. Continuing."
        return 0
    fi
    if [ "\$SCHEMA_VERSION" -le "\$TARGET_MAX" ]; then
        echo "✅ Migration boundary OK: the target image ships migrations up to \$TARGET_MAX,"
        echo "   the database is at \$SCHEMA_VERSION, so migrate will find its version."
        return 0
    fi

    # Everything between the target's highest version and the applied one is
    # only in the CURRENT image; the target predates those files by definition.
    GUARD_ORPHANS=""
    GUARD_CONTRACTS=""
    for up in "\$GUARD_DIR/current"/*.up.sql; do
        if [ ! -f "\$up" ]; then continue; fi
        GUARD_V=\$(basename "\$up" | cut -c1-6 | sed 's/^0*//')
        if [ -z "\$GUARD_V" ]; then continue; fi
        if [ "\$GUARD_V" -le "\$TARGET_MAX" ]; then continue; fi
        if [ "\$GUARD_V" -gt "\$SCHEMA_VERSION" ]; then continue; fi
        GUARD_PHASE=\$(grep -m1 '^-- phase:' "\$up" | sed 's/^-- phase: *//')
        GUARD_ORPHANS="\$GUARD_ORPHANS \$(basename "\$up" .up.sql)[\${GUARD_PHASE:-unmarked}]"
        if [ "\$GUARD_PHASE" != "expand" ]; then
            GUARD_CONTRACTS="\$GUARD_CONTRACTS \$(basename "\$up" .up.sql)"
        fi
    done

    echo "❌ REFUSING: this rollback would cross a migration boundary."
    echo ""
    echo "   schema_migrations.version              : \$SCHEMA_VERSION"
    echo "   openmentor-backend:\$BACKEND_TARGET_TAG carries migrations up to: \$TARGET_MAX"
    echo "   Orphaned by the target image           :\${GUARD_ORPHANS:- <could not read the running image>}"
    echo ""
    echo "   Why a refusal and not a warning: migrate shares this tag, so"
    echo "   golang-migrate would look for version \$SCHEMA_VERSION in an image that"
    echo "   does not contain it, print 'no migration found for version"
    echo "   \$SCHEMA_VERSION' and exit 1. service_completed_successfully then never"
    echo "   completes, set -e aborts this script, and production is left on the"
    echo "   version you are rolling back FROM. There is nothing safer about trying."
    echo ""
    if [ -n "\$GUARD_CONTRACTS" ]; then
        echo "   Contract-phase migrations in the way:\$GUARD_CONTRACTS"
        echo "   Those removed or renamed something the target build still reads, so the"
        echo "   schema has to move with the image. Read each .down.sql header first —"
        echo "   some are explicitly lossy and a restore is the honest answer instead."
    else
        echo "   Every orphaned migration is expand-phase, so the target build runs"
        echo "   correctly against today's schema and nothing has to be undone for"
        echo "   correctness. Only the migrate gate is in the way."
    fi
    echo ""
    echo "   Do this instead — infra/DEPLOYMENT.md, section"
    echo "   'Rolling back across a migration boundary' — then re-run this script."
    echo "   Nothing has been changed — .env still reads frontend=\$CURRENT_FRONTEND_TAG"
    echo "   backend=\$CURRENT_BACKEND_TAG, and no .env.backup was written."
    return 1
}

# A frontend tag carries no migrations, so nothing on the VM can decide whether
# it predates an applied contract migration. Warn, do not refuse: the failure
# mode is wrong content (a pre-D30 build asks for tag names 000009 renamed away,
# so its catalog filters match nothing), not data loss or a failed converge —
# and a frontend rollback is often the fix being reached for mid-incident.
warn_frontend_schema_skew() {
    if [ -z "\$FRONTEND_TARGET_TAG" ] || [ -z "\$SCHEMA_VERSION" ]; then return 0; fi
    GUARD_SKEW=\$(applied_contract_migrations)
    if [ -z "\$GUARD_SKEW" ]; then return 0; fi
    echo "⚠️  Frontend rollback vs applied contract migrations:\$GUARD_SKEW"
    echo "   If \$FRONTEND_TARGET_TAG was built before those, it renders against a"
    echo "   schema that no longer has what it asks for. Not a refusal — but if the"
    echo "   catalog comes back with empty category filters, this is why."
    return 0
}

GUARD_RC=0
check_migration_boundary || GUARD_RC=1
if [ \$GUARD_RC -eq 0 ]; then
    warn_frontend_schema_skew
fi
rm -rf "\$GUARD_DIR"
if [ \$GUARD_RC -ne 0 ]; then
    exit 1
fi
# --- end H10 migration boundary guard ---------------------------------------

cp .env .env.backup

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
    echo "Previous .env preserved as .env.backup in $REMOTE_INFRA_DIR"
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
