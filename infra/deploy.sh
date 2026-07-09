#!/bin/bash
set -e

# OpenMentor Production Deployment Script
# Deploys from local machine to production VM on Yandex Cloud

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default flags - build both by default
BUILD_FRONTEND=true
BUILD_BACKEND=true

STAGING=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --frontend-only)
            BUILD_FRONTEND=true
            BUILD_BACKEND=false
            shift
            ;;
        --backend-only)
            BUILD_FRONTEND=false
            BUILD_BACKEND=true
            shift
            ;;
        --skip-frontend)
            BUILD_FRONTEND=false
            shift
            ;;
        --skip-backend)
            BUILD_BACKEND=false
            shift
            ;;
        --staging)
            STAGING=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --frontend-only    Build and push only frontend (keep current backend)"
            echo "  --backend-only     Build and push only backend (keep current frontend)"
            echo "  --skip-frontend    Skip frontend build (keep current frontend)"
            echo "  --skip-backend     Skip backend build (keep current backend)"
            echo "  --staging          Deploy to staging VM"
            echo "  -h, --help         Show this help message"
            echo ""
            echo "By default, both frontend and backend are built and pushed."
            echo "When skipping a build, the currently deployed image tag will be preserved."
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Validate that at least one service is being built
if [ "$BUILD_FRONTEND" = false ] && [ "$BUILD_BACKEND" = false ]; then
    echo -e "${RED}❌ Error: Cannot skip both frontend and backend builds${NC}"
    echo "At least one service must be built. Use --help for usage information."
    exit 1
fi

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$SCRIPT_DIR/../openmentor"
BACKEND_DIR="$SCRIPT_DIR/../openmentor-api"

# Load environment variables
if [ ! -f "$SCRIPT_DIR/.env.production" ]; then
    echo -e "${RED}❌ Error: .env.production file not found${NC}"
    echo -e "${YELLOW}💡 Please create .env.production with your production configuration${NC}"
    echo ""
    echo "Required variables:"
    echo "  - YANDEX_REGISTRY_ID"
    echo "  - YANDEX_SA_KEY_FILE (path to service account JSON key)"
    echo "  - VM_SSH_HOST"
    echo "  - VM_SSH_USER"
    echo "  - VM_SSH_KEY_FILE (path to SSH private key)"
    echo "  - DOMAIN"
    echo ""
    echo "Copy from template:"
    echo "  cp .env.production.example .env.production"
    echo "  # Then edit .env.production with your production values"
    exit 1
fi

source "$SCRIPT_DIR/version"

source "$SCRIPT_DIR/.env.production"

# Validate required variables
REQUIRED_VARS=(
    "YANDEX_REGISTRY_ID"
    "YANDEX_SA_KEY_FILE"
    "VM_SSH_HOST"
    "VM_SSH_USER"
    "VM_SSH_KEY_FILE"
)

if [ "$STAGING" = true ]; then
    REQUIRED_VARS=(
        "YANDEX_REGISTRY_ID"
        "YANDEX_SA_KEY_FILE"
        "VM_SSH_HOST_STAGING"
        "VM_SSH_USER_STAGING"
        "VM_SSH_KEY_FILE_STAGING"
    )
fi

_VM_SSH_HOST="$VM_SSH_HOST"
_VM_SSH_USER="$VM_SSH_USER"
_VM_SSH_KEY_FILE="$VM_SSH_KEY_FILE"

if [ "$STAGING" = true ]; then
    _VM_SSH_HOST="$VM_SSH_HOST_STAGING"
    _VM_SSH_USER="$VM_SSH_USER_STAGING"
    _VM_SSH_KEY_FILE="$VM_SSH_KEY_FILE_STAGING"

    echo -e "${YELLOW}⚠️  Deploying to STAGING environment at $_VM_SSH_HOST${NC}"
else
    echo -e "${GREEN}✅ Environment variables uploaded securely${NC}"
fi

for var in "${REQUIRED_VARS[@]}"; do
    if [ -z "${!var}" ]; then
        echo -e "${RED}❌ Error: $var is not set in .env.production${NC}"
        exit 1
    fi
done

# Validate key files exist
if [ ! -f "$YANDEX_SA_KEY_FILE" ]; then
    echo -e "${RED}❌ Error: Yandex service account key file not found: $YANDEX_SA_KEY_FILE${NC}"
    exit 1
fi

if [ ! -f "$_VM_SSH_KEY_FILE" ]; then
    echo -e "${RED}❌ Error: SSH key file not found: $_VM_SSH_KEY_FILE${NC}"
    exit 1
