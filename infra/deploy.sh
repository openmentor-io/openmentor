#!/bin/bash
set -e

# ============================================================================
# OpenMentor Production Deployment Script
# ============================================================================
# Deploys from a workstation to the production VM.
#
#   ./deploy.sh [targets...] [options]
#
# Targets (default: frontend backend):
#   frontend   build ../web, push, roll the frontend container
#   backend    build ../api, push, roll migrate + backend + worker
#              (one image, three services; migrate runs before backend/worker
#              start via depends_on: service_completed_successfully)
#   infra      rsync this infra/ directory to /opt/openmentor/infra on the VM
#              (excluding .env*, logs/, alloy-secrets/) and converge
#              compose-level changes with `up -d --remove-orphans`
#   all        frontend backend infra
#
# Options:
#   --tag TAG    use TAG for built images instead of the git commit SHA
#   --yes, -y    skip the confirmation prompt
#   --dry-run    print the deployment plan and exit without doing anything
#   --staging    deploy to the staging VM (VM_SSH_*_STAGING variables)
#   -h, --help   show help
#
# Bind-mount trap (infra target): `docker compose up -d` only reacts to
# compose-level changes (image tags, service definitions, env). Config that
# reaches a container through a bind-mounted FILE changes on disk without
# compose noticing. Inventory of file bind mounts in docker-compose.yml:
#   - alloy      <- ./alloy/config.alloy        (restart needed on change)
#   - alloy      <- ./alloy-secrets/            (runtime state written by this
#                                                script, never rsynced)
#   - postgres-backup is BUILT on the VM from ./postgres-backup/ (a rebuild,
#                                                not a restart, on change)
#   - traefik has no file-based config (all static config is command flags,
#     dynamic config is docker labels) — compose convergence covers it.
# The infra target detects changed files via `rsync --checksum
# --itemize-changes` and restarts/rebuilds exactly the affected services.
#
# Postgres note: bumping the pinned postgres image recreates the container
# safely — data lives in the external volume `openmentor-postgres-data`
# (minor/patch versions only; MAJOR upgrades follow
# ../docs/runbooks/postgres-backup-restore.md).
# ============================================================================

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

usage() {
    echo "Usage: $0 [targets...] [options]"
    echo ""
    echo "Targets (default: frontend backend):"
    echo "  frontend           Build + push + deploy the frontend image"
    echo "  backend            Build + push + deploy the backend image (backend/worker/migrate)"
    echo "  infra              Sync infra/ to the VM and converge compose-level changes"
    echo "  all                frontend backend infra"
    echo ""
    echo "Options:"
    echo "  --tag TAG          Use TAG for built images instead of the git commit SHA"
    echo "  --yes, -y          Skip the confirmation prompt"
    echo "  --dry-run          Print the deployment plan and exit"
    echo "  --staging          Deploy to the staging VM (VM_SSH_*_STAGING vars)"
    echo "  -h, --help         Show this help message"
    echo ""
    echo "Services not being deployed keep their currently deployed image tags."
}

# --------------------------------------------------------------------------
# Parse command line arguments
# --------------------------------------------------------------------------
DEPLOY_FRONTEND=false
DEPLOY_BACKEND=false
DEPLOY_INFRA=false
TARGETS_GIVEN=false
TAG_OVERRIDE=""
SKIP_CONFIRM=false
DRY_RUN=false
STAGING=false

while [[ $# -gt 0 ]]; do
    case $1 in
        frontend)
            DEPLOY_FRONTEND=true; TARGETS_GIVEN=true; shift ;;
        backend)
            DEPLOY_BACKEND=true; TARGETS_GIVEN=true; shift ;;
        infra)
            DEPLOY_INFRA=true; TARGETS_GIVEN=true; shift ;;
        all)
            DEPLOY_FRONTEND=true; DEPLOY_BACKEND=true; DEPLOY_INFRA=true
            TARGETS_GIVEN=true; shift ;;
        --tag)
            if [ -z "$2" ]; then
                echo -e "${RED}❌ --tag requires a value${NC}"; exit 1
            fi
            TAG_OVERRIDE="$2"; shift 2 ;;
        --yes|-y)
            SKIP_CONFIRM=true; shift ;;
        --dry-run)
            DRY_RUN=true; shift ;;
        --staging)
            STAGING=true; shift ;;
        -h|--help)
            usage; exit 0 ;;
        *)
            echo -e "${RED}Unknown target/option: $1${NC}"
            echo "Use --help for usage information"
            exit 1 ;;
    esac
