#!/bin/bash
# Development script for basepod
# Usage: ./scripts/dev.sh [start|stop|restart|status|logs|build]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_DIR/build"
BINARY="$BUILD_DIR/basepodd"
PID_FILE="$PROJECT_DIR/.basepodd.pid"
LOG_FILE="/tmp/basepod.log"

cd "$PROJECT_DIR"

build() {
    echo "Building frontend..."
    cd "$PROJECT_DIR/web"
    rm -rf .output .nuxt
    bun install
    bunx nuxi generate
    cd "$PROJECT_DIR"

    echo "Building backend..."
    mkdir -p "$BUILD_DIR"
    go build -ldflags "-X main.version=$(grep 'version = ' cmd/basepodd/main.go | sed 's/.*"\(.*\)".*/\1/')" \
        -o "$BINARY" ./cmd/basepodd

    echo "Build complete: $BINARY"
}

start() {
    if is_running; then
        echo "Basepod is already running (PID: $(cat "$PID_FILE"))"
        return 1
    fi

    # Build if binary doesn't exist
    if [ ! -f "$BINARY" ]; then
        build
    fi

    echo "Starting basepod..."
    "$BINARY" > "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"

    sleep 2
    if is_running; then
        echo "Basepod started (PID: $(cat "$PID_FILE"))"
        echo "Logs: $LOG_FILE"
        echo "API: http://localhost:3000"
    else
        echo "Failed to start basepod. Check logs:"
        tail -20 "$LOG_FILE"
        return 1
    fi
}

stop() {
    if ! is_running; then
        echo "Basepod is not running"
        rm -f "$PID_FILE"
        return 0
    fi

    echo "Stopping basepod..."
    kill "$(cat "$PID_FILE")" 2>/dev/null || true
    rm -f "$PID_FILE"

    # Also kill any stray processes
    pkill -f "basepodd" 2>/dev/null || true

    echo "Basepod stopped"
}

status() {
    if is_running; then
        echo "Basepod is running (PID: $(cat "$PID_FILE"))"
        echo ""
        echo "Recent logs:"
        tail -10 "$LOG_FILE" 2>/dev/null || echo "No logs found"
    else
        echo "Basepod is not running"
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
    build)
        build
        ;;
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        stop
        sleep 1
        build
        start
        ;;
    status)
        status
        ;;
    logs)
        logs
        ;;
    *)
        echo "Usage: $0 {build|start|stop|restart|status|logs}"
        echo ""
        echo "Commands:"
        echo "  build   - Build frontend and backend"
        echo "  start   - Start the server (builds if needed)"
        echo "  stop    - Stop the server"
        echo "  restart - Rebuild and restart the server"
        echo "  status  - Show server status and recent logs"
        echo "  logs    - Follow the log file"
        exit 1
        ;;
esac