fi

# Generate image tags from respective repos
REGISTRY="cr.yandex"
FRONTEND_IMAGE="$REGISTRY/$YANDEX_REGISTRY_ID/openmentor-frontend"
BACKEND_IMAGE="$REGISTRY/$YANDEX_REGISTRY_ID/openmentor-backend"

# Get frontend SHA if building
if [ "$BUILD_FRONTEND" = true ]; then
    if [ -d "$FRONTEND_DIR/.git" ]; then
        FRONTEND_GIT_TAG=$(git -C "$FRONTEND_DIR" rev-parse --short HEAD)
    else
        FRONTEND_GIT_TAG=$(date +%Y%m%d-%H%M%S)
    fi
fi

# Get backend SHA if building
if [ "$BUILD_BACKEND" = true ]; then
    if [ -d "$BACKEND_DIR/.git" ]; then
        BACKEND_GIT_TAG=$(git -C "$BACKEND_DIR" rev-parse --short HEAD)
    else
        BACKEND_GIT_TAG=$(date +%Y%m%d-%H%M%S)
    fi
fi

echo -e "${GREEN}🚀 OpenMentor Production Deployment${NC}"
echo "=================================="
echo ""
echo "Registry: $REGISTRY/$YANDEX_REGISTRY_ID"
echo "VM: $_VM_SSH_USER@$_VM_SSH_HOST"
echo ""
echo "Build configuration:"
if [ "$BUILD_FRONTEND" = true ]; then
    echo -e "  • Frontend: BUILD NEW (${BLUE}$FRONTEND_GIT_TAG${NC})"
else
    echo -e "  • Frontend: ${YELLOW}SKIP (keep current)${NC}"
fi
if [ "$BUILD_BACKEND" = true ]; then
    echo -e "  • Backend: BUILD NEW (${BLUE}$BACKEND_GIT_TAG${NC})"
else
    echo -e "  • Backend: ${YELLOW}SKIP (keep current)${NC}"
fi
echo ""

# Confirmation prompt
read -p "$(echo -e ${YELLOW}Do you want to proceed with deployment? \(yes/no\):${NC} )" -r
echo
if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
    echo -e "${YELLOW}Deployment cancelled${NC}"
    exit 0
fi

