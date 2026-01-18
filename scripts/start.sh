#!/bin/bash
# UserEngine - Start All Services
# Usage: ./scripts/start.sh [--build]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BIN_DIR="$PROJECT_DIR/bin"
LOG_DIR="$PROJECT_DIR/logs"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration (can be overridden by environment)
REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
JWT_SECRET="${JWT_SECRET:-dev-secret-change-in-production}"
GATEWAY_ADDR="${GATEWAY_ADDR:-:8080}"
API_ADDR="${API_ADDR:-:8081}"

print_banner() {
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════════╗"
    echo "║         UserEngine Presence Engine        ║"
    echo "╚═══════════════════════════════════════════╝"
    echo -e "${NC}"
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
    echo -e "${YELLOW}Building services...${NC}"
    mkdir -p "$BIN_DIR"
    
    cd "$PROJECT_DIR"
    go build -o "$BIN_DIR/gateway" ./cmd/gateway
    go build -o "$BIN_DIR/api" ./cmd/api
    go build -o "$BIN_DIR/sweeper" ./cmd/worker-sweeper
    go build -o "$BIN_DIR/debouncer" ./cmd/worker-debouncer
    
    echo -e "${GREEN}✓ All services built${NC}"
}

start_service() {
    local name=$1
    local binary=$2
    local log_file="$LOG_DIR/${name}.log"
    
    echo -e "${BLUE}Starting $name...${NC}"
    
    # Export environment for the service
    export REDIS_ADDR
    export JWT_SECRET
    export GATEWAY_ADDR
    export API_ADDR
    
    nohup "$binary" > "$log_file" 2>&1 &
    local pid=$!
    echo $pid > "$LOG_DIR/${name}.pid"
    
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
    
    # Start services
    start_service "gateway" "$BIN_DIR/gateway"
    start_service "api" "$BIN_DIR/api"
    start_service "sweeper" "$BIN_DIR/sweeper"
    start_service "debouncer" "$BIN_DIR/debouncer"
    
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

# Parse arguments
BUILD=false
for arg in "$@"; do
    case $arg in
        --build|-b)
            BUILD=true
            ;;
    esac
done

# Check Redis
check_redis || exit 1

# Build if requested or binaries don't exist
if [ "$BUILD" = true ] || [ ! -f "$BIN_DIR/gateway" ]; then
    build_services
fi

# Start all services
start_all

