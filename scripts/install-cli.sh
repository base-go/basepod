#!/bin/bash
# Deployer CLI Installation Script (Local Machine)
# Installs only the deployerctl CLI tool

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

# Configuration
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
GITHUB_REPO="deployer/deployer"

echo -e "${BLUE}╔═══════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     Deployer CLI Installer            ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════╝${NC}"
echo ""

# Detect platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo -e "${RED}Unsupported architecture: $ARCH${NC}"; exit 1 ;;
esac

case "$OS" in
    linux) OS="linux" ;;
    darwin) OS="darwin" ;;
    *) echo -e "${RED}Unsupported OS: $OS${NC}"; exit 1 ;;
esac

echo -e "${YELLOW}Platform: ${OS}-${ARCH}${NC}"

# Create install directory
mkdir -p "$INSTALL_DIR"

# Check if Go is available for building
if command -v go &> /dev/null; then
    echo -e "${YELLOW}Building from source...${NC}"

    # Create temp build directory
    BUILD_DIR=$(mktemp -d)
    trap "rm -rf $BUILD_DIR" EXIT

    cd "$BUILD_DIR"

    # Initialize module and create minimal CLI
    go mod init temp

    # For now, download and build
    echo "Downloading deployerctl source..."

    # Build directly if in repo
    if [ -f "../cmd/deployerctl/main.go" ]; then
        cd ..
        go build -o "$INSTALL_DIR/deployerctl" ./cmd/deployerctl
    else
        echo -e "${RED}Source not found. Please build manually.${NC}"
        exit 1
    fi
else
    echo -e "${RED}Go not found. Please install Go first or download pre-built binary.${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Installed deployerctl to $INSTALL_DIR${NC}"

# Add to PATH if needed
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo -e "${YELLOW}Add this to your shell profile:${NC}"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
fi

echo ""
echo -e "${GREEN}Installation complete!${NC}"
echo ""
echo "Usage:"
echo "  deployerctl login https://deployer.example.com"
echo "  deployerctl apps"
echo "  deployerctl create myapp"
echo "  deployerctl deploy myapp --image nginx:latest"
echo ""
echo "Run 'deployerctl --help' for more commands."