# Step 1: Login to Yandex Container Registry
echo -e "${BLUE}🔑 Step 1/8: Logging in to Yandex Container Registry...${NC}"
cat "$YANDEX_SA_KEY_FILE" | docker login \
    --username json_key \
    --password-stdin \
    $REGISTRY

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to login to Yandex Container Registry${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Logged in successfully${NC}"
echo ""

# Step 2: Build Frontend Image
if [ "$BUILD_FRONTEND" = true ]; then
    echo -e "${BLUE}🏗️  Step 2/8: Building frontend image...${NC}"
    cd "$FRONTEND_DIR"

    # Load production build args
    NEXT_PUBLIC_GO_API_URL="${NEXT_PUBLIC_GO_API_URL:-http://backend:8081}"
    NEXT_PUBLIC_RECAPTCHA_V2_SITE_KEY="${NEXT_PUBLIC_RECAPTCHA_V2_SITE_KEY}"
    NEXT_PUBLIC_S3_STORAGE_ENDPOINT="${NEXT_PUBLIC_S3_STORAGE_ENDPOINT:-s3.eu-central-1.amazonaws.com}"
    NEXT_PUBLIC_S3_STORAGE_BUCKET="${NEXT_PUBLIC_S3_STORAGE_BUCKET:-mentor-images}"
    NEXT_PUBLIC_CDN_ENDPOINT="${NEXT_PUBLIC_CDN_ENDPOINT:-cdn.openmentor.io}"
    NEXT_PUBLIC_O11Y_SERVICE_NAMESPACE="${NEXT_PUBLIC_O11Y_SERVICE_NAMESPACE:-openmentor-frontend}"
    NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION="${NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION:-1.0.0}"
    NEXT_PUBLIC_FARO_APP_NAME="${NEXT_PUBLIC_FARO_APP_NAME:-openmentor-frontend}"
    NEXT_PUBLIC_FARO_COLLECTOR_URL="${NEXT_PUBLIC_FARO_COLLECTOR_URL}"
    NEXT_PUBLIC_FARO_SAMPLE_RATE="${NEXT_PUBLIC_FARO_SAMPLE_RATE:-0.5}"
    FARO_API_ENDPOINT="${FARO_API_ENDPOINT}"
    FARO_APP_ID="${FARO_APP_ID}"
    FARO_STACK_ID="${FARO_STACK_ID}"
    FARO_API_KEY="${FARO_API_KEY}"
    NEXT_PUBLIC_APP_ENV="${NEXT_PUBLIC_APP_ENV:-production}"
    NEXT_PUBLIC_ANALYTICS_PROVIDER="${NEXT_PUBLIC_ANALYTICS_PROVIDER:-posthog}"
    NEXT_PUBLIC_ANALYTICS_EVENT_VERSION="${NEXT_PUBLIC_ANALYTICS_EVENT_VERSION:-v1}"
    NEXT_PUBLIC_POSTHOG_KEY="${NEXT_PUBLIC_POSTHOG_KEY}"
    NEXT_PUBLIC_POSTHOG_HOST="${NEXT_PUBLIC_POSTHOG_HOST}"
    POSTHOG_PERSONAL_API_KEY="${POSTHOG_PERSONAL_API_KEY}"
    POSTHOG_PROJECT_ID="${POSTHOG_PROJECT_ID}"

    docker build \
        --platform linux/amd64 \
        --build-arg NEXT_PUBLIC_GO_API_URL="$NEXT_PUBLIC_GO_API_URL" \
        --build-arg NEXT_PUBLIC_RECAPTCHA_V2_SITE_KEY="$NEXT_PUBLIC_RECAPTCHA_V2_SITE_KEY" \
        --build-arg NEXT_PUBLIC_S3_STORAGE_ENDPOINT="$NEXT_PUBLIC_S3_STORAGE_ENDPOINT" \
        --build-arg NEXT_PUBLIC_S3_STORAGE_BUCKET="$NEXT_PUBLIC_S3_STORAGE_BUCKET" \
        --build-arg NEXT_PUBLIC_CDN_ENDPOINT="$NEXT_PUBLIC_CDN_ENDPOINT" \
        --build-arg NEXT_PUBLIC_O11Y_SERVICE_NAMESPACE="$NEXT_PUBLIC_O11Y_SERVICE_NAMESPACE" \
        --build-arg NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION="$NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION" \
        --build-arg NEXT_PUBLIC_FARO_APP_NAME="$NEXT_PUBLIC_FARO_APP_NAME" \
        --build-arg NEXT_PUBLIC_FARO_COLLECTOR_URL="$NEXT_PUBLIC_FARO_COLLECTOR_URL" \
        --build-arg NEXT_PUBLIC_FARO_SAMPLE_RATE="$NEXT_PUBLIC_FARO_SAMPLE_RATE" \
        --build-arg NEXT_PUBLIC_POSTHOG_KEY="$NEXT_PUBLIC_POSTHOG_KEY" \
        --build-arg NEXT_PUBLIC_POSTHOG_HOST="$NEXT_PUBLIC_POSTHOG_HOST" \
        --build-arg POSTHOG_PERSONAL_API_KEY="$POSTHOG_PERSONAL_API_KEY" \
        --build-arg POSTHOG_PROJECT_ID="$POSTHOG_PROJECT_ID" \
        --build-arg FARO_API_ENDPOINT="$FARO_API_ENDPOINT" \
        --build-arg FARO_APP_ID="$FARO_APP_ID" \
        --build-arg FARO_STACK_ID="$FARO_STACK_ID" \
        --build-arg FARO_API_KEY="$FARO_API_KEY" \
        --build-arg NEXT_PUBLIC_APP_ENV="$NEXT_PUBLIC_APP_ENV" \
        --build-arg NEXT_PUBLIC_ANALYTICS_PROVIDER="$NEXT_PUBLIC_ANALYTICS_PROVIDER" \
        --build-arg NEXT_PUBLIC_ANALYTICS_EVENT_VERSION="$NEXT_PUBLIC_ANALYTICS_EVENT_VERSION" \
        -t "$FRONTEND_IMAGE:$FRONTEND_GIT_TAG" \
        .

    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to build frontend image${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Frontend image built${NC}"
    echo ""
else
    echo -e "${YELLOW}⏭️  Step 2/8: Skipping frontend build${NC}"
    echo ""
fi

# Step 3: Build Backend Image
if [ "$BUILD_BACKEND" = true ]; then
    echo -e "${BLUE}🏗️  Step 3/8: Building backend image...${NC}"
    cd "$BACKEND_DIR"

    docker build \
        --platform linux/amd64 \
        -t "$BACKEND_IMAGE:$BACKEND_GIT_TAG" \
        .

    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to build backend image${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Backend image built${NC}"
    echo ""
else
    echo -e "${YELLOW}⏭️  Step 3/8: Skipping backend build${NC}"
    echo ""
fi

# Step 4: Fetch current image tags from remote (if needed)
if [ "$BUILD_FRONTEND" = false ] || [ "$BUILD_BACKEND" = false ]; then
    echo -e "${BLUE}📡 Step 4/7: Fetching current image tags from production...${NC}"
    REMOTE_ENV=$(ssh -i "$_VM_SSH_KEY_FILE" \
        -o StrictHostKeyChecking=no \
        "$_VM_SSH_USER@$_VM_SSH_HOST" \
        "cat /opt/openmentor-infra/.env 2>/dev/null || echo ''")

    if [ -n "$REMOTE_ENV" ]; then
        CURRENT_FRONTEND_TAG=$(echo "$REMOTE_ENV" | grep "^FRONTEND_IMAGE_TAG=" | cut -d'=' -f2)
        CURRENT_BACKEND_TAG=$(echo "$REMOTE_ENV" | grep "^BACKEND_IMAGE_TAG=" | cut -d'=' -f2)
        # Fallback to IMAGE_TAG if separate tags don't exist (backward compatibility)
        if [ -z "$CURRENT_FRONTEND_TAG" ] || [ -z "$CURRENT_BACKEND_TAG" ]; then
            CURRENT_IMAGE_TAG=$(echo "$REMOTE_ENV" | grep "^IMAGE_TAG=" | cut -d'=' -f2)
            CURRENT_FRONTEND_TAG="${CURRENT_FRONTEND_TAG:-$CURRENT_IMAGE_TAG}"
            CURRENT_BACKEND_TAG="${CURRENT_BACKEND_TAG:-$CURRENT_IMAGE_TAG}"
        fi
        echo -e "${GREEN}✅ Current production tags:${NC}"
        echo "  • Frontend: $CURRENT_FRONTEND_TAG"
        echo "  • Backend: $CURRENT_BACKEND_TAG"
    else
        echo -e "${RED}❌ Failed to fetch current tags from production${NC}"
        echo "Cannot skip builds without knowing current deployed versions"
        exit 1
    fi
    echo ""
fi

# Step 5: Push Images
echo -e "${BLUE}📤 Step 5/7: Pushing images to registry...${NC}"

if [ "$BUILD_FRONTEND" = true ]; then
    echo "Pushing frontend ($FRONTEND_GIT_TAG)..."
    docker push "$FRONTEND_IMAGE:$FRONTEND_GIT_TAG"
    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to push frontend image${NC}"
        exit 1
    fi
    FRONTEND_IMAGE_TAG="$FRONTEND_GIT_TAG"
else
    echo -e "${YELLOW}Skipping frontend push (keeping current: $CURRENT_FRONTEND_TAG)${NC}"
    FRONTEND_IMAGE_TAG="$CURRENT_FRONTEND_TAG"
fi

if [ "$BUILD_BACKEND" = true ]; then
    echo "Pushing backend ($BACKEND_GIT_TAG)..."
    docker push "$BACKEND_IMAGE:$BACKEND_GIT_TAG"
    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to push backend image${NC}"
        exit 1
    fi
    BACKEND_IMAGE_TAG="$BACKEND_GIT_TAG"
else
    echo -e "${YELLOW}Skipping backend push (keeping current: $CURRENT_BACKEND_TAG)${NC}"
    BACKEND_IMAGE_TAG="$CURRENT_BACKEND_TAG"
fi

echo -e "${GREEN}✅ Images pushed successfully${NC}"
echo ""

# Step 6: Upload Runtime Environment Variables
echo -e "${BLUE}🔐 Step 6/8: Uploading runtime environment variables...${NC}"

# Create temporary env file with image tags
TEMP_ENV_FILE=$(mktemp)
trap "rm -f $TEMP_ENV_FILE" EXIT

# Copy .env.production and set image tags
cp "$SCRIPT_DIR/.env.production" "$TEMP_ENV_FILE"
echo "" >> "$TEMP_ENV_FILE"
echo "# Auto-generated by deployment script" >> "$TEMP_ENV_FILE"
echo "FRONTEND_IMAGE_TAG=$FRONTEND_IMAGE_TAG" >> "$TEMP_ENV_FILE"
echo "BACKEND_IMAGE_TAG=$BACKEND_IMAGE_TAG" >> "$TEMP_ENV_FILE"

# Upload to VM
echo "Uploading .env file to production VM..."
scp -i "$_VM_SSH_KEY_FILE" \
    -o StrictHostKeyChecking=no \
    "$TEMP_ENV_FILE" \
    "$_VM_SSH_USER@$_VM_SSH_HOST:/opt/openmentor-infra/.env"

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to upload environment file${NC}"
    exit 1
fi

# Set proper permissions on remote .env file
ssh -i "$_VM_SSH_KEY_FILE" \
    -o StrictHostKeyChecking=no \
    "$_VM_SSH_USER@$_VM_SSH_HOST" \
    "chmod 600 /opt/openmentor-infra/.env"

echo -e "${GREEN}✅ Environment variables uploaded securely${NC}"
echo ""

# Step 6b: Create Alloy database observability secrets on the VM
# The DSN is extracted from the already-uploaded .env file on the remote machine
# so that the password never appears as a command-line argument.
POSTGRES_OBS_DSN_LOCAL=$(grep "^POSTGRES_OBS_DSN=" "$SCRIPT_DIR/.env.production" | cut -d'=' -f2-)
if [ -n "$POSTGRES_OBS_DSN_LOCAL" ]; then
    echo -e "${BLUE}🔐 Step 6b: Creating Alloy database observability secrets...${NC}"

    SECRETS_SCRIPT=$(cat <<'SECRETS_SCRIPT_EOF'
#!/bin/bash
set -e
SECRETS_DIR=/opt/openmentor-infra/alloy-secrets

mkdir -p "$SECRETS_DIR"
chmod 700 "$SECRETS_DIR"

# Extract DSN from the uploaded .env and write it to the secrets file
grep "^POSTGRES_OBS_DSN=" /opt/openmentor-infra/.env | cut -d'=' -f2- | tr -d '\n' \
    > "$SECRETS_DIR/postgres_secret_openmentor"
chmod 600 "$SECRETS_DIR/postgres_secret_openmentor"

# Download the Yandex Cloud CA certificate (only if not already present)
if [ ! -f "$SECRETS_DIR/CA.pem" ]; then
    wget -q "https://storage.yandexcloud.net/cloud-certs/CA.pem" \
        -O "$SECRETS_DIR/CA.pem"
    chmod 644 "$SECRETS_DIR/CA.pem"
    echo "Downloaded Yandex Cloud CA certificate"
fi

echo "Database observability secrets ready in $SECRETS_DIR"
SECRETS_SCRIPT_EOF
)

    ssh -i "$_VM_SSH_KEY_FILE" \
        -o StrictHostKeyChecking=no \
        "$_VM_SSH_USER@$_VM_SSH_HOST" \
        "bash -s" <<< "$SECRETS_SCRIPT"

    if [ $? -ne 0 ]; then
        echo -e "${YELLOW}⚠️  Failed to create Alloy database secrets (non-fatal, observability only)${NC}"
    else
        echo -e "${GREEN}✅ Alloy database observability secrets created${NC}"
    fi
    echo ""
else
    echo -e "${YELLOW}⚠️  POSTGRES_OBS_DSN not set in .env.production – skipping Alloy database secrets${NC}"
    echo ""
fi

# Step 7: Deploy to Production VM
echo -e "${BLUE}🚢 Step 7/8: Deploying to production VM...${NC}"
cd "$SCRIPT_DIR"

# Create remote deployment script
DEPLOY_SCRIPT=$(cat <<'REMOTE_SCRIPT'
#!/bin/bash
set -e

ENCODED_SA_KEY="$1"

# Decode the base64-encoded service account key
YANDEX_SA_KEY=$(echo "$ENCODED_SA_KEY" | base64 -d)

echo "🚀 Starting deployment on production VM..."

cd /opt/openmentor-infra

# Backup existing .env file for rollback
if [ -f .env ]; then
    cp .env .env.backup
fi

# Read image tags from uploaded .env file
FRONTEND_IMAGE_TAG=$(grep "^FRONTEND_IMAGE_TAG=" .env | cut -d'=' -f2)
BACKEND_IMAGE_TAG=$(grep "^BACKEND_IMAGE_TAG=" .env | cut -d'=' -f2)
echo "Deploying with:"
echo "  • Frontend image tag: $FRONTEND_IMAGE_TAG"
echo "  • Backend image tag: $BACKEND_IMAGE_TAG"

# Pull latest infrastructure code
echo "📥 Pulling latest infrastructure configuration..."
git pull origin main || echo "⚠️  Git pull failed, using existing code"

# Login to Yandex Container Registry
echo "🔑 Logging in to registry..."

# Disable docker credential helper for this session
# Move config.json temporarily if it exists
DOCKER_CONFIG_MOVED=0
if [ -f ~/.docker/config.json ]; then
    mv ~/.docker/config.json ~/.docker/config.json.disabled
    DOCKER_CONFIG_MOVED=1
fi

# Login to registry (creates new config.json without credential helper)
echo "$YANDEX_SA_KEY" | docker login \
    --username json_key \
    --password-stdin \
    cr.yandex

LOGIN_EXIT=$?

# Restore original config if login was successful
# (we want to keep the new auth, but merge with old config if needed)
if [ $DOCKER_CONFIG_MOVED -eq 1 ] && [ $LOGIN_EXIT -eq 0 ]; then
    # Just remove the disabled config - we have fresh auth now
    rm -f ~/.docker/config.json.disabled
elif [ $DOCKER_CONFIG_MOVED -eq 1 ]; then
    # Login failed, restore original
    mv ~/.docker/config.json.disabled ~/.docker/config.json
    echo "❌ Docker login failed"
    exit 1
fi

if [ $LOGIN_EXIT -ne 0 ]; then
    echo "❌ Docker login failed"
    exit 1
fi

# Ensure the Postgres data volume exists (idempotent). It is declared
# `external` in docker-compose.yml so `docker compose down -v` can never
# delete the production database.
echo "🗄️  Ensuring Postgres data volume exists..."
docker volume create openmentor-postgres-data

# Pull new images
echo "📦 Pulling new images..."
docker-compose pull

# Restart services with zero downtime
echo "🔄 Restarting services..."
docker-compose up -d

# Wait for containers to start
echo "⏳ Waiting for containers to start..."
sleep 20

# Check service status
echo "📊 Service status:"
docker-compose ps

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

# Check postgres health (credentials from the uploaded .env)
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

    # Restore backup .env file
    if [ -f .env.backup ]; then
        cp .env.backup .env
        echo "Restored previous .env file"
    else
        echo "❌ No backup .env file found, cannot rollback!"
        exit 1
    fi

    docker-compose pull
    docker-compose up -d
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
REMOTE_SCRIPT
)

# Execute deployment on remote VM
# Encode the JSON key in base64 to avoid quoting issues
ENCODED_SA_KEY=$(cat "$YANDEX_SA_KEY_FILE" | base64)

ssh -i "$_VM_SSH_KEY_FILE" \
    -o StrictHostKeyChecking=no \
    "$_VM_SSH_USER@$_VM_SSH_HOST" \
    "bash -s" -- "$ENCODED_SA_KEY" <<< "$DEPLOY_SCRIPT"

DEPLOY_EXIT_CODE=$?

if [ $DEPLOY_EXIT_CODE -ne 0 ]; then
    echo -e "${RED}❌ Deployment failed!${NC}"
    echo -e "${YELLOW}💡 Check the logs above for details${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Deployment successful${NC}"
echo ""

# Step 7: Verify public endpoint
echo -e "${BLUE}🔍 Step 8/8: Verifying public endpoint...${NC}"
sleep 10

if [ -n "$DOMAIN" ]; then
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "https://$DOMAIN/api/healthcheck" || echo "000")

    if [ "$HTTP_STATUS" = "200" ]; then
        echo -e "${GREEN}✅ Public health check passed (HTTP $HTTP_STATUS)${NC}"
    else
        echo -e "${YELLOW}⚠️  Public health check returned HTTP $HTTP_STATUS${NC}"
        echo -e "${YELLOW}This might be expected if DNS/SSL is still propagating${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  DOMAIN not set, skipping public health check${NC}"
fi

echo ""
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "${GREEN}✨ Deployment completed successfully! ✨${NC}"
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo ""
echo "📋 Deployment Summary:"
echo "  • Frontend: $FRONTEND_IMAGE:$FRONTEND_IMAGE_TAG"
echo "  • Backend: $BACKEND_IMAGE:$BACKEND_IMAGE_TAG"
if [ -n "$DOMAIN" ]; then
    echo "  • URL: https://$DOMAIN"
fi
echo ""
echo "🔗 Next steps:"
echo "  1. Monitor application at https://$DOMAIN"
echo "  2. Check Grafana dashboards for metrics"
echo "  3. Verify logs in Grafana Loki"
echo ""
