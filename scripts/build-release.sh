#!/bin/bash
set -e

# Build release binaries using Podman
# Usage: ./scripts/build-release.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
RELEASES_DIR="${RELEASES_DIR:-$PROJECT_DIR/../deployer-releases}"

GO_VERSION="1.22"

echo "Building deployer release binaries..."
echo "Project dir: $PROJECT_DIR"
echo "Releases dir: $RELEASES_DIR"

# Build web UI first
echo ""
echo "=== Building Web UI ==="
cd "$PROJECT_DIR/web"
bun install
bun run generate

# Copy static files to embedded directory
echo ""
echo "=== Copying static files ==="
rm -rf "$PROJECT_DIR/internal/web/static"
mkdir -p "$PROJECT_DIR/internal/web/static"
cp -r "$PROJECT_DIR/web/.output/public/"* "$PROJECT_DIR/internal/web/static/"
echo "Static files copied"

cd "$PROJECT_DIR"

# Build Linux ARM64
echo ""
echo "=== Building Linux ARM64 ==="
podman run --rm --platform linux/arm64 \
    -v "$PROJECT_DIR:/app" \
    -w /app \
    "golang:$GO_VERSION" \
    bash -c "CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags='-s -w' -o /app/deployer-linux-arm64 ./cmd/deployer"
echo "Linux ARM64 built"

# Build Linux AMD64 (native container, not emulated)
echo ""
echo "=== Building Linux AMD64 ==="
# Use a pre-built cross-compiler or build natively
# For CGO with SQLite, we need proper cross-compilation
podman run --rm --platform linux/amd64 \
    -v "$PROJECT_DIR:/app" \
    -w /app \
    -e CGO_ENABLED=0 \
    "golang:$GO_VERSION" \
    bash -c "go build -ldflags='-s -w' -o /app/deployer-linux-amd64-nocgo ./cmd/deployer" 2>/dev/null || {
    echo "AMD64 emulation failed, trying alternative..."
    # Build without CGO as fallback (SQLite will use pure Go fallback)
    podman run --rm --platform linux/arm64 \
        -v "$PROJECT_DIR:/app" \
        -w /app \
        "golang:$GO_VERSION" \
        bash -c "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o /app/deployer-linux-amd64 ./cmd/deployer"
}
echo "Linux AMD64 built"

# Build macOS binaries (native, no container needed)
echo ""
echo "=== Building macOS ARM64 ==="
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags='-s -w' -o "$PROJECT_DIR/deployer-darwin-arm64" ./cmd/deployer
echo "macOS ARM64 built"

echo ""
echo "=== Building macOS AMD64 ==="
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags='-s -w' -o "$PROJECT_DIR/deployer-darwin-amd64" ./cmd/deployer
echo "macOS AMD64 built"

# Copy to releases directory if it exists
if [ -d "$RELEASES_DIR" ]; then
    echo ""
    echo "=== Copying to releases directory ==="
    cp "$PROJECT_DIR/deployer-linux-arm64" "$RELEASES_DIR/"
    cp "$PROJECT_DIR/deployer-linux-amd64" "$RELEASES_DIR/" 2>/dev/null || cp "$PROJECT_DIR/deployer-linux-amd64-nocgo" "$RELEASES_DIR/deployer-linux-amd64"
    cp "$PROJECT_DIR/deployer-darwin-arm64" "$RELEASES_DIR/"
    cp "$PROJECT_DIR/deployer-darwin-amd64" "$RELEASES_DIR/"
    cp "$PROJECT_DIR/install.sh" "$RELEASES_DIR/"
    echo "Files copied to $RELEASES_DIR"
fi

echo ""
echo "=== Build complete ==="
ls -lh "$PROJECT_DIR"/deployer-*
