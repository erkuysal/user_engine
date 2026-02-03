#!/bin/bash
# UserEngine - Start All Services
# Usage: ./scripts/start.sh [--build] [--production]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BIN_DIR="$PROJECT_DIR/bin"
# Use USERENGINE_LOG_DIR if set (from ops.sh), otherwise default to local logs
LOG_DIR="${USERENGINE_LOG_DIR:-$PROJECT_DIR/logs}"

# Parse arguments
BUILD=false
PRODUCTION=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --build)
            BUILD=true
            shift
            ;;
        --production)
            PRODUCTION=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--build] [--production]"
            exit 1
            ;;
    esac
done

# Set environment mode
if [ "$PRODUCTION" = true ]; then
    export ENVIRONMENT="production"
    echo -e "${RED}Running in PRODUCTION mode${NC}"
else
    export ENVIRONMENT="${ENVIRONMENT:-development}"
    echo -e "${GREEN}Running in $ENVIRONMENT mode${NC}"
fi

# Load .env file if it exists
if [ -f "$PROJECT_DIR/.env" ]; then
    echo "Loading configuration from .env"
    set -a
    source "$PROJECT_DIR/.env"
    set +a
    # Restore ENVIRONMENT if we set it explicitly
    if [ "$PRODUCTION" = true ]; then
        export ENVIRONMENT="production"
    fi
fi

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration (can be overridden by environment)
REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
JWT_SECRET="${JWT_SECRET:-dev-secret-change-in-production}"
GATEWAY_ADDR="${GATEWAY_ADDR:-:8080}"
API_ADDR="${API_ADDR:-:8081}"

# Validate production configuration
if [ "$ENVIRONMENT" = "production" ]; then
    echo -e "${YELLOW}Validating production configuration...${NC}"
    
    if [[ "$JWT_SECRET" == dev-secret* ]]; then
        echo -e "${RED}❌ Error: JWT_SECRET must be changed for production${NC}"
        echo -e "${YELLOW}   Set a secure JWT_SECRET in your .env file${NC}"
        exit 1
    fi
    
    if [[ "${CORS_ALLOW_ALL,,}" == "true" ]]; then
        echo -e "${RED}❌ Error: CORS_ALLOW_ALL=true is not allowed in production${NC}"
        exit 1
    fi
    
    if [ -z "$CORS_ALLOWED_ORIGINS" ]; then
        echo -e "${RED}❌ Error: CORS_ALLOWED_ORIGINS must be set in production${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✓ Production configuration is valid${NC}"
fi

print_banner() {
    local env_color=$GREEN
    if [ "$ENVIRONMENT" == "production" ]; then
        env_color=$RED
    fi
    
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════════╗"
    echo "║         UserEngine Presence Engine        ║"
    echo "╚═══════════════════════════════════════════╝"
    echo -e "${NC}"
    echo -e "  ${YELLOW}Environment:${NC} ${env_color}$ENVIRONMENT${NC}"
    echo ""
}

check_redis() {
    echo -e "${YELLOW}Checking Redis connection...${NC}"
    if command -v redis-cli &> /dev/null; then
        if redis-cli -h "${REDIS_ADDR%:*}" -p "${REDIS_ADDR#*:}" ping &> /dev/null; then
            echo -e "${GREEN}✓ Redis is running at $REDIS_ADDR${NC}"
            return 0
        fi
    fi
    echo -e "${RED}✗ Redis not available at $REDIS_ADDR${NC}"
    echo -e "${YELLOW}  Please start Redis first:${NC}"
    echo "    docker run -d -p 6379:6379 redis:7-alpine"
    echo "    # or"
    echo "    redis-server"
    return 1
}

build_services() {
    echo -e "${YELLOW}Building services (via PowerShell)...${NC}"
    mkdir -p "$BIN_DIR"
    
    cd "$PROJECT_DIR"
    # Use powershell to invoke go build, creating Windows executables (.exe)
    powershell.exe -Command "go build -o bin/gateway.exe ./cmd/gateway"
    powershell.exe -Command "go build -o bin/api.exe ./cmd/api"
    powershell.exe -Command "go build -o bin/sweeper.exe ./cmd/worker-sweeper"
    powershell.exe -Command "go build -o bin/debouncer.exe ./cmd/worker-debouncer"
    
    echo -e "${GREEN}✓ All services built${NC}"
}

start_service() {
    local name=$1
    local binary=$2
    local log_file="$LOG_DIR/${name}.log"
    local pid_file="$LOG_DIR/${name}.pid"
    
    # Check if already running (Windows check via PowerShell)
    if powershell.exe -Command "Get-Process -Name '$name' -ErrorAction SilentlyContinue" > /dev/null 2>&1; then
        echo -e "${YELLOW}○ $name is already running (checked via PowerShell)${NC}"
        # If PID file exists but process is controlled by Windows, just keep it or ignore it.
        # But if it's stale, we might want to update it?
        # For now, just return 0 to skip starting.
        return 0
    fi
    
    # Check PID file (legacy/fallback, mostly to clean up stale files)
    if [ -f "$pid_file" ]; then
        rm -f "$pid_file"
    fi
    
    echo -e "${BLUE}Starting $name...${NC}"
    
    # Export environment for the service
    export REDIS_ADDR
    export JWT_SECRET
    export GATEWAY_ADDR
    export API_ADDR
    # Ensure Windows processes inherit these environment variables
    export WSLENV=REDIS_ADDR:JWT_SECRET:GATEWAY_ADDR:API_ADDR
    
    nohup "$binary" > "$log_file" 2>&1 &
    local pid=$!
    echo $pid > "$pid_file"
    
    sleep 0.5
    if kill -0 $pid 2>/dev/null; then
        echo -e "${GREEN}✓ $name started (PID: $pid)${NC}"
        return 0
    else
        echo -e "${RED}✗ $name failed to start${NC}"
        cat "$log_file"
        return 1
    fi
}

start_all() {
    mkdir -p "$LOG_DIR"
    
    # Start services (using .exe binaries)
    start_service "gateway" "$BIN_DIR/gateway.exe"
    start_service "api" "$BIN_DIR/api.exe"
    start_service "sweeper" "$BIN_DIR/sweeper.exe"
    start_service "debouncer" "$BIN_DIR/debouncer.exe"
    
    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════${NC}"
    echo -e "${GREEN}  All services started successfully!${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════${NC}"
    echo ""
    echo -e "  ${BLUE}WebSocket Gateway:${NC} ws://localhost${GATEWAY_ADDR}/ws"
    echo -e "  ${BLUE}REST API:${NC}          http://localhost${API_ADDR}"
    echo ""
    echo -e "  ${YELLOW}Logs:${NC} $LOG_DIR/"
    echo -e "  ${YELLOW}Stop:${NC} ./scripts/stop.sh"
    echo ""
}

# Main
print_banner

# Check Redis
check_redis || exit 1

# Build if requested or binaries don't exist
if [ "$BUILD" = true ] || [ ! -f "$BIN_DIR/gateway.exe" ]; then
    build_services
fi

# Start all services
start_all

# Keep running in foreground by default when called from Make
echo -e "${YELLOW}Services started. Press Ctrl+C to stop...${NC}"
trap "./scripts/stop.sh; exit 0" SIGINT SIGTERM
wait

