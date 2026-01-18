#!/bin/bash
# UserEngine - Check Service Status
# Usage: ./scripts/status.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
LOG_DIR="$PROJECT_DIR/logs"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}UserEngine Service Status${NC}"
echo "════════════════════════════════════════"

check_service() {
    local name=$1
    local port=$2
    local pid_file="$LOG_DIR/${name}.pid"
    
    local status="${RED}●${NC} stopped"
    local pid_info=""
    
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 $pid 2>/dev/null; then
            status="${GREEN}●${NC} running"
            pid_info="(PID: $pid)"
        fi
    fi
    
    printf "  %-12s %b %s\n" "$name" "$status" "$pid_info"
}

check_service "gateway" "8080"
check_service "api" "8081"
check_service "sweeper" ""
check_service "debouncer" ""

echo ""
echo -e "${BLUE}Redis Connection${NC}"
echo "════════════════════════════════════════"

REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
if command -v redis-cli &> /dev/null; then
    if redis-cli -h "${REDIS_ADDR%:*}" -p "${REDIS_ADDR#*:}" ping &> /dev/null; then
        echo -e "  ${GREEN}●${NC} connected ($REDIS_ADDR)"
    else
        echo -e "  ${RED}●${NC} disconnected ($REDIS_ADDR)"
    fi
else
    echo -e "  ${YELLOW}○${NC} redis-cli not installed"
fi

echo ""
echo -e "${BLUE}Endpoints${NC}"
echo "════════════════════════════════════════"
echo "  WebSocket: ws://localhost:8080/ws"
echo "  REST API:  http://localhost:8081"
echo ""

