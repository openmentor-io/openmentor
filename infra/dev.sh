#!/bin/bash

# Local development startup script for OpenMentor
# This script helps developers quickly start the full stack locally

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}🚀 OpenMentor Local Development Environment${NC}"
echo ""

# Check if .env file exists
if [ ! -f .env ]; then
    echo -e "${YELLOW}⚠️  .env file not found!${NC}"
    echo "Creating .env from .env.example..."
    cp .env.example .env
    echo -e "${RED}⚠️  Please edit .env file and fill in your actual values!${NC}"
    echo "Run this script again after configuring .env"
    exit 1
fi

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}❌ Docker is not running!${NC}"
    echo "Please start Docker Desktop and try again."
    exit 1
fi

# Check if parent directory has frontend and backend repos
if [ ! -d "../openmentor" ]; then
    echo -e "${YELLOW}⚠️  Frontend repository not found at ../openmentor${NC}"
    echo "Please clone the frontend repository:"
    echo "  cd .. && git clone <frontend-repo-url> openmentor"
    exit 1
fi

if [ ! -d "../openmentor-api" ]; then
    echo -e "${YELLOW}⚠️  Backend repository not found at ../openmentor-api${NC}"
    echo "Please clone the backend repository:"
    echo "  cd .. && git clone <backend-repo-url> openmentor-api"
    exit 1
fi

echo -e "${GREEN}✅ Pre-flight checks passed${NC}"
echo ""

# Parse command
COMMAND=${1:-up}

# The base compose file declares the Postgres data volume as external
# (protects production data from `down -v`); create it idempotently so the
# merged config always resolves. The dev overlay mounts its own
# openmentor-postgres-data-dev volume instead.
ensure_postgres_volume() {
    docker volume create openmentor-postgres-data > /dev/null
}

case $COMMAND in
    up)
        echo "🔨 Building and starting services..."
        ensure_postgres_volume
        docker-compose -f docker-compose.yml -f docker-compose.dev.yml up --build
        ;;
    up-d)
        echo "🔨 Building and starting services in detached mode..."
        ensure_postgres_volume
        docker-compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
        echo ""
        echo -e "${GREEN}✅ Services started!${NC}"
        echo ""
        echo "Access your services:"
        echo "  🌐 Frontend:  http://localhost:3000"
        echo "  🔧 Backend:   http://localhost:8081/api/healthcheck"
        echo "  ⚙️  Worker:    http://localhost:8090/healthz"
        echo "  📊 Alloy:     http://localhost:12345/metrics"
        echo ""
        echo "View logs:"
        echo "  docker-compose logs -f"
        echo "  docker-compose logs -f frontend"
        echo "  docker-compose logs -f backend"
        echo "  docker-compose logs -f worker"
        echo ""
        echo "Stop services:"
        echo "  ./dev.sh down"
        ;;
    down)
        echo "🛑 Stopping services..."
        docker-compose down
        echo -e "${GREEN}✅ Services stopped${NC}"
        ;;
    restart)
        echo "🔄 Restarting services..."
        docker-compose restart
        echo -e "${GREEN}✅ Services restarted${NC}"
        ;;
    logs)
        SERVICE=${2:-}
        if [ -z "$SERVICE" ]; then
            docker-compose logs -f --tail=100
        else
            docker-compose logs -f --tail=100 $SERVICE
        fi
        ;;
    ps)
        docker-compose ps
        ;;
    clean)
        echo "🧹 Cleaning up..."
        docker-compose down -v
        docker system prune -f
        echo -e "${GREEN}✅ Cleanup complete${NC}"
        ;;
    rebuild)
        echo "🔨 Rebuilding services..."
        docker-compose down
        docker-compose -f docker-compose.yml -f docker-compose.dev.yml build --no-cache
        ensure_postgres_volume
        docker-compose -f docker-compose.yml -f docker-compose.dev.yml up -d
        echo -e "${GREEN}✅ Rebuild complete${NC}"
        ;;
    health)
        echo "🏥 Checking service health..."
        echo ""
        echo "Frontend:"
        curl -s http://localhost:3000/api/healthcheck | jq '.' || echo "❌ Failed"
        echo ""
        echo "Backend:"
        curl -s http://localhost:8081/api/healthcheck | jq '.' || echo "❌ Failed"
        echo ""
        echo "Worker:"
        curl -s http://localhost:8090/healthz | jq '.' || echo "❌ Failed"
        echo ""
        echo "PostgreSQL:"
        docker exec openmentor-postgres-dev pg_isready -U openmentor || echo "❌ Failed"
        echo ""
        echo "Alloy:"
        curl -s http://localhost:12345/metrics | grep -i alloy_build_info || echo "❌ Failed"
        ;;
    db)
        echo "🗄️  Database Information:"
        echo ""
        echo "Connection String:"
        echo "  postgresql://openmentor:password@localhost:5433/openmentor?sslmode=disable"
        echo ""
        echo "Connect with psql:"
        echo "  psql postgresql://openmentor:password@localhost:5433/openmentor"
        echo ""
        echo "Or use docker exec:"
        echo "  docker exec -it openmentor-postgres-dev psql -U openmentor"
        echo ""
        echo "Check tables:"
        echo "  docker exec -it openmentor-postgres-dev psql -U openmentor -c '\\dt'"
        ;;
    *)
        echo "Usage: ./dev.sh [command]"
        echo ""
        echo "Commands:"
        echo "  up        - Start services (attached mode, shows logs)"
        echo "  up-d      - Start services in detached mode"
        echo "  down      - Stop services"
        echo "  restart   - Restart all services"
        echo "  logs      - View logs (optionally specify service: ./dev.sh logs frontend)"
        echo "  ps        - Show service status"
        echo "  clean     - Stop services and remove volumes"
        echo "  rebuild   - Rebuild services from scratch"
        echo "  health    - Check health of all services"
        echo "  db        - Show database connection information"
        echo ""
        echo "Examples:"
        echo "  ./dev.sh up-d         # Start in background"
        echo "  ./dev.sh logs backend # View backend logs"
        echo "  ./dev.sh health       # Check if everything is running"
        echo "  ./dev.sh db           # Get database connection string"
        ;;
esac