done

# Default targets: frontend backend
if [ "$TARGETS_GIVEN" = false ]; then
    DEPLOY_FRONTEND=true
    DEPLOY_BACKEND=true
fi

# --------------------------------------------------------------------------
# Configuration (frontend/backend are sibling directories of infra/ in the
# openmentor monorepo)
# --------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$SCRIPT_DIR/../web"
BACKEND_DIR="$SCRIPT_DIR/../api"
REMOTE_INFRA_DIR="/opt/openmentor/infra"

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
    echo "  - VM_SSH_KEY_FILE (optional; omit to use your ssh agent, e.g. 1Password)"
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

# VM_SSH_KEY_FILE is OPTIONAL: when unset, ssh uses your agent (works with
# the 1Password SSH agent, ssh-agent, etc.). Set it only for file-based keys.
SSH_OPTS=(-o StrictHostKeyChecking=no)
if [ -n "${_VM_SSH_KEY_FILE:-}" ]; then
    if [ ! -f "$_VM_SSH_KEY_FILE" ]; then
        echo -e "${RED}❌ Error: SSH key file not found: $_VM_SSH_KEY_FILE${NC}"
        exit 1
    fi
    SSH_OPTS=(-i "$_VM_SSH_KEY_FILE" "${SSH_OPTS[@]}")
fi

# Generate image tags. TODO(P6.4): registry swap cr.yandex -> AWS ECR (D19)
REGISTRY="cr.yandex"
FRONTEND_IMAGE="$REGISTRY/$YANDEX_REGISTRY_ID/openmentor-frontend"
BACKEND_IMAGE="$REGISTRY/$YANDEX_REGISTRY_ID/openmentor-backend"

# The monorepo's short commit SHA is the image tag (see DOCKER_TAG_POLICY.md
# — never `latest`). web/ and api/ are part of the monorepo, so the .git
# directory lives at the repo root — resolve HEAD via git -C, with a
# timestamp fallback for non-git exports. --tag overrides.
if [ "$DEPLOY_FRONTEND" = true ]; then
    FRONTEND_GIT_TAG="${TAG_OVERRIDE:-$(git -C "$FRONTEND_DIR" rev-parse --short HEAD 2>/dev/null || date +%Y%m%d-%H%M%S)}"
fi

if [ "$DEPLOY_BACKEND" = true ]; then
    BACKEND_GIT_TAG="${TAG_OVERRIDE:-$(git -C "$BACKEND_DIR" rev-parse --short HEAD 2>/dev/null || date +%Y%m%d-%H%M%S)}"
fi

# --------------------------------------------------------------------------
# Deployment plan
# --------------------------------------------------------------------------
echo -e "${GREEN}🚀 OpenMentor Production Deployment${NC}"
echo "=================================="
echo ""
echo "Registry: $REGISTRY/$YANDEX_REGISTRY_ID"
echo "VM: $_VM_SSH_USER@$_VM_SSH_HOST ($REMOTE_INFRA_DIR)"
echo ""
echo "Deployment plan:"
if [ "$DEPLOY_FRONTEND" = true ]; then
    echo -e "  • frontend: BUILD + PUSH + DEPLOY (${BLUE}$FRONTEND_GIT_TAG${NC})"
else
    echo -e "  • frontend: ${YELLOW}keep current tag${NC}"
fi
if [ "$DEPLOY_BACKEND" = true ]; then
    echo -e "  • backend:  BUILD + PUSH + DEPLOY (${BLUE}$BACKEND_GIT_TAG${NC}) — backend + worker + migrate"
else
    echo -e "  • backend:  ${YELLOW}keep current tag${NC}"
fi
if [ "$DEPLOY_INFRA" = true ]; then
    echo -e "  • infra:    SYNC infra/ + converge compose (up -d --remove-orphans)"
    echo "              restart/rebuild services whose bind-mounted config changed"
else
    echo -e "  • infra:    ${YELLOW}skip (no config sync)${NC}"
fi
echo ""
echo "Every deploy also re-uploads .env.production as the VM's .env"
echo "(services not being deployed keep their current image tags)."
echo ""

