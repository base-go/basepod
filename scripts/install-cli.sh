#!/bin/bash
set -e

# Basepod CLI Install Script
# Usage: curl -fsSL https://pod.base.al/cli | bash

GITHUB_REPO="base-go/basepod"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="bp"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[x]${NC} $1"; exit 1; }

# Detect OS and architecture
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case $OS in
        darwin) OS="darwin" ;;
        linux) OS="linux" ;;
        *) error "Unsupported OS: $OS" ;;
    esac

    case $ARCH in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) error "Unsupported architecture: $ARCH" ;;
    esac

    log "Detected platform: $OS/$ARCH"
}

# Download and install
install_cli() {
    local download_url="https://github.com/$GITHUB_REPO/releases/latest/download/$BINARY_NAME-$OS-$ARCH"
    local tmp_file=$(mktemp)

    log "Downloading from $download_url..."

    if ! curl -fsSL "$download_url" -o "$tmp_file"; then
        rm -f "$tmp_file"
        error "Failed to download. Check your internet connection and try again."
    fi

    # Check if we need sudo
    if [ -w "$INSTALL_DIR" ]; then
        mv "$tmp_file" "$INSTALL_DIR/$BINARY_NAME"
        chmod +x "$INSTALL_DIR/$BINARY_NAME"
    else
        log "Installing to $INSTALL_DIR (requires sudo)..."
        sudo mv "$tmp_file" "$INSTALL_DIR/$BINARY_NAME"
        sudo chmod +x "$INSTALL_DIR/$BINARY_NAME"
    fi

    log "Installed $BINARY_NAME to $INSTALL_DIR/$BINARY_NAME"
}

# Verify installation
verify() {
    if command -v $BINARY_NAME &> /dev/null; then
        local version=$($BINARY_NAME version 2>/dev/null || echo "unknown")
        echo ""
        echo -e "${GREEN}Basepod CLI installed successfully!${NC}"
        echo ""
        echo "  Version: $version"
        echo ""
        echo "  Get started:"
        echo "    bp login your-server.com    # Connect to your server"
        echo "    bp init                     # Initialize a project"
        echo "    bp push                     # Deploy your app"
        echo ""
        echo "  Documentation: https://pod.base.al"
        echo ""
    else
        warn "Installation complete, but 'bp' not found in PATH."
        warn "You may need to add $INSTALL_DIR to your PATH."
    fi
}

main() {
    echo ""
    echo "  Basepod CLI Installer"
    echo ""

    detect_platform
    install_cli
    verify
}

main "$@"
