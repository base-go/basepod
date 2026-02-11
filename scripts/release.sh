#!/bin/bash
set -e

# Release script - builds all binaries and creates GitHub release
# Usage: ./scripts/release.sh [version]  - specify version like 0.1.11
#        ./scripts/release.sh            - auto-increment patch version

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"

GO_VERSION="1.25"

# Validate version format (x.x.x where x is a number)
validate_version() {
    if [[ ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        return 1
    fi
    return 0
}

# Normalize version to x.x.x format
normalize_version() {
    local v="$1"
    v="${v#v}"  # Remove leading 'v' if present

    # Split by dots and filter numeric parts
    IFS='.' read -ra parts <<< "$v"
    local major="${parts[0]:-0}"
    local minor="${parts[1]:-0}"
    local patch="${parts[2]:-0}"

    # Ensure each part is numeric
    [[ "$major" =~ ^[0-9]+$ ]] || major=0
    [[ "$minor" =~ ^[0-9]+$ ]] || minor=0
    [[ "$patch" =~ ^[0-9]+$ ]] || patch=0

    echo "$major.$minor.$patch"
}

# Get current version from basepod (server)
RAW_VERSION=$(grep 'version = "' cmd/basepod/main.go | sed 's/.*"\(.*\)".*/\1/')
CURRENT_VERSION=$(normalize_version "$RAW_VERSION")
echo "Current version: $CURRENT_VERSION"

# Auto-increment function
increment_version() {
    IFS='.' read -r major minor patch <<< "$CURRENT_VERSION"
    patch=$((patch + 1))
    echo "$major.$minor.$patch"
}

# Set new version
if [ -n "$1" ]; then
    # Check if provided version is valid
    if validate_version "$1"; then
        NEW_VERSION="$1"
    else
        echo "Error: Invalid version format '$1'. Must be x.x.x (e.g., 0.1.32)"
        NEXT_VERSION=$(increment_version)
        echo ""
        read -p "Do you want to release $NEXT_VERSION instead? [y/N] " -n 1 -r
        echo ""
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            NEW_VERSION="$NEXT_VERSION"
        else
            echo "Aborted."
            exit 1
        fi
    fi
else
    # Auto-increment patch
    NEW_VERSION=$(increment_version)
fi

echo "New version: $NEW_VERSION"

# Update version in both main.go files
sed -i '' "s/version = \"$CURRENT_VERSION\"/version = \"$NEW_VERSION\"/" cmd/basepod/main.go
sed -i '' "s/version = \"[^\"]*\"/version = \"$NEW_VERSION\"/" cmd/bp/main.go

# Build web UI
echo "Building web UI..."
cd web && bun run generate && cd ..

# Copy static files to dist (matches embed.go)
rm -rf internal/web/dist
mkdir -p internal/web/dist
cp -r web/.output/public/* internal/web/dist/

# Build basepod (server) - Linux ARM64
echo "Building basepod linux-arm64..."
podman run --rm --platform linux/arm64 -v "$PWD:/app" -w /app golang:$GO_VERSION \
    bash -c "CGO_ENABLED=1 go build -ldflags='-s -w' -o basepod-linux-arm64 ./cmd/basepod"

# Build basepod (server) - Linux AMD64 (without CGO)
echo "Building basepod linux-amd64..."
podman run --rm --platform linux/arm64 -v "$PWD:/app" -w /app golang:$GO_VERSION \
    bash -c "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o basepod-linux-amd64 ./cmd/basepod"

# Build basepod (server) - macOS
echo "Building basepod darwin-arm64..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags='-s -w' -o basepod-darwin-arm64 ./cmd/basepod
codesign -s - basepod-darwin-arm64

echo "Building basepod darwin-amd64..."
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags='-s -w' -o basepod-darwin-amd64 ./cmd/basepod
codesign -s - basepod-darwin-amd64

# Build bp (CLI client) - all platforms (no CGO needed for client)
echo "Building bp CLI linux-arm64..."
GOOS=linux GOARCH=arm64 go build -ldflags='-s -w' -o bp-linux-arm64 ./cmd/bp

echo "Building bp CLI linux-amd64..."
GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o bp-linux-amd64 ./cmd/bp

echo "Building bp CLI darwin-arm64..."
GOOS=darwin GOARCH=arm64 go build -ldflags='-s -w' -o bp-darwin-arm64 ./cmd/bp
codesign -s - bp-darwin-arm64

echo "Building bp CLI darwin-amd64..."
GOOS=darwin GOARCH=amd64 go build -ldflags='-s -w' -o bp-darwin-amd64 ./cmd/bp
codesign -s - bp-darwin-amd64

# Commit version bump
git add -A
git commit -m "Release v$NEW_VERSION" || true
git push

# Copy install scripts to release names
cp scripts/install-server.sh install.sh
cp scripts/install-cli.sh install-cli.sh

# Create GitHub release with binaries
echo "Creating GitHub release..."
gh release create "v$NEW_VERSION" \
    basepod-linux-arm64 \
    basepod-linux-amd64 \
    basepod-darwin-arm64 \
    basepod-darwin-amd64 \
    bp-linux-arm64 \
    bp-linux-amd64 \
    bp-darwin-arm64 \
    bp-darwin-amd64 \
    install.sh \
    install-cli.sh \
    --title "v$NEW_VERSION" \
    --notes "Release v$NEW_VERSION

**Server (basepod)**: Run on your Mac Mini or Linux VPS
**CLI (bp)**: Run on your local machine

## Install

\`\`\`bash
# Server (requires sudo)
curl -fsSL https://pod.base.al/install | sudo bash

# CLI
curl -fsSL https://pod.base.al/cli | bash
\`\`\`

Documentation: https://pod.base.al"

# Cleanup binaries and install scripts
rm -f basepod-* bp-* install.sh install-cli.sh

echo ""
echo "Released v$NEW_VERSION"
echo "Install: curl -fsSL https://pod.base.al/install | sudo bash"
