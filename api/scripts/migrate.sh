#!/bin/bash
#
# Database Migration Script for Local Development
#
# Usage:
#   ./scripts/migrate.sh          # Run migrations with default .env
#   ./scripts/migrate.sh --build  # Build migrate binary first
#
# Requirements:
#   - PostgreSQL running (default: localhost:5433)
#   - .env file with DATABASE_URL configured
#

set -e  # Exit on error

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MIGRATE_BIN="$PROJECT_ROOT/bin/migrate"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🔄 Database Migration Tool${NC}"
echo "================================"

# Check if --build flag is passed
if [ "$1" == "--build" ]; then
    echo -e "${YELLOW}Building migrate binary...${NC}"
    cd "$PROJECT_ROOT"
    go build -o bin/migrate ./cmd/migrate/main.go
    echo -e "${GREEN}✓ Build complete${NC}"
fi

# Check if migrate binary exists
if [ ! -f "$MIGRATE_BIN" ]; then
    echo -e "${RED}✗ Migrate binary not found at $MIGRATE_BIN${NC}"
    echo -e "${YELLOW}Run: ./scripts/migrate.sh --build${NC}"
    exit 1
fi

# Load environment variables from .env
if [ -f "$PROJECT_ROOT/.env" ]; then
    echo -e "${GREEN}✓ Loading environment from .env${NC}"
    set -a  # automatically export all variables
    source "$PROJECT_ROOT/.env"
    set +a
else
    echo -e "${YELLOW}⚠ Warning: .env file not found, using existing environment${NC}"
fi

# Check if DATABASE_URL is set
if [ -z "$DATABASE_URL" ]; then
    echo -e "${RED}✗ DATABASE_URL not set${NC}"
    echo "Set it in .env or environment"
    exit 1
fi

# Mask password in URL for display
DISPLAY_URL=$(echo "$DATABASE_URL" | sed -E 's/:([^@:]+)@/:***@/')
echo -e "Database: ${GREEN}$DISPLAY_URL${NC}"

# Run migrations
echo -e "${YELLOW}Running migrations...${NC}"
cd "$PROJECT_ROOT"
"$MIGRATE_BIN"

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Migrations completed successfully${NC}"
else
    echo -e "${RED}✗ Migrations failed${NC}"
    exit 1
fi
