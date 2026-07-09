#!/bin/bash
set -e

# OpenMentor Local Development Deployment Script
# Builds and deploys the full stack locally for development/testing

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$SCRIPT_DIR/../web"
BACKEND_DIR="$SCRIPT_DIR/../api"

echo -e "${GREEN}🚀 OpenMentor Local Development Deployment${NC}"
echo "============================================"
echo ""

# Step 1: Pre-flight checks
echo -e "${BLUE}✓ Step 1/6: Running pre-flight checks...${NC}"

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}❌ Docker is not running!${NC}"
    echo "Please start Docker Desktop and try again."
    exit 1
fi
echo -e "${GREEN}  • Docker is running${NC}"

# Check if .env file exists
if [ ! -f "$SCRIPT_DIR/.env" ]; then
    echo -e "${YELLOW}⚠️  .env file not found!${NC}"
    echo "Creating .env from .env.example..."
    cp "$SCRIPT_DIR/.env.example" "$SCRIPT_DIR/.env"
    echo -e "${RED}⚠️  Please edit .env file and fill in your actual values!${NC}"
    echo "Run this script again after configuring .env"
    exit 1
fi
echo -e "${GREEN}  • .env file exists${NC}"

# Check if frontend directory exists (web/ of the monorepo)
if [ ! -d "$FRONTEND_DIR" ]; then
    echo -e "${RED}❌ Frontend directory not found at $FRONTEND_DIR${NC}"
    echo "Run this script from infra/ of a full monorepo checkout:"
    echo "  git clone https://github.com/openmentor-io/openmentor.git && cd openmentor/infra"
    exit 1
fi
echo -e "${GREEN}  • Frontend directory found${NC}"

# Check if backend directory exists (api/ of the monorepo)
if [ ! -d "$BACKEND_DIR" ]; then
    echo -e "${RED}❌ Backend directory not found at $BACKEND_DIR${NC}"
    echo "Run this script from infra/ of a full monorepo checkout:"
    echo "  git clone https://github.com/openmentor-io/openmentor.git && cd openmentor/infra"
    exit 1
fi
echo -e "${GREEN}  • Backend directory found${NC}"

echo -e "${GREEN}✅ All pre-flight checks passed${NC}"
echo ""

# Step 2: Load version and configuration
echo -e "${BLUE}📋 Step 2/6: Loading configuration...${NC}"

# Load version
if [ -f "$SCRIPT_DIR/version" ]; then
    source "$SCRIPT_DIR/version"
    echo -e "${GREEN}  • App version: ${APP_VERSION}${NC}"
else
    APP_VERSION="dev"
    echo -e "${YELLOW}  • Using default version: ${APP_VERSION}${NC}"
fi

# Generate image tag for local deployment
IMAGE_TAG="local-$(date +%Y%m%d-%H%M%S)"
echo -e "${GREEN}  • Image tag: ${IMAGE_TAG}${NC}"

echo ""

# Step 3: Build Frontend Image
echo -e "${BLUE}🏗️  Step 3/6: Building frontend image...${NC}"
cd "$FRONTEND_DIR"

