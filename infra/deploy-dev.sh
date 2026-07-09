#!/bin/bash
set -e

# ============================================================================
# OpenMentor Local Development Deployment Script
# ============================================================================
# Same CLI and flow as ./deploy.sh, but the target is the LOCAL docker daemon
# instead of the production VM: images are built with local dev-<sha> tags
# (no registry push), the tags are written to the local infra/.env, and the
# dev compose stack (docker-compose.yml + docker-compose.dev.yml) converges
# exactly like production does — only services whose image tag changed are
# recreated.
#
#   ./deploy-dev.sh [targets...] [options]
#
# Targets (default: frontend backend):
#   frontend   build ../web as openmentor-frontend:dev-<sha>, roll frontend
#   backend    build ../api as openmentor-backend:dev-<sha>, roll
#              migrate + backend + worker (one image, three services; migrate
#              runs before backend/worker via depends_on)
#   infra      converge compose-level changes (`up -d --remove-orphans`) —
#              the "sync" is a no-op locally (the files are already here) —
#              and restart services whose bind-mounted config changed
#   all        frontend backend infra
#
# Options:
#   --tag TAG    use TAG for built images instead of dev-<git sha>
#   --yes, -y    skip the confirmation prompt
#   --dry-run    print the deployment plan and exit without doing anything
#   -h, --help   show help
#
# Bind-mount trap (infra target): compose does not react to changes in
# bind-mounted config files. The only file-config service is alloy
# (./alloy/config.alloy); locally we restart it when the file is newer than
# the running container (alloy only runs with `--profile observability`).
#
# The stack mirrors production: traefik (HTTP-only on :80) / frontend /
# backend / worker / migrate / postgres (dev creds, host :5433). alloy and
# cadvisor are opt-in via `--profile observability`; postgres-backup never
# runs in dev. See docker-compose.dev.yml for the full parity notes.
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
    echo "  frontend           Build + deploy the frontend image locally"
    echo "  backend            Build + deploy the backend image locally (backend/worker/migrate)"
    echo "  infra              Converge compose-level changes of the local stack"
    echo "  all                frontend backend infra"
    echo ""
    echo "Options:"
    echo "  --tag TAG          Use TAG for built images instead of dev-<git sha>"
    echo "  --yes, -y          Skip the confirmation prompt"
    echo "  --dry-run          Print the deployment plan and exit"
    echo "  -h, --help         Show this help message"
    echo ""
    echo "Services not being deployed keep their current image tags (from .env)."
}

# --------------------------------------------------------------------------
# Parse command line arguments (same CLI as deploy.sh, minus --staging)
# --------------------------------------------------------------------------
DEPLOY_FRONTEND=false
DEPLOY_BACKEND=false
DEPLOY_INFRA=false
TARGETS_GIVEN=false
TAG_OVERRIDE=""
SKIP_CONFIRM=false
DRY_RUN=false

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
# Configuration
# --------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$SCRIPT_DIR/../web"
BACKEND_DIR="$SCRIPT_DIR/../api"
ENV_FILE="$SCRIPT_DIR/.env"

COMPOSE=(docker compose -f "$SCRIPT_DIR/docker-compose.yml" -f "$SCRIPT_DIR/docker-compose.dev.yml")

# Read a KEY=value from the local .env (empty string when absent)
env_get() {
    grep "^$1=" "$ENV_FILE" 2>/dev/null | head -n1 | cut -d'=' -f2-
}

# Regenerate .env.runtime: what the containers read via compose `env_file`.
# It is .env WITHOUT the image-tag lines, so a tag-only deploy changes only
# the retagged service's compose config (convergence recreates nothing else).
regen_env_runtime() {
    grep -vE '^(FRONTEND_IMAGE_TAG|BACKEND_IMAGE_TAG)=' "$ENV_FILE" > "$SCRIPT_DIR/.env.runtime"
}

# Set KEY=value in the local .env (replace existing line or append)
env_set() {
    local key="$1" value="$2"
    if grep -q "^${key}=" "$ENV_FILE"; then
        # BSD/GNU sed compatible in-place edit
        sed -i.sedbak "s|^${key}=.*|${key}=${value}|" "$ENV_FILE" && rm -f "$ENV_FILE.sedbak"
    else
        printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
    fi
}

