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
# a .env.backup), pulls, re-converges with `docker-compose up -d` and runs
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

source "$SCRIPT_DIR/.env.production"

# Validate required variables
if [ -z "$VM_SSH_HOST" ] || [ -z "$VM_SSH_USER" ] || [ -z "$VM_SSH_KEY_FILE" ]; then
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
    read -p "$(echo -e ${BLUE}Enter image tag to rollback BOTH services to \(commit SHA\):${NC} )" TARGET_TAG
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
    read -p "$(echo -e ${RED}⚠️  Are you sure you want to rollback? \(yes/no\):${NC} )" -r
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
YANDEX_SA_KEY="$(cat $YANDEX_SA_KEY_FILE)"

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

# Regenerate .env.runtime (container env WITHOUT the tag lines — tags only
# affect compose interpolation, so only retagged services get recreated)
grep -vE '^(FRONTEND_IMAGE_TAG|BACKEND_IMAGE_TAG)=' .env > .env.runtime
chmod 600 .env.runtime

# Login to registry (TODO(P6.4): registry swap cr.yandex -> ghcr.io)
echo "🔑 Logging in to registry..."
echo "\$YANDEX_SA_KEY" | docker login \
    --username json_key \
    --password-stdin \
    cr.yandex

# Ensure the Postgres data volume exists (idempotent; declared external in
# docker-compose.yml so compose never deletes it)
echo "🗄️  Ensuring Postgres data volume exists..."
docker volume create openmentor-postgres-data

# Pull images with target tags
echo "📦 Pulling images..."
docker-compose pull

# Converge: only services whose tag changed are recreated
echo "🔄 Restarting services..."
docker-compose up -d

# Wait for startup
echo "⏳ Waiting for services to start..."
sleep 20

# Verify health
echo "🏥 Verifying health..."
HEALTH_OK=1

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

if [ "\$(docker inspect -f '{{.State.Status}}' openmentor-postgres-backup 2>/dev/null)" != "running" ]; then
    echo "❌ Postgres-backup container is not running"
    HEALTH_OK=0
fi

if [ \$HEALTH_OK -eq 1 ]; then
    echo "✅ Rollback successful!"
    exit 0
else
    echo "❌ Rollback health checks failed!"
    echo "Previous .env preserved as .env.backup in $REMOTE_INFRA_DIR"
    exit 1
fi
REMOTE_SCRIPT
)

# Execute on remote
ROLLBACK_EXIT_CODE=0
ssh -i "$VM_SSH_KEY_FILE" \
    -o StrictHostKeyChecking=no \
    "$VM_SSH_USER@$VM_SSH_HOST" \
    bash <<< "$ROLLBACK_SCRIPT" || ROLLBACK_EXIT_CODE=$?

if [ $ROLLBACK_EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}✅ Rollback completed successfully!${NC}"
    echo ""
    echo "Rolled back to: frontend=${FRONTEND_TARGET_TAG:-<unchanged>} backend=${BACKEND_TARGET_TAG:-<unchanged>}"
    if [ -n "$DOMAIN" ]; then
        echo "Verify at: https://$DOMAIN"
    fi
else
    echo -e "${RED}❌ Rollback failed!${NC}"
    echo -e "${YELLOW}💡 Manual intervention may be required${NC}"
    exit 1
fi
