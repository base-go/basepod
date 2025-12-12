#!/bin/bash
set -e

# Release script - builds all binaries and pushes to releases repo
# Usage: ./release.sh [version]  - specify version like 0.1.11
#        ./release.sh            - auto-increment patch version

cd "$(dirname "$0")"

RELEASES_DIR="${RELEASES_DIR:-../deployer-releases}"
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

# Get current version from deployerd (server)
RAW_VERSION=$(grep 'version = "' cmd/deployerd/main.go | sed 's/.*"\(.*\)".*/\1/')
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
sed -i '' "s/version = \"$CURRENT_VERSION\"/version = \"$NEW_VERSION\"/" cmd/deployerd/main.go
sed -i '' "s/version = \"[^\"]*\"/version = \"$NEW_VERSION\"/" cmd/deployer/main.go

# Update version in releases README
sed -i '' "s/\*\*v$CURRENT_VERSION\*\*/\*\*v$NEW_VERSION\*\*/" "$RELEASES_DIR/README.md" 2>/dev/null || true

# Build web UI
echo "Building web UI..."
cd web && bun run generate && cd ..

# Copy static files to dist (matches embed.go)
rm -rf internal/web/dist
mkdir -p internal/web/dist
cp -r web/.output/public/* internal/web/dist/

# Build deployerd (server) - Linux ARM64
echo "Building deployerd linux-arm64..."
podman run --rm --platform linux/arm64 -v "$PWD:/app" -w /app golang:$GO_VERSION \
    bash -c "CGO_ENABLED=1 go build -ldflags='-s -w' -o deployerd-linux-arm64 ./cmd/deployerd"

# Build deployerd (server) - Linux AMD64 (without CGO)
echo "Building deployerd linux-amd64..."
podman run --rm --platform linux/arm64 -v "$PWD:/app" -w /app golang:$GO_VERSION \
    bash -c "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o deployerd-linux-amd64 ./cmd/deployerd"

# Build deployerd (server) - macOS
echo "Building deployerd darwin-arm64..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags='-s -w' -o deployerd-darwin-arm64 ./cmd/deployerd

echo "Building deployerd darwin-amd64..."
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags='-s -w' -o deployerd-darwin-amd64 ./cmd/deployerd

# Build deployer (CLI client) - all platforms (no CGO needed for client)
echo "Building deployer CLI linux-arm64..."
GOOS=linux GOARCH=arm64 go build -ldflags='-s -w' -o deployer-linux-arm64 ./cmd/deployer

echo "Building deployer CLI linux-amd64..."
GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o deployer-linux-amd64 ./cmd/deployer

echo "Building deployer CLI darwin-arm64..."
GOOS=darwin GOARCH=arm64 go build -ldflags='-s -w' -o deployer-darwin-arm64 ./cmd/deployer

echo "Building deployer CLI darwin-amd64..."
GOOS=darwin GOARCH=amd64 go build -ldflags='-s -w' -o deployer-darwin-amd64 ./cmd/deployer

# Copy to releases
cp deployerd-linux-arm64 deployerd-linux-amd64 deployerd-darwin-arm64 deployerd-darwin-amd64 \
   deployer-linux-arm64 deployer-linux-amd64 deployer-darwin-arm64 deployer-darwin-amd64 \
   install.sh "$RELEASES_DIR/"

# Commit and push releases
cd "$RELEASES_DIR"
git add -A
git commit -m "v$NEW_VERSION"
git push

# Create GitHub release with binaries
echo "Creating GitHub release..."
gh release create "v$NEW_VERSION" \
    deployerd-linux-arm64 \
    deployerd-linux-amd64 \
    deployerd-darwin-arm64 \
    deployerd-darwin-amd64 \
    deployer-linux-arm64 \
    deployer-linux-amd64 \
    deployer-darwin-arm64 \
    deployer-darwin-amd64 \
    --title "v$NEW_VERSION" \
    --notes "Release v$NEW_VERSION

Server (deployerd): Run on your server
CLI (deployer): Run on your local machine"

echo ""
echo "Released v$NEW_VERSION"
