#!/bin/bash
# UserEngine - Stop All Services
# Usage: ./scripts/stop.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
# Use USERENGINE_LOG_DIR if set (from ops.sh), otherwise default to local logs
LOG_DIR="${USERENGINE_LOG_DIR:-$PROJECT_DIR/logs}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}Stopping UserEngine services...${NC}"

stop_service() {
    local name=$1
    local stopped=false

    # Helper: gracefully kill a PID, then force if needed
    kill_pid() {
        local pid=$1
        kill "$pid" 2>/dev/null || true
        for i in 1 2 3; do
            kill -0 "$pid" 2>/dev/null || { stopped=true; return; }
            sleep 1
        done
        kill -9 "$pid" 2>/dev/null || true
        stopped=true
    }

    # 1. Try every known PID file location (centralized log dir and local fallback)
    local -a pid_files=(
        "$LOG_DIR/${name}.pid"
        "$(dirname "$SCRIPT_DIR")/logs/${name}.pid"
    )
    for pid_file in "${pid_files[@]}"; do
        if [ -f "$pid_file" ]; then
            local pid
            pid=$(cat "$pid_file")
            if kill -0 "$pid" 2>/dev/null; then
                echo -e "${YELLOW}Stopping $name (PID: $pid from $pid_file)...${NC}"
                kill_pid "$pid"
            fi
            rm -f "$pid_file"
        fi
    done

    # 2. Fallback: kill any remaining process matching the binary name exactly
    #    (covers cases where PID files are missing, stale, or in unexpected locations)
    if pgrep -x "$name" > /dev/null 2>&1; then
        if [ "$stopped" = false ]; then
            echo -e "${YELLOW}Stopping $name (by name, no PID file found)...${NC}"
        else
            echo -e "${YELLOW}Cleaning up extra $name process(es)...${NC}"
        fi
        pkill -x "$name" 2>/dev/null || true
        sleep 1
        pkill -9 -x "$name" 2>/dev/null || true
        stopped=true
    fi

    if [ "$stopped" = true ]; then
        echo -e "${GREEN}✓ Stopped $name${NC}"
    else
        echo -e "${YELLOW}○ $name not running${NC}"
    fi
}

stop_service "gateway"
stop_service "api"
stop_service "sweeper"
stop_service "debouncer"

echo ""
echo -e "${GREEN}All services stopped${NC}"