# Load build args from .env if they exist
# Save IMAGE_TAG before sourcing to prevent it from being overwritten
SAVED_IMAGE_TAG="$IMAGE_TAG"
source "$SCRIPT_DIR/.env" 2>/dev/null || true
IMAGE_TAG="$SAVED_IMAGE_TAG"

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
NEXT_PUBLIC_APP_ENV="${NEXT_PUBLIC_APP_ENV:-production}"
NEXT_PUBLIC_ANALYTICS_PROVIDER="${NEXT_PUBLIC_ANALYTICS_PROVIDER:-posthog}"
NEXT_PUBLIC_ANALYTICS_EVENT_VERSION="${NEXT_PUBLIC_ANALYTICS_EVENT_VERSION:-v1}"
NEXT_PUBLIC_POSTHOG_KEY="${NEXT_PUBLIC_POSTHOG_KEY}"
NEXT_PUBLIC_POSTHOG_HOST="${NEXT_PUBLIC_POSTHOG_HOST}"
POSTHOG_PERSONAL_API_KEY="${POSTHOG_PERSONAL_API_KEY}"
POSTHOG_PROJECT_ID="${POSTHOG_PROJECT_ID}"

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
    --target deps \
    -t openmentor-frontend:dev \
    -t openmentor-frontend:$IMAGE_TAG \
    .

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to build frontend image${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Frontend image built successfully${NC}"
echo ""

# Step 4: Build Backend Image
echo -e "${BLUE}🏗️  Step 4/6: Building backend image...${NC}"
cd "$BACKEND_DIR"

docker build \
    --target runner \
    -t openmentor-backend:dev \
    -t openmentor-backend:$IMAGE_TAG \
    .

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to build backend image${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Backend image built successfully${NC}"
echo ""

# Step 5: Deploy with Docker Compose
echo -e "${BLUE}🚢 Step 5/8: Starting services...${NC}"
cd "$SCRIPT_DIR"

# The base compose file declares the Postgres data volume as external
# (protects production data from `down -v`); create it idempotently so the
# merged config always resolves. The dev overlay mounts its own
# openmentor-postgres-data-dev volume instead.
docker volume create openmentor-postgres-data > /dev/null

# Use docker-compose with dev overrides
docker-compose -f docker-compose.yml -f docker-compose.dev.yml up -d

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to start services${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Services started${NC}"
echo ""

# Step 6: Wait for PostgreSQL
echo -e "${BLUE}🗄️  Step 6/8: Waiting for PostgreSQL...${NC}"
echo "Checking PostgreSQL readiness..."

POSTGRES_HEALTHY=0
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

if [ $POSTGRES_HEALTHY -eq 0 ]; then
    echo -e "${RED}❌ PostgreSQL failed to start${NC}"
    echo "Check logs: docker-compose logs postgres"
    exit 1
fi
echo -e "${GREEN}✅ PostgreSQL is ready${NC}"
echo ""

# Step 7: Database migrations
echo -e "${BLUE}📊 Step 7/8: Database migrations...${NC}"
echo "Note: Migrations run automatically when backend starts"
echo "Check backend logs if you need to verify migration status:"
echo "  docker-compose logs backend | grep -i migration"
echo -e "${GREEN}✅ Migrations will be applied on backend startup${NC}"
echo ""

# Step 8: Health checks and verification
echo -e "${BLUE}🏥 Step 8/8: Verifying deployment...${NC}"
echo "Waiting for application services to be ready..."
sleep 5

# Show service status
echo ""
echo -e "${BLUE}📊 Service Status:${NC}"
docker-compose ps
echo ""

# Check health endpoints
echo -e "${BLUE}🏥 Health Checks:${NC}"

FRONTEND_HEALTHY=0
BACKEND_HEALTHY=0
WORKER_HEALTHY=0

# Check frontend (with retries)
for i in {1..6}; do
    echo -n "  • Checking frontend health (attempt $i/6)... "
    if curl -f -s http://localhost:3000/api/healthcheck > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        FRONTEND_HEALTHY=1
        break
    else
        echo -e "${YELLOW}waiting...${NC}"
        sleep 5
    fi
done

if [ $FRONTEND_HEALTHY -eq 0 ]; then
    echo -e "    ${RED}✗ Frontend health check failed${NC}"
fi

# Check backend (with retries)
for i in {1..6}; do
    echo -n "  • Checking backend health (attempt $i/6)... "
    if curl -f -s http://localhost:8081/api/healthcheck > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        BACKEND_HEALTHY=1
        break
    else
        echo -e "${YELLOW}waiting...${NC}"
        sleep 5
    fi
done

if [ $BACKEND_HEALTHY -eq 0 ]; then
    echo -e "    ${RED}✗ Backend health check failed${NC}"
fi

# Check worker (with retries)
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

if [ $WORKER_HEALTHY -eq 0 ]; then
    echo -e "    ${RED}✗ Worker health check failed${NC}"
fi

echo ""

# Final summary
if [ $FRONTEND_HEALTHY -eq 1 ] && [ $BACKEND_HEALTHY -eq 1 ] && [ $WORKER_HEALTHY -eq 1 ]; then
    echo -e "${GREEN}════════════════════════════════════════${NC}"
    echo -e "${GREEN}✨ Deployment completed successfully! ✨${NC}"
    echo -e "${GREEN}════════════════════════════════════════${NC}"
else
    echo -e "${YELLOW}════════════════════════════════════════${NC}"
    echo -e "${YELLOW}⚠️  Deployment completed with warnings${NC}"
    echo -e "${YELLOW}════════════════════════════════════════${NC}"
fi

echo ""
echo "📋 Deployment Summary:"
echo "  • Image Tag: $IMAGE_TAG"
echo "  • App Version: $APP_VERSION"
echo "  • Environment: development"
echo ""
echo "🌐 Access Services:"
echo "  • Frontend:  http://localhost:3000"
echo "  • Backend:   http://localhost:8081/api/healthcheck"
echo "  • Worker:    http://localhost:8090/healthz"
echo "  • Alloy:     http://localhost:12345/metrics"
echo ""
echo "🗄️  Database Connection:"
echo "  • Host:      localhost"
echo "  • Port:      5433"
echo "  • Database:  openmentor"
echo "  • User:      openmentor"
echo "  • Password:  password"
echo ""
echo "📋 Database Connection String (for data import):"
echo -e "${GREEN}postgresql://openmentor:password@localhost:5433/openmentor?sslmode=disable${NC}"
echo ""
echo "💡 Connect with psql:"
echo "  psql postgresql://openmentor:password@localhost:5433/openmentor"
echo ""
echo "📝 Useful Commands:"
echo "  • View logs:       docker-compose logs -f"
echo "  • View frontend:   docker-compose logs -f frontend"
echo "  • View backend:    docker-compose logs -f backend"
echo "  • View worker:     docker-compose logs -f worker"
echo "  • View postgres:   docker-compose logs -f postgres"
echo "  • Stop services:   docker-compose down"
echo "  • Restart:         ./deploy-dev.sh"
echo "  • Access DB:       docker exec -it openmentor-postgres-dev psql -U openmentor"
echo ""

# Show warnings if health checks failed
if [ $FRONTEND_HEALTHY -eq 0 ] || [ $BACKEND_HEALTHY -eq 0 ] || [ $WORKER_HEALTHY -eq 0 ]; then
    echo -e "${YELLOW}⚠️  Some services may not be healthy. Check logs:${NC}"
    echo "  docker-compose logs -f"
    echo ""
fi
