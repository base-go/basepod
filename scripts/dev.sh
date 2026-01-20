#!/bin/bash
# Development script for deployer
# Usage: ./scripts/dev.sh [start|stop|restart|status|logs]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY="$PROJECT_DIR/deployerd"
PID_FILE="$PROJECT_DIR/.deployerd.pid"
LOG_FILE="/tmp/deployer.log"

cd "$PROJECT_DIR"

start() {
    if is_running; then
        echo "Deployer is already running (PID: $(cat "$PID_FILE"))"
        return 1
    fi

    echo "Building deployer..."
    go build -o "$BINARY" ./cmd/deployerd

    echo "Starting deployer..."
    "$BINARY" > "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"

    sleep 2
    if is_running; then
        echo "Deployer started (PID: $(cat "$PID_FILE"))"
        echo "Logs: $LOG_FILE"
        echo "API: http://localhost:3000"
    else
        echo "Failed to start deployer. Check logs:"
        tail -20 "$LOG_FILE"
        return 1
    fi
}

stop() {
    if ! is_running; then
        echo "Deployer is not running"
        rm -f "$PID_FILE"
        return 0
    fi

    echo "Stopping deployer..."
    kill "$(cat "$PID_FILE")" 2>/dev/null || true
    rm -f "$PID_FILE"

    # Also kill any stray processes
    pkill -f "deployerd" 2>/dev/null || true

    echo "Deployer stopped"
}

restart() {
    stop
    sleep 1
    start
}

status() {
    if is_running; then
        echo "Deployer is running (PID: $(cat "$PID_FILE"))"
        echo ""
        echo "Recent logs:"
        tail -10 "$LOG_FILE" 2>/dev/null || echo "No logs found"
    else
        echo "Deployer is not running"
    fi
}

logs() {
    if [ -f "$LOG_FILE" ]; then
        tail -f "$LOG_FILE"
    else
        echo "No log file found at $LOG_FILE"
    fi
}

is_running() {
    if [ -f "$PID_FILE" ]; then
        pid=$(cat "$PID_FILE")
        if ps -p "$pid" > /dev/null 2>&1; then
            return 0
        fi
    fi
    return 1
}

case "${1:-}" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        restart
        ;;
    status)
        status
        ;;
    logs)
        logs
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status|logs}"
        echo ""
        echo "Commands:"
        echo "  start   - Build and start the server"
        echo "  stop    - Stop the server"
        echo "  restart - Rebuild and restart the server"
        echo "  status  - Show server status and recent logs"
        echo "  logs    - Follow the log file"
        exit 1
        ;;
esac