echo -e "${GREEN}🚀 OpenMentor Local Development Deployment${NC}"
echo "============================================"
echo ""

# --------------------------------------------------------------------------
# Pre-flight checks (the local equivalent of credential validation)
# --------------------------------------------------------------------------
# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}❌ Docker is not running!${NC}"
    echo "Please start Docker Desktop and try again."
    exit 1
fi

# Check that the monorepo's frontend and backend directories are present
if [ ! -d "$FRONTEND_DIR" ]; then
    echo -e "${RED}❌ Frontend directory not found at $FRONTEND_DIR${NC}"
    echo "Run this script from infra/ of a full monorepo checkout:"
    echo "  git clone https://github.com/openmentor-io/openmentor.git && cd openmentor/infra"
    exit 1
fi

if [ ! -d "$BACKEND_DIR" ]; then
    echo -e "${RED}❌ Backend directory not found at $BACKEND_DIR${NC}"
    echo "Run this script from infra/ of a full monorepo checkout:"
    echo "  git clone https://github.com/openmentor-io/openmentor.git && cd openmentor/infra"
    exit 1
fi

# Create .env with dev defaults if absent (NEVER commit it — gitignored).
# Placeholders are fine for a booting stack; fill in S3/SES/PostHog values
# for full functionality.
if [ ! -f "$ENV_FILE" ]; then
    echo -e "${YELLOW}⚠️  .env not found — creating it from .env.example with dev defaults${NC}"
    cp "$SCRIPT_DIR/.env.example" "$ENV_FILE"
    env_set "DOMAIN" "localhost"
    env_set "APP_ENV" "development"
    env_set "LOG_LEVEL" "debug"
    env_set "NEXT_PUBLIC_APP_ENV" "development"
    env_set "JWT_SECRET" "$(openssl rand -hex 32)"
    env_set "WORKER_AUTH_TOKEN" "$(openssl rand -hex 32)"
    echo -e "${GREEN}  • .env created (dev defaults + generated JWT/worker secrets)${NC}"
    echo -e "${YELLOW}  • Fill in S3/SES/PostHog values in .env for full functionality${NC}"
fi

# Load app version
if [ -f "$SCRIPT_DIR/version" ]; then
    source "$SCRIPT_DIR/version"
else
    APP_VERSION="dev"
fi

# --------------------------------------------------------------------------
# Image tags: dev-<monorepo short sha> (real, unique tags — convergence works
# exactly like production; see DOCKER_TAG_POLICY.md). --tag overrides.
# --------------------------------------------------------------------------
GIT_SHA=$(git -C "$SCRIPT_DIR" rev-parse --short HEAD 2>/dev/null || date +%Y%m%d-%H%M%S)
if [ "$DEPLOY_FRONTEND" = true ]; then
    FRONTEND_GIT_TAG="${TAG_OVERRIDE:-dev-$GIT_SHA}"
fi
if [ "$DEPLOY_BACKEND" = true ]; then
    BACKEND_GIT_TAG="${TAG_OVERRIDE:-dev-$GIT_SHA}"
fi

# --------------------------------------------------------------------------
# Deployment plan
# --------------------------------------------------------------------------
echo "Target: local docker (dev compose stack)"
echo "App version: $APP_VERSION"
echo ""
echo "Deployment plan:"
if [ "$DEPLOY_FRONTEND" = true ]; then
    echo -e "  • frontend: BUILD + DEPLOY (${BLUE}openmentor-frontend:$FRONTEND_GIT_TAG${NC})"
else
    echo -e "  • frontend: ${YELLOW}keep current tag${NC}"
fi
if [ "$DEPLOY_BACKEND" = true ]; then
    echo -e "  • backend:  BUILD + DEPLOY (${BLUE}openmentor-backend:$BACKEND_GIT_TAG${NC}) — backend + worker + migrate"
else
    echo -e "  • backend:  ${YELLOW}keep current tag${NC}"
