#!/bin/bash
set -e

# OpenMentor Production Rollback Script
# Quickly rollback to a previous deployment

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

echo -e "${YELLOW}🔄 OpenMentor Production Rollback${NC}"
echo "================================"
echo ""

# Get target tag from argument or ask user
if [ -n "$1" ]; then
    TARGET_TAG="$1"
else
    read -p "$(echo -e ${BLUE}Enter image tag to rollback to \(commit SHA\):${NC} )" TARGET_TAG
    echo ""
fi

if [ -z "$TARGET_TAG" ]; then
    echo -e "${RED}❌ Error: No target tag specified${NC}"
    exit 1
fi

echo "Target tag: $TARGET_TAG"
echo "VM: $VM_SSH_USER@$VM_SSH_HOST"
echo ""

# Confirmation
read -p "$(echo -e ${RED}⚠️  Are you sure you want to rollback? \(yes/no\):${NC} )" -r
echo
if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
    echo -e "${YELLOW}Rollback cancelled${NC}"
    exit 0
fi

echo -e "${BLUE}🔄 Executing rollback...${NC}"

# Create rollback script
ROLLBACK_SCRIPT=$(cat <<REMOTE_SCRIPT
#!/bin/bash
set -e

TARGET_TAG="$TARGET_TAG"
YANDEX_SA_KEY="$(cat $YANDEX_SA_KEY_FILE)"

echo "🔄 Rolling back to tag: \$TARGET_TAG"

# The monorepo is checked out at /opt/openmentor; compose files live in infra/
cd /opt/openmentor/infra

# Save current tag as backup
CURRENT_TAG=\$(grep "IMAGE_TAG=" .env 2>/dev/null | cut -d'=' -f2 || echo "unknown")
echo "Current tag: \$CURRENT_TAG"
echo "\$CURRENT_TAG" > /tmp/rollback_from_tag

# Update image tag
export IMAGE_TAG="\$TARGET_TAG"
echo "IMAGE_TAG=\$TARGET_TAG" > .env.image_tag

# Login to registry
echo "🔑 Logging in to registry..."
echo "\$YANDEX_SA_KEY" | docker login \
    --username json_key \
    --password-stdin \
    cr.yandex

# Ensure the Postgres data volume exists (idempotent; declared external in
# docker-compose.yml so compose never deletes it)
echo "🗄️  Ensuring Postgres data volume exists..."
docker volume create openmentor-postgres-data

# Pull images with target tag
echo "📦 Pulling images with tag: \$TARGET_TAG..."
docker-compose pull

# Restart services
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
    exit 1
fi
REMOTE_SCRIPT
)

# Execute on remote
ssh -i "$VM_SSH_KEY_FILE" \
    -o StrictHostKeyChecking=no \
    "$VM_SSH_USER@$VM_SSH_HOST" \
    bash <<< "$ROLLBACK_SCRIPT"

ROLLBACK_EXIT_CODE=$?

if [ $ROLLBACK_EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}✅ Rollback completed successfully!${NC}"
    echo ""
    echo "Rolled back to: $TARGET_TAG"
    if [ -n "$DOMAIN" ]; then
        echo "Verify at: https://$DOMAIN"
    fi
else
    echo -e "${RED}❌ Rollback failed!${NC}"
    echo -e "${YELLOW}💡 Manual intervention may be required${NC}"
    exit 1
fi
