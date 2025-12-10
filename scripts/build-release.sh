#!/bin/bash
set -e

# Build release binaries using Podman
# Usage: ./scripts/build-release.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
RELEASES_DIR="${RELEASES_DIR:-$PROJECT_DIR/../deployer-releases}"

GO_VERSION="1.25"

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

# Build deployer CLI - Linux ARM64
echo ""
echo "=== Building deployer CLI - Linux ARM64 ==="
podman run --rm --platform linux/arm64 \
    -v "$PROJECT_DIR:/app" \
    -w /app \
    "golang:$GO_VERSION" \
    bash -c "CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags='-s -w' -o /app/deployer-linux-arm64 ./cmd/deployer"
echo "deployer CLI Linux ARM64 built"

# Build deployer CLI - Linux AMD64
echo ""
echo "=== Building deployer CLI - Linux AMD64 ==="
podman run --rm --platform linux/amd64 \
    -v "$PROJECT_DIR:/app" \
    -w /app \
    -e CGO_ENABLED=0 \
    "golang:$GO_VERSION" \
    bash -c "go build -ldflags='-s -w' -o /app/deployer-linux-amd64-nocgo ./cmd/deployer" 2>/dev/null || {
    echo "AMD64 emulation failed, trying cross-compile..."
    podman run --rm --platform linux/arm64 \
        -v "$PROJECT_DIR:/app" \
        -w /app \
        "golang:$GO_VERSION" \
        bash -c "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o /app/deployer-linux-amd64 ./cmd/deployer"
}
echo "deployer CLI Linux AMD64 built"

# Build deployerd daemon - Linux ARM64
echo ""
echo "=== Building deployerd daemon - Linux ARM64 ==="
podman run --rm --platform linux/arm64 \
    -v "$PROJECT_DIR:/app" \
    -w /app \
    "golang:$GO_VERSION" \
    bash -c "CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags='-s -w' -o /app/deployerd-linux-arm64 ./cmd/deployerd"
echo "deployerd daemon Linux ARM64 built"

# Build deployerd daemon - Linux AMD64
echo ""
echo "=== Building deployerd daemon - Linux AMD64 ==="
podman run --rm --platform linux/amd64 \
    -v "$PROJECT_DIR:/app" \
    -w /app \
    -e CGO_ENABLED=0 \
    "golang:$GO_VERSION" \
    bash -c "go build -ldflags='-s -w' -o /app/deployerd-linux-amd64-nocgo ./cmd/deployerd" 2>/dev/null || {
    echo "AMD64 emulation failed, trying cross-compile..."
    podman run --rm --platform linux/arm64 \
        -v "$PROJECT_DIR:/app" \
        -w /app \
        "golang:$GO_VERSION" \
        bash -c "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o /app/deployerd-linux-amd64 ./cmd/deployerd"
}
echo "deployerd daemon Linux AMD64 built"

# Build macOS binaries (native, no container needed)
echo ""
echo "=== Building macOS ARM64 (CLI + Daemon) ==="
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags='-s -w' -o "$PROJECT_DIR/deployer-darwin-arm64" ./cmd/deployer
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags='-s -w' -o "$PROJECT_DIR/deployerd-darwin-arm64" ./cmd/deployerd
echo "macOS ARM64 built"

echo ""
echo "=== Building macOS AMD64 (CLI + Daemon) ==="
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags='-s -w' -o "$PROJECT_DIR/deployer-darwin-amd64" ./cmd/deployer
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags='-s -w' -o "$PROJECT_DIR/deployerd-darwin-amd64" ./cmd/deployerd
echo "macOS AMD64 built"

# Copy to releases directory if it exists
if [ -d "$RELEASES_DIR" ]; then
    echo ""
    echo "=== Copying to releases directory ==="
    # CLI binaries
    cp "$PROJECT_DIR/deployer-linux-arm64" "$RELEASES_DIR/"
    cp "$PROJECT_DIR/deployer-linux-amd64" "$RELEASES_DIR/" 2>/dev/null || cp "$PROJECT_DIR/deployer-linux-amd64-nocgo" "$RELEASES_DIR/deployer-linux-amd64"
    cp "$PROJECT_DIR/deployer-darwin-arm64" "$RELEASES_DIR/"
    cp "$PROJECT_DIR/deployer-darwin-amd64" "$RELEASES_DIR/"
    # Daemon binaries
    cp "$PROJECT_DIR/deployerd-linux-arm64" "$RELEASES_DIR/"
    cp "$PROJECT_DIR/deployerd-linux-amd64" "$RELEASES_DIR/" 2>/dev/null || cp "$PROJECT_DIR/deployerd-linux-amd64-nocgo" "$RELEASES_DIR/deployerd-linux-amd64"
    cp "$PROJECT_DIR/deployerd-darwin-arm64" "$RELEASES_DIR/"
    cp "$PROJECT_DIR/deployerd-darwin-amd64" "$RELEASES_DIR/"
    # Scripts
    cp "$PROJECT_DIR/scripts/install.sh" "$RELEASES_DIR/" 2>/dev/null || true
    echo "Files copied to $RELEASES_DIR"
fi

echo ""
echo "=== Build complete ==="
echo "CLI binaries:"
ls -lh "$PROJECT_DIR"/deployer-* 2>/dev/null || true
echo ""
echo "Daemon binaries:"
ls -lh "$PROJECT_DIR"/deployerd-* 2>/dev/null || true
