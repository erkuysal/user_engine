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
    mkdir -p "$BIN_DIR"
    cd "$PROJECT_DIR"
    
    # Check if Go is available natively in WSL
    if command -v go &> /dev/null; then
        echo -e "${YELLOW}Building services (native Linux binaries)...${NC}"
        go build -o bin/gateway ./cmd/gateway
        go build -o bin/api ./cmd/api
        go build -o bin/sweeper ./cmd/worker-sweeper
        go build -o bin/debouncer ./cmd/worker-debouncer
    else
        # Cross-compile Linux binaries via PowerShell (Windows Go with GOOS=linux)
        # This produces native Linux binaries that run directly in WSL,
        # binding to WSL's localhost (same network as Django).
        echo -e "${YELLOW}Building services (cross-compile Linux via Windows Go)...${NC}"
        powershell.exe -Command "\$env:GOOS='linux'; \$env:GOARCH='amd64'; go build -o bin/gateway ./cmd/gateway"
        powershell.exe -Command "\$env:GOOS='linux'; \$env:GOARCH='amd64'; go build -o bin/api ./cmd/api"
        powershell.exe -Command "\$env:GOOS='linux'; \$env:GOARCH='amd64'; go build -o bin/sweeper ./cmd/worker-sweeper"
        powershell.exe -Command "\$env:GOOS='linux'; \$env:GOARCH='amd64'; go build -o bin/debouncer ./cmd/worker-debouncer"
    fi
    
    echo -e "${GREEN}✓ All services built${NC}"
}

start_service() {
    local name=$1
    local binary=$2
    local log_file="$LOG_DIR/${name}.log"
    local pid_file="$LOG_DIR/${name}.pid"
    
    # Detect if this is a native Linux binary or Windows .exe
    local is_exe=false
    [[ "$binary" == *.exe ]] && is_exe=true
    
    if [ "$is_exe" = true ]; then
        # Windows .exe: check via PowerShell
        if powershell.exe -Command "Get-Process -Name '$name' -ErrorAction SilentlyContinue" > /dev/null 2>&1; then
            echo -e "${YELLOW}○ $name is already running (Windows process)${NC}"
            return 0
        fi
    else
        # Native Linux binary: check via PID file
        if [ -f "$pid_file" ]; then
            local old_pid=$(cat "$pid_file")
            if kill -0 "$old_pid" 2>/dev/null; then
                echo -e "${YELLOW}○ $name is already running (PID: $old_pid)${NC}"
                return 0
            fi
            rm -f "$pid_file"
        fi
    fi
    
    echo -e "${BLUE}Starting $name...${NC}"
    
    # Export environment for the service
    export REDIS_ADDR
    export JWT_SECRET
    export GATEWAY_ADDR
    export API_ADDR
    
    if [ "$is_exe" = true ]; then
        # Ensure Windows processes inherit these environment variables
        export WSLENV=REDIS_ADDR:JWT_SECRET:GATEWAY_ADDR:API_ADDR
    fi
    
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
    
    # Detect which binaries are available (prefer native Linux over .exe)
    if [ -f "$BIN_DIR/gateway" ]; then
        local ext=""
    elif [ -f "$BIN_DIR/gateway.exe" ]; then
        local ext=".exe"
    else
        echo -e "${RED}✗ No binaries found. Run with --build flag.${NC}"
        return 1
    fi
    
    start_service "gateway" "$BIN_DIR/gateway${ext}"
    start_service "api" "$BIN_DIR/api${ext}"
    start_service "sweeper" "$BIN_DIR/sweeper${ext}"
    start_service "debouncer" "$BIN_DIR/debouncer${ext}"
    
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
if [ "$BUILD" = true ] || { [ ! -f "$BIN_DIR/gateway" ] && [ ! -f "$BIN_DIR/gateway.exe" ]; }; then
    build_services
fi

# Start all services
start_all

#
# NOTE:
# -----
# We intentionally do NOT block here.
# - When launched via the monorepo's ./ops.sh start userengine, this script
#   should return so that the wrapper can immediately tail the log files.
# - Services are cross-compiled as native Linux binaries (GOOS=linux) and run
#   directly in WSL, binding to WSL's localhost. This ensures Django (also in
#   WSL) can reach them on localhost:<port>.
# - Stopping is handled via ./scripts/stop.sh (or ops.sh ... stop).
# If you want a blocking foreground mode, you can wrap this script and add
# your own trap/wait logic there.
