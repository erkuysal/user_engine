#!/bin/bash
# UserEngine - Stop All Services
# Usage: ./scripts/stop.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
LOG_DIR="$PROJECT_DIR/logs"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}Stopping UserEngine services...${NC}"

stop_service() {
    local name=$1
    local binary_name="${name}.exe"
    
    # Try to kill by name using Windows taskkill (more reliable/aggressive for this setup)
    if powershell.exe -Command "Get-Process -Name '$name' -ErrorAction SilentlyContinue" > /dev/null; then
        echo -e "${YELLOW}Stopping $name...${NC}"
        # /F = force, /IM = image name, /T = tree (kill children)
        powershell.exe -Command "taskkill /F /IM '$binary_name' /T" > /dev/null 2>&1
        echo -e "${GREEN}✓ Stopped $binary_name${NC}"
    else
        echo -e "${YELLOW}○ $name not running (checked via PowerShell)${NC}"
    fi

    # Clean up pid file if it exists, just in case
    local pid_file="$LOG_DIR/${name}.pid"
    if [ -f "$pid_file" ]; then
        rm -f "$pid_file"
    fi
}

stop_service "gateway"
stop_service "api"
stop_service "sweeper"
stop_service "debouncer"

echo ""
echo -e "${GREEN}All services stopped${NC}"