fi
if [ "$DEPLOY_INFRA" = true ]; then
    echo -e "  • infra:    converge compose (up -d --remove-orphans) + restart bind-mount-config services"
else
    echo -e "  • infra:    ${YELLOW}skip${NC}"
fi
echo ""

if [ "$DRY_RUN" = true ]; then
    echo -e "${YELLOW}--dry-run: stopping here, nothing was executed.${NC}"
    exit 0
fi

if [ "$SKIP_CONFIRM" = false ]; then
    read -p "$(echo -e ${YELLOW}Do you want to proceed with deployment? \(yes/no\):${NC} )" -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        echo -e "${YELLOW}Deployment cancelled${NC}"
        exit 0
    fi
fi

# --------------------------------------------------------------------------
# Step 1/7: Build frontend image
# --------------------------------------------------------------------------
if [ "$DEPLOY_FRONTEND" = true ]; then
    echo -e "${BLUE}🏗️  Step 1/7: Building frontend image...${NC}"
    cd "$FRONTEND_DIR"

    # Load NEXT_PUBLIC_* build args from .env (same mechanism as deploy.sh
    # uses with .env.production). Preserve computed tags across the source.
    SAVED_FRONTEND_TAG="$FRONTEND_GIT_TAG"
    SAVED_BACKEND_TAG="$BACKEND_GIT_TAG"
    source "$ENV_FILE" 2>/dev/null || true
    FRONTEND_GIT_TAG="$SAVED_FRONTEND_TAG"
    BACKEND_GIT_TAG="$SAVED_BACKEND_TAG"

    NEXT_PUBLIC_GO_API_URL="${NEXT_PUBLIC_GO_API_URL:-http://backend:8081}"
    NEXT_PUBLIC_RECAPTCHA_V2_SITE_KEY="${NEXT_PUBLIC_RECAPTCHA_V2_SITE_KEY}"
    NEXT_PUBLIC_S3_STORAGE_ENDPOINT="${NEXT_PUBLIC_S3_STORAGE_ENDPOINT:-s3.eu-central-1.amazonaws.com}"
    NEXT_PUBLIC_S3_STORAGE_BUCKET="${NEXT_PUBLIC_S3_STORAGE_BUCKET:-mentor-images}"
    NEXT_PUBLIC_CDN_ENDPOINT="${NEXT_PUBLIC_CDN_ENDPOINT:-}"
    NEXT_PUBLIC_O11Y_SERVICE_NAMESPACE="${NEXT_PUBLIC_O11Y_SERVICE_NAMESPACE:-openmentor-frontend}"
    NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION="${NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION:-$APP_VERSION}"
    NEXT_PUBLIC_FARO_APP_NAME="${NEXT_PUBLIC_FARO_APP_NAME:-openmentor-frontend}"
    NEXT_PUBLIC_FARO_COLLECTOR_URL="${NEXT_PUBLIC_FARO_COLLECTOR_URL}"
    NEXT_PUBLIC_FARO_SAMPLE_RATE="${NEXT_PUBLIC_FARO_SAMPLE_RATE:-0.5}"
    NEXT_PUBLIC_APP_ENV="${NEXT_PUBLIC_APP_ENV:-development}"
    NEXT_PUBLIC_ANALYTICS_PROVIDER="${NEXT_PUBLIC_ANALYTICS_PROVIDER:-posthog}"
    NEXT_PUBLIC_ANALYTICS_EVENT_VERSION="${NEXT_PUBLIC_ANALYTICS_EVENT_VERSION:-v1}"
    NEXT_PUBLIC_POSTHOG_KEY="${NEXT_PUBLIC_POSTHOG_KEY}"
    NEXT_PUBLIC_POSTHOG_HOST="${NEXT_PUBLIC_POSTHOG_HOST}"

    echo "Building with configuration:"
    echo "  • API URL: $NEXT_PUBLIC_GO_API_URL"
    echo "  • Environment: $NEXT_PUBLIC_APP_ENV"
    echo "  • Analytics provider: $NEXT_PUBLIC_ANALYTICS_PROVIDER"

    docker build \
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
        --build-arg NEXT_PUBLIC_APP_ENV="$NEXT_PUBLIC_APP_ENV" \
        --build-arg NEXT_PUBLIC_ANALYTICS_PROVIDER="$NEXT_PUBLIC_ANALYTICS_PROVIDER" \
        --build-arg NEXT_PUBLIC_ANALYTICS_EVENT_VERSION="$NEXT_PUBLIC_ANALYTICS_EVENT_VERSION" \
        -t "openmentor-frontend:$FRONTEND_GIT_TAG" \
        .

    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to build frontend image${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Frontend image built${NC}"
else
    echo -e "${YELLOW}⏭️  Step 1/7: Skipping frontend build${NC}"
fi
echo ""

# --------------------------------------------------------------------------
# Step 2/7: Build backend image
# --------------------------------------------------------------------------
if [ "$DEPLOY_BACKEND" = true ]; then
    echo -e "${BLUE}🏗️  Step 2/7: Building backend image...${NC}"
    cd "$BACKEND_DIR"

    docker build \
        --target runner \
        -t "openmentor-backend:$BACKEND_GIT_TAG" \
        .

    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Failed to build backend image${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ Backend image built${NC}"
else
    echo -e "${YELLOW}⏭️  Step 2/7: Skipping backend build${NC}"
fi
echo ""

# --------------------------------------------------------------------------
# Step 3/7: Resolve current tags for services not being deployed
# --------------------------------------------------------------------------
if [ "$DEPLOY_FRONTEND" = false ] || [ "$DEPLOY_BACKEND" = false ]; then
    echo -e "${BLUE}📡 Step 3/7: Resolving current image tags from .env...${NC}"
    CURRENT_FRONTEND_TAG=$(env_get "FRONTEND_IMAGE_TAG")
    CURRENT_BACKEND_TAG=$(env_get "BACKEND_IMAGE_TAG")

    if [ "$DEPLOY_FRONTEND" = false ] && [ -z "$CURRENT_FRONTEND_TAG" ]; then
        echo -e "${RED}❌ FRONTEND_IMAGE_TAG not set in .env — no current frontend to keep${NC}"
        echo "Run './deploy-dev.sh all' (or include the frontend target) first."
        exit 1
    fi
    if [ "$DEPLOY_BACKEND" = false ] && [ -z "$CURRENT_BACKEND_TAG" ]; then
        echo -e "${RED}❌ BACKEND_IMAGE_TAG not set in .env — no current backend to keep${NC}"
        echo "Run './deploy-dev.sh all' (or include the backend target) first."
        exit 1
    fi

    echo -e "${GREEN}✅ Current tags:${NC}"
    echo "  • Frontend: ${CURRENT_FRONTEND_TAG:-<being deployed>}"
    echo "  • Backend: ${CURRENT_BACKEND_TAG:-<being deployed>}"
else
    echo -e "${YELLOW}⏭️  Step 3/7: All images rebuilt — current tags not needed${NC}"
fi
echo ""

FRONTEND_IMAGE_TAG="${FRONTEND_GIT_TAG:-$CURRENT_FRONTEND_TAG}"
BACKEND_IMAGE_TAG="${BACKEND_GIT_TAG:-$CURRENT_BACKEND_TAG}"

# --------------------------------------------------------------------------
# Step 4/7: Update local .env with the image tags (backup kept for rollback)
# --------------------------------------------------------------------------
echo -e "${BLUE}🔐 Step 4/7: Updating image tags in .env...${NC}"
cp "$ENV_FILE" "$ENV_FILE.backup"
env_set "FRONTEND_IMAGE_TAG" "$FRONTEND_IMAGE_TAG"
env_set "BACKEND_IMAGE_TAG" "$BACKEND_IMAGE_TAG"
regen_env_runtime
echo "  • FRONTEND_IMAGE_TAG=$FRONTEND_IMAGE_TAG"
echo "  • BACKEND_IMAGE_TAG=$BACKEND_IMAGE_TAG"
echo -e "${GREEN}✅ .env updated (previous version in .env.backup; .env.runtime regenerated)${NC}"
echo ""

# --------------------------------------------------------------------------
# Step 5/7: Infra convergence flags (local "sync" is a no-op)
# --------------------------------------------------------------------------
UP_FLAGS=""
RESTART_ALLOY=0
if [ "$DEPLOY_INFRA" = true ]; then
    echo -e "${BLUE}📁 Step 5/7: Infra target — compose-level changes will converge...${NC}"
    UP_FLAGS="--remove-orphans"

    # Bind-mount trap: alloy's config is a bind-mounted file compose won't
    # react to. Restart alloy if it is running an older config than on disk
    # (alloy only runs when the `observability` profile is enabled).
    ALLOY_ID=$("${COMPOSE[@]}" ps -q alloy 2>/dev/null || true)
    if [ -n "$ALLOY_ID" ]; then
        ALLOY_STARTED=$(docker inspect -f '{{.State.StartedAt}}' "$ALLOY_ID" 2>/dev/null || echo "")
        if [ -n "$ALLOY_STARTED" ]; then
            ALLOY_STARTED_EPOCH=$(date -d "$ALLOY_STARTED" +%s 2>/dev/null || \
                date -j -f "%Y-%m-%dT%H:%M:%S" "${ALLOY_STARTED%%.*}" +%s 2>/dev/null || echo 0)
            CONFIG_MTIME=$(stat -f %m "$SCRIPT_DIR/alloy/config.alloy" 2>/dev/null || \
                stat -c %Y "$SCRIPT_DIR/alloy/config.alloy" 2>/dev/null || echo 0)
            if [ "$CONFIG_MTIME" -gt "$ALLOY_STARTED_EPOCH" ]; then
                RESTART_ALLOY=1
                echo -e "${YELLOW}  ↻ alloy/config.alloy is newer than the running alloy → will restart it${NC}"
            fi
        fi
    fi
    echo -e "${GREEN}✅ Infra convergence prepared${NC}"
else
    echo -e "${YELLOW}⏭️  Step 5/7: Skipping infra convergence${NC}"
fi
echo ""

# --------------------------------------------------------------------------
# Step 6/7: Deploy (converge the compose stack)
# --------------------------------------------------------------------------
echo -e "${BLUE}🚢 Step 6/7: Converging the dev stack...${NC}"
cd "$SCRIPT_DIR"

# The base compose file declares the Postgres data volume as external
# (protects production data from `down -v`); create it idempotently so the
# merged config always resolves. The dev overlay mounts its own
# openmentor-postgres-data-dev volume instead. Postgres image pin bumps are
# safe: the container is recreated, data volumes persist (minor versions
# only — major upgrades follow ../docs/runbooks/postgres-backup-restore.md).
docker volume create openmentor-postgres-data > /dev/null

# Converge: compose recreates ONLY the services whose image tag (or
# definition) changed — same semantics as the production deploy.
"${COMPOSE[@]}" up -d $UP_FLAGS

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to start services${NC}"
    exit 1
fi

if [ "$RESTART_ALLOY" = "1" ]; then
    echo "↻ Restarting alloy (bind-mounted config changed)..."
    "${COMPOSE[@]}" restart alloy
fi

echo -e "${GREEN}✅ Services converged${NC}"
echo ""
echo -e "${BLUE}📊 Service Status:${NC}"
"${COMPOSE[@]}" ps
echo ""

# --------------------------------------------------------------------------
# Step 7/7: Health checks (+ automatic rollback to previous tags on failure)
# --------------------------------------------------------------------------
echo -e "${BLUE}🏥 Step 7/7: Verifying deployment...${NC}"

POSTGRES_HEALTHY=0
FRONTEND_HEALTHY=0
BACKEND_HEALTHY=0
WORKER_HEALTHY=0
TRAEFIK_HEALTHY=0

# Postgres first — everything depends on it
for i in {1..30}; do
    echo -n "  • Checking PostgreSQL (attempt $i/30)... "
    if docker exec openmentor-postgres-dev pg_isready -U openmentor > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        POSTGRES_HEALTHY=1
        break
    else
        echo -e "${YELLOW}waiting...${NC}"
        sleep 2
    fi
done

# Backend (with retries)
for i in {1..12}; do
    echo -n "  • Checking backend health (attempt $i/12)... "
    if curl -f -s http://localhost:8081/api/healthcheck > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        BACKEND_HEALTHY=1
        break
    else
        echo -e "${YELLOW}waiting...${NC}"
        sleep 5
    fi
done

# Worker (with retries)
for i in {1..6}; do
    echo -n "  • Checking worker health (attempt $i/6)... "
    if curl -f -s http://localhost:8090/healthz > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        WORKER_HEALTHY=1
        break
    else
        echo -e "${YELLOW}waiting...${NC}"
        sleep 5
    fi
done

# Frontend, direct port (with retries)
for i in {1..12}; do
    echo -n "  • Checking frontend health (attempt $i/12)... "
    if curl -f -s http://localhost:3000/api/healthcheck > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        FRONTEND_HEALTHY=1
        break
    else
        echo -e "${YELLOW}waiting...${NC}"
        sleep 5
    fi
done

# Frontend via traefik (HTTP-only localhost routing)
for i in {1..3}; do
    echo -n "  • Checking traefik route http://localhost/ (attempt $i/3)... "
    if curl -f -s http://localhost/api/healthcheck > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        TRAEFIK_HEALTHY=1
        break
    else
        echo -e "${YELLOW}waiting...${NC}"
        sleep 3
    fi
done

echo ""

# Automatic rollback to the previous tags if a core service is unhealthy
if [ $POSTGRES_HEALTHY -eq 0 ] || [ $FRONTEND_HEALTHY -eq 0 ] || \
   [ $BACKEND_HEALTHY -eq 0 ] || [ $WORKER_HEALTHY -eq 0 ]; then
    echo -e "${RED}❌ Health checks failed${NC}"
    if [ -f "$ENV_FILE.backup" ] && ! cmp -s "$ENV_FILE" "$ENV_FILE.backup"; then
        echo -e "${YELLOW}🔄 ROLLING BACK to previous image tags (.env.backup)...${NC}"
        cp "$ENV_FILE.backup" "$ENV_FILE"
        regen_env_runtime
        "${COMPOSE[@]}" up -d $UP_FLAGS
        echo -e "${YELLOW}Previous .env restored and stack re-converged.${NC}"
    fi
    echo "Check logs: ${COMPOSE[*]} logs -f"
    exit 1
fi

echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "${GREEN}✨ Deployment completed successfully! ✨${NC}"
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo ""
echo "📋 Deployment Summary:"
echo "  • Frontend: openmentor-frontend:$FRONTEND_IMAGE_TAG"
echo "  • Backend: openmentor-backend:$BACKEND_IMAGE_TAG"
echo "  • App Version: $APP_VERSION"
echo "  • Environment: development"
if [ $TRAEFIK_HEALTHY -eq 0 ]; then
    echo -e "  • ${YELLOW}⚠️  traefik route http://localhost/ did not answer (direct ports work)${NC}"
fi
echo ""
echo "🌐 Access Services:"
echo "  • Frontend (traefik):  http://localhost/"
echo "  • Frontend (direct):   http://localhost:3000"
echo "  • Backend:             http://localhost:8081/api/healthcheck"
echo "  • Worker:              http://localhost:8090/healthz"
echo ""
echo "🗄️  Database (dev credentials, host port 5433):"
echo -e "  ${GREEN}postgresql://openmentor:password@localhost:5433/openmentor?sslmode=disable${NC}"
echo "  psql: docker exec -it openmentor-postgres-dev psql -U openmentor"
echo ""
echo "📝 Useful Commands (compose = docker compose -f docker-compose.yml -f docker-compose.dev.yml):"
echo "  • View logs:        docker compose logs -f [frontend|backend|worker|postgres]"
echo "  • Service status:   docker compose ps"
echo "  • Stop services:    docker compose down"
echo "  • Reset dev data:   docker compose down && docker volume rm openmentor-postgres-data-dev"
echo "  • Observability:    add '--profile observability' to run alloy + cadvisor"
echo "                      (needs real GCLOUD_* creds and alloy-secrets/ — see docker-compose.dev.yml)"
echo "  • Redeploy:         ./deploy-dev.sh [frontend|backend|infra|all]"
echo ""