if [ "$DRY_RUN" = true ]; then
    echo -e "${YELLOW}--dry-run: stopping here, nothing was executed.${NC}"
    exit 0
fi

# Confirmation prompt
if [ "$SKIP_CONFIRM" = false ]; then
    read -p "$(echo -e ${YELLOW}Do you want to proceed with deployment? \(yes/no\):${NC} )" -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        echo -e "${YELLOW}Deployment cancelled${NC}"
        exit 0
    fi
fi

# --------------------------------------------------------------------------
# Step 1/9: Registry login
# --------------------------------------------------------------------------
if [ "$DEPLOY_FRONTEND" = true ] || [ "$DEPLOY_BACKEND" = true ]; then
    echo -e "${BLUE}🔑 Step 1/9: Logging in to container registry...${NC}"
    cat "$YANDEX_SA_KEY_FILE" | docker login \
        --username json_key \
        --password-stdin \
        $REGISTRY

    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to login to container registry${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Logged in successfully${NC}"
else
    echo -e "${YELLOW}⏭️  Step 1/9: No images to push — skipping registry login${NC}"
fi
echo ""

# --------------------------------------------------------------------------
# Step 2/9: Build frontend image
# --------------------------------------------------------------------------
if [ "$DEPLOY_FRONTEND" = true ]; then
    echo -e "${BLUE}🏗️  Step 2/9: Building frontend image...${NC}"
    cd "$FRONTEND_DIR"

    # Load production build args
    NEXT_PUBLIC_GO_API_URL="${NEXT_PUBLIC_GO_API_URL:-http://backend:8081}"
    NEXT_PUBLIC_TURNSTILE_SITE_KEY="${NEXT_PUBLIC_TURNSTILE_SITE_KEY}"
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
        --build-arg NEXT_PUBLIC_TURNSTILE_SITE_KEY="$NEXT_PUBLIC_TURNSTILE_SITE_KEY" \
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
else
    echo -e "${YELLOW}⏭️  Step 2/9: Skipping frontend build${NC}"
fi
echo ""

# --------------------------------------------------------------------------
# Step 3/9: Build backend image
# --------------------------------------------------------------------------
if [ "$DEPLOY_BACKEND" = true ]; then
    echo -e "${BLUE}🏗️  Step 3/9: Building backend image...${NC}"
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
else
    echo -e "${YELLOW}⏭️  Step 3/9: Skipping backend build${NC}"
fi
echo ""

# --------------------------------------------------------------------------
# Step 4/9: Fetch current image tags from the VM (for services not deployed)
# --------------------------------------------------------------------------
if [ "$DEPLOY_FRONTEND" = false ] || [ "$DEPLOY_BACKEND" = false ]; then
    echo -e "${BLUE}📡 Step 4/9: Fetching current image tags from production...${NC}"
    REMOTE_ENV=$(ssh "${SSH_OPTS[@]}" \
        "$_VM_SSH_USER@$_VM_SSH_HOST" \
        "cat $REMOTE_INFRA_DIR/.env 2>/dev/null || echo ''")

    if [ -n "$REMOTE_ENV" ]; then
        CURRENT_FRONTEND_TAG=$(echo "$REMOTE_ENV" | grep "^FRONTEND_IMAGE_TAG=" | cut -d'=' -f2)
        CURRENT_BACKEND_TAG=$(echo "$REMOTE_ENV" | grep "^BACKEND_IMAGE_TAG=" | cut -d'=' -f2)
        # Fallback to IMAGE_TAG if separate tags don't exist (backward compatibility)
        if [ -z "$CURRENT_FRONTEND_TAG" ] || [ -z "$CURRENT_BACKEND_TAG" ]; then
            CURRENT_IMAGE_TAG=$(echo "$REMOTE_ENV" | grep "^IMAGE_TAG=" | cut -d'=' -f2)
            CURRENT_FRONTEND_TAG="${CURRENT_FRONTEND_TAG:-$CURRENT_IMAGE_TAG}"
            CURRENT_BACKEND_TAG="${CURRENT_BACKEND_TAG:-$CURRENT_IMAGE_TAG}"
        fi
    fi

    if [ -z "$CURRENT_FRONTEND_TAG" ] || [ -z "$CURRENT_BACKEND_TAG" ]; then
        echo -e "${RED}❌ Failed to fetch current tags from production${NC}"
        echo "Cannot skip builds without knowing current deployed versions"
        exit 1
    fi

    echo -e "${GREEN}✅ Current production tags:${NC}"
    echo "  • Frontend: $CURRENT_FRONTEND_TAG"
    echo "  • Backend: $CURRENT_BACKEND_TAG"
else
    echo -e "${YELLOW}⏭️  Step 4/9: All images rebuilt — current production tags not needed${NC}"
fi
echo ""

FRONTEND_IMAGE_TAG="${FRONTEND_GIT_TAG:-$CURRENT_FRONTEND_TAG}"
BACKEND_IMAGE_TAG="${BACKEND_GIT_TAG:-$CURRENT_BACKEND_TAG}"

# --------------------------------------------------------------------------
# Step 5/9: Push images
# --------------------------------------------------------------------------
if [ "$DEPLOY_FRONTEND" = true ] || [ "$DEPLOY_BACKEND" = true ]; then
    echo -e "${BLUE}📤 Step 5/9: Pushing images to registry...${NC}"

    if [ "$DEPLOY_FRONTEND" = true ]; then
        echo "Pushing frontend ($FRONTEND_GIT_TAG)..."
        docker push "$FRONTEND_IMAGE:$FRONTEND_GIT_TAG"
        if [ $? -ne 0 ]; then
            echo -e "${RED}❌ Failed to push frontend image${NC}"
            exit 1
        fi
    else
        echo -e "${YELLOW}Skipping frontend push (keeping current: $CURRENT_FRONTEND_TAG)${NC}"
    fi

    if [ "$DEPLOY_BACKEND" = true ]; then
        echo "Pushing backend ($BACKEND_GIT_TAG)..."
        docker push "$BACKEND_IMAGE:$BACKEND_GIT_TAG"
        if [ $? -ne 0 ]; then
            echo -e "${RED}❌ Failed to push backend image${NC}"
            exit 1
        fi
    else
        echo -e "${YELLOW}Skipping backend push (keeping current: $CURRENT_BACKEND_TAG)${NC}"
    fi

    echo -e "${GREEN}✅ Images pushed successfully${NC}"
else
    echo -e "${YELLOW}⏭️  Step 5/9: No images to push${NC}"
fi
echo ""

# --------------------------------------------------------------------------
# Step 6/9: Sync infra/ to the VM (infra target only)
# --------------------------------------------------------------------------
# rsync mirrors the deploy workflow: no --delete (runtime state living next
# to the compose files must survive), .env*/logs/alloy-secrets excluded.
# --checksum --itemize-changes tells us WHICH files actually changed so we
# can restart/rebuild only the services whose bind-mounted config changed
# (see "Bind-mount trap" in the header).
RESTART_ALLOY=0
REBUILD_BACKUP_SIDECAR=0
UP_FLAGS=""

if [ "$DEPLOY_INFRA" = true ]; then
    echo -e "${BLUE}📁 Step 6/9: Syncing infra/ to the VM...${NC}"
    cd "$SCRIPT_DIR"

    RSYNC_OUTPUT=$(rsync -az --checksum --itemize-changes \
        --exclude '.env' \
        --exclude '.env.*' \
        --exclude 'logs/' \
        --exclude 'alloy-secrets/' \
        -e "ssh -i $_VM_SSH_KEY_FILE -o StrictHostKeyChecking=no" \
        "$SCRIPT_DIR/" \
        "$_VM_SSH_USER@$_VM_SSH_HOST:$REMOTE_INFRA_DIR/")

    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to sync infra/ to the VM${NC}"
        exit 1
    fi

    # Files whose content changed on the VM (new or updated regular files)
    CHANGED_FILES=$(echo "$RSYNC_OUTPUT" | awk '$1 ~ /^[<>]f/ {print $2}')

    if [ -n "$CHANGED_FILES" ]; then
        echo "Changed files:"
        echo "$CHANGED_FILES" | sed 's/^/  • /'
    else
        echo "No file changes — compose convergence only."
    fi

    if echo "$CHANGED_FILES" | grep -qx 'alloy/config.alloy'; then
        RESTART_ALLOY=1
        echo -e "${YELLOW}  ↻ alloy/config.alloy changed → alloy will be restarted (bind-mounted config)${NC}"
    fi
    if echo "$CHANGED_FILES" | grep -q '^postgres-backup/'; then
        REBUILD_BACKUP_SIDECAR=1
        echo -e "${YELLOW}  ↻ postgres-backup/ changed → sidecar image will be rebuilt on the VM${NC}"
    fi

    UP_FLAGS="--remove-orphans"
    echo -e "${GREEN}✅ infra/ synced${NC}"
else
    echo -e "${YELLOW}⏭️  Step 6/9: Skipping infra sync${NC}"
fi
echo ""

# --------------------------------------------------------------------------
# Step 7/9: Upload runtime environment variables
# --------------------------------------------------------------------------
echo -e "${BLUE}🔐 Step 7/9: Uploading runtime environment variables...${NC}"

# Create temporary env file with image tags
TEMP_ENV_FILE=$(mktemp)
trap "rm -f $TEMP_ENV_FILE" EXIT

# Copy .env.production and set image tags (untouched services keep their
# currently deployed tags fetched in step 4)
cp "$SCRIPT_DIR/.env.production" "$TEMP_ENV_FILE"
echo "" >> "$TEMP_ENV_FILE"
echo "# Auto-generated by deployment script" >> "$TEMP_ENV_FILE"
echo "FRONTEND_IMAGE_TAG=$FRONTEND_IMAGE_TAG" >> "$TEMP_ENV_FILE"
echo "BACKEND_IMAGE_TAG=$BACKEND_IMAGE_TAG" >> "$TEMP_ENV_FILE"

# Upload to VM
echo "Uploading .env file to production VM..."
scp "${SSH_OPTS[@]}" \
    "$TEMP_ENV_FILE" \
    "$_VM_SSH_USER@$_VM_SSH_HOST:$REMOTE_INFRA_DIR/.env"

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to upload environment file${NC}"
    exit 1
fi

# Set proper permissions on remote .env file
ssh "${SSH_OPTS[@]}" \
    "$_VM_SSH_USER@$_VM_SSH_HOST" \
    "chmod 600 $REMOTE_INFRA_DIR/.env"

echo -e "${GREEN}✅ Environment variables uploaded securely${NC}"
echo ""

# Step 7b: Create Alloy database observability secrets on the VM
# The DSN is extracted from the already-uploaded .env file on the remote machine
# so that the password never appears as a command-line argument.
POSTGRES_OBS_DSN_LOCAL=$(grep "^POSTGRES_OBS_DSN=" "$SCRIPT_DIR/.env.production" | cut -d'=' -f2-)
if [ -n "$POSTGRES_OBS_DSN_LOCAL" ]; then
    echo -e "${BLUE}🔐 Step 7b: Creating Alloy database observability secrets...${NC}"

    SECRETS_SCRIPT=$(cat <<'SECRETS_SCRIPT_EOF'
#!/bin/bash
set -e
SECRETS_DIR=/opt/openmentor/infra/alloy-secrets

mkdir -p "$SECRETS_DIR"
chmod 700 "$SECRETS_DIR"

# Extract DSN from the uploaded .env and write it to the secrets file
grep "^POSTGRES_OBS_DSN=" /opt/openmentor/infra/.env | cut -d'=' -f2- | tr -d '\n' \
    > "$SECRETS_DIR/postgres_secret_openmentor"
chmod 600 "$SECRETS_DIR/postgres_secret_openmentor"

echo "Database observability secrets ready in $SECRETS_DIR"
SECRETS_SCRIPT_EOF
)

    ssh "${SSH_OPTS[@]}" \
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

# --------------------------------------------------------------------------
# Step 8/9: Deploy on the VM (pull, converge, health checks, auto-rollback)
# --------------------------------------------------------------------------
echo -e "${BLUE}🚢 Step 8/9: Deploying to production VM...${NC}"
cd "$SCRIPT_DIR"

# Create remote deployment script. Compose convergence recreates ONLY the
# services whose image tag (or definition) changed; everything else keeps
# running untouched.
DEPLOY_SCRIPT=$(cat <<'REMOTE_SCRIPT'
#!/bin/bash
set -e

ENCODED_SA_KEY="$1"
UP_FLAGS="$2"
RESTART_ALLOY="$3"
REBUILD_BACKUP_SIDECAR="$4"

# Decode the base64-encoded service account key
YANDEX_SA_KEY=$(echo "$ENCODED_SA_KEY" | base64 -d)

echo "🚀 Starting deployment on production VM..."

# The monorepo's infra/ directory is synced to /opt/openmentor/infra (by the
# infra target of deploy.sh or the deploy workflow); compose runs from there.
cd /opt/openmentor/infra

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

# Regenerate .env.runtime: what the containers read via compose `env_file`.
# It is .env WITHOUT the image-tag lines, so a tag-only deploy changes only
# the retagged service's compose config (convergence recreates nothing else).
regen_env_runtime() {
    grep -vE '^(FRONTEND_IMAGE_TAG|BACKEND_IMAGE_TAG)=' .env > .env.runtime
    chmod 600 .env.runtime
}
regen_env_runtime

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
# delete the production database. Postgres image pin bumps are safe for the
# same reason: the container is recreated, the data volume persists (minor
# versions only — major upgrades follow docs/runbooks/postgres-backup-restore.md).
echo "🗄️  Ensuring Postgres data volume exists..."
docker volume create openmentor-postgres-data

# Rebuild the backup sidecar image if its build context changed in the sync
# (it is BUILT on the VM from ./postgres-backup — `up -d` alone would keep
# running the stale image)
if [ "$REBUILD_BACKUP_SIDECAR" = "1" ]; then
    echo "🔨 postgres-backup/ changed — rebuilding sidecar image..."
    docker-compose build postgres-backup
fi

# Pull new images
echo "📦 Pulling new images..."
docker-compose pull

# Converge: compose recreates only services whose image/definition changed
echo "🔄 Converging services (docker-compose up -d $UP_FLAGS)..."
docker-compose up -d $UP_FLAGS

# Post-up guard: verify every running project container is attached to the
# compose network. Docker can (rarely - seen with a port conflict during a
# delayed image-pull start) bring a container up with no network endpoint;
# in-container healthchecks still pass while inter-service DNS fails.
# Self-heal once with a force-recreate.
for svc in $(docker-compose ps --services 2>/dev/null); do
    cid=$(docker-compose ps -q "$svc" 2>/dev/null | head -1)
    [ -n "$cid" ] || continue
    running=$(docker inspect -f '{{.State.Running}}' "$cid" 2>/dev/null)
    [ "$running" = "true" ] || continue
    nets=$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$cid")
    if [ -z "${nets// /}" ]; then
        echo "⚠️  '$svc' is running but detached from the network - force-recreating..."
        docker-compose up -d --force-recreate "$svc"
        nets=$(docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$(docker-compose ps -q "$svc" | head -1)")
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
    docker-compose restart alloy
fi

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

    # Restore backup .env file (previous image tags)
    if [ -f .env.backup ]; then
        cp .env.backup .env
        regen_env_runtime
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

DEPLOY_EXIT_CODE=0
ssh "${SSH_OPTS[@]}" \
    "$_VM_SSH_USER@$_VM_SSH_HOST" \
    "bash -s" -- "$ENCODED_SA_KEY" "$UP_FLAGS" "$RESTART_ALLOY" "$REBUILD_BACKUP_SIDECAR" <<< "$DEPLOY_SCRIPT" \
    || DEPLOY_EXIT_CODE=$?

if [ $DEPLOY_EXIT_CODE -ne 0 ]; then
    echo -e "${RED}❌ Deployment failed!${NC}"
    echo -e "${YELLOW}💡 Check the logs above for details${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Deployment successful${NC}"
echo ""

# --------------------------------------------------------------------------
# Step 9/9: Verify public endpoint
# --------------------------------------------------------------------------
echo -e "${BLUE}🔍 Step 9/9: Verifying public endpoint...${NC}"
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
if [ "$DEPLOY_INFRA" = true ]; then
    echo "  • Infra: synced (alloy restart: $RESTART_ALLOY, backup sidecar rebuild: $REBUILD_BACKUP_SIDECAR)"
fi
if [ -n "$DOMAIN" ]; then
    echo "  • URL: https://$DOMAIN"
fi
echo ""
echo "🔗 Next steps:"
echo "  1. Monitor application at https://$DOMAIN"
echo "  2. Check Grafana dashboards for metrics"
echo "  3. Verify logs in Grafana Loki"
echo ""
