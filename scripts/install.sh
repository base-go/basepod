#!/bin/bash
# Basepod Installation Script
# Works on Linux and macOS

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
DEPLOYER_HOME="$HOME/basepod"
DEPLOYER_VERSION="${DEPLOYER_VERSION:-latest}"
GITHUB_REPO="basepod/basepod"

# Detect OS and architecture
detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            echo -e "${RED}Unsupported architecture: $ARCH${NC}"
            exit 1
            ;;
    esac

    case "$OS" in
        linux)
            OS="linux"
            ;;
        darwin)
            OS="darwin"
            ;;
        *)
            echo -e "${RED}Unsupported OS: $OS${NC}"
            exit 1
            ;;
    esac

    echo -e "${BLUE}Detected platform: ${OS}-${ARCH}${NC}"
}

# Print banner
print_banner() {
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════╗"
    echo "║         Basepod Installer            ║"
    echo "║    PaaS with Podman + Caddy + Go      ║"
    echo "╚═══════════════════════════════════════╝"
    echo -e "${NC}"
}

# Check prerequisites
check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"

    # Check for Podman
    if command -v podman &> /dev/null; then
        PODMAN_VERSION=$(podman --version | awk '{print $3}')
        echo -e "${GREEN}✓ Podman ${PODMAN_VERSION} found${NC}"
    else
        echo -e "${RED}✗ Podman not found${NC}"
        echo ""
        echo "Please install Podman first:"
        if [ "$OS" = "linux" ]; then
            echo "  Ubuntu/Debian: sudo apt install podman"
            echo "  Fedora/RHEL:   sudo dnf install podman"
            echo "  Arch:          sudo pacman -S podman"
        else
            echo "  macOS: brew install podman && podman machine init && podman machine start"
        fi
        exit 1
    fi

    # Check if Podman socket is running
    SOCKET_PATH=$(get_podman_socket)
    if [ -S "$SOCKET_PATH" ]; then
        echo -e "${GREEN}✓ Podman socket active at ${SOCKET_PATH}${NC}"
    else
        echo -e "${YELLOW}⚠ Podman socket not found at ${SOCKET_PATH}${NC}"
        echo ""
        echo "Starting Podman socket service..."
        if [ "$OS" = "linux" ]; then
            systemctl --user enable --now podman.socket 2>/dev/null || \
            podman system service --time=0 &
            sleep 2
        else
            # macOS - podman machine should handle this
            echo "On macOS, ensure podman machine is running: podman machine start"
        fi
    fi

    # Check for curl or wget
    if command -v curl &> /dev/null; then
        DOWNLOADER="curl -fsSL"
    elif command -v wget &> /dev/null; then
        DOWNLOADER="wget -qO-"
    else
        echo -e "${RED}✗ Neither curl nor wget found${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Downloader available${NC}"
}

# Get Podman socket path
get_podman_socket() {
    if [ "$OS" = "linux" ]; then
        if [ -n "$XDG_RUNTIME_DIR" ]; then
            echo "$XDG_RUNTIME_DIR/podman/podman.sock"
        else
            echo "/run/user/$(id -u)/podman/podman.sock"
        fi
    else
        echo "$HOME/.local/share/containers/podman/machine/podman.sock"
    fi
}

# Create directory structure
create_directories() {
    echo -e "${YELLOW}Creating directory structure...${NC}"

    mkdir -p "$DEPLOYER_HOME"/{bin,config,data/apps,data/certs,logs,caddy,tmp}

    echo -e "${GREEN}✓ Created $DEPLOYER_HOME${NC}"
}

# Download and install Caddy
install_caddy() {
    echo -e "${YELLOW}Installing Caddy...${NC}"

    CADDY_PATH="$DEPLOYER_HOME/bin/caddy"

    if [ -f "$CADDY_PATH" ]; then
        echo -e "${GREEN}✓ Caddy already installed${NC}"
        return
    fi

    # Download Caddy
    CADDY_URL="https://caddyserver.com/api/download?os=${OS}&arch=${ARCH}"

    echo "Downloading Caddy from official source..."
    curl -fsSL "$CADDY_URL" -o "$CADDY_PATH"
    chmod +x "$CADDY_PATH"

    # Verify installation
    if "$CADDY_PATH" version &> /dev/null; then
        CADDY_VERSION=$("$CADDY_PATH" version | head -1)
        echo -e "${GREEN}✓ Caddy ${CADDY_VERSION} installed${NC}"
    else
        echo -e "${RED}✗ Caddy installation failed${NC}"
        exit 1
    fi

    # Set capability for low port binding (Linux only)
    if [ "$OS" = "linux" ]; then
        echo ""
        echo -e "${YELLOW}To allow Caddy to bind to ports 80/443 without root:${NC}"
        echo "  sudo setcap 'cap_net_bind_service=+ep' $CADDY_PATH"
        echo ""
    fi
}

# Download and install Basepod
install_basepod() {
    echo -e "${YELLOW}Installing Basepod...${NC}"

    DEPLOYER_PATH="$DEPLOYER_HOME/bin/basepod"
    DEPLOYERCTL_PATH="$DEPLOYER_HOME/bin/basepodctl"

    # For now, build from source if Go is available
    if command -v go &> /dev/null; then
        echo "Go found, building from source..."

        # Create temp directory for build
        BUILD_DIR=$(mktemp -d)
        cd "$BUILD_DIR"

        # Clone or download source
        if command -v git &> /dev/null; then
            git clone --depth 1 https://github.com/${GITHUB_REPO}.git . 2>/dev/null || {
                echo "Repository not available, using local build..."
                cd -
                BUILD_DIR="."
            }
        fi

        # Build
        go build -o "$DEPLOYER_PATH" ./cmd/basepod
        go build -o "$DEPLOYERCTL_PATH" ./cmd/basepodctl

        # Cleanup
        if [ "$BUILD_DIR" != "." ]; then
            rm -rf "$BUILD_DIR"
        fi

        echo -e "${GREEN}✓ Basepod built and installed${NC}"
    else
        echo -e "${RED}Go not found. Please install Go 1.25+ first.${NC}"
        echo "  https://go.dev/dl/"
        exit 1
    fi
}

# Create default configuration
create_config() {
    echo -e "${YELLOW}Creating default configuration...${NC}"

    CONFIG_FILE="$DEPLOYER_HOME/config/basepod.yaml"

    if [ -f "$CONFIG_FILE" ]; then
        echo -e "${GREEN}✓ Config already exists${NC}"
        return
    fi

    cat > "$CONFIG_FILE" << 'EOF'
# Basepod Configuration
server:
  host: "0.0.0.0"
  port: 443
  api_port: 3000
  log_level: "info"

domain:
  root: ""  # Set this to your domain, e.g., basepod.example.com
  wildcard: true
  email: "" # For Let's Encrypt

podman:
  socket_path: "" # Auto-detected if empty
  network: "basepod"

database:
  path: "data/basepod.db"
EOF

    echo -e "${GREEN}✓ Config created at $CONFIG_FILE${NC}"
}

# Create systemd user service (Linux only)
create_systemd_service() {
    if [ "$OS" != "linux" ]; then
        return
    fi

    echo -e "${YELLOW}Creating systemd user service...${NC}"

    mkdir -p "$HOME/.config/systemd/user"

    cat > "$HOME/.config/systemd/user/basepod.service" << EOF
[Unit]
Description=Basepod PaaS
After=network.target podman.socket

[Service]
Type=simple
ExecStart=$DEPLOYER_HOME/bin/basepod
Restart=always
RestartSec=5
Environment=HOME=$HOME

[Install]
WantedBy=default.target
EOF

    cat > "$HOME/.config/systemd/user/basepod-caddy.service" << EOF
[Unit]
Description=Basepod Caddy Proxy
After=network.target

[Service]
Type=simple
ExecStart=$DEPLOYER_HOME/bin/caddy run --config $DEPLOYER_HOME/caddy/Caddyfile
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
EOF

    systemctl --user daemon-reload

    echo -e "${GREEN}✓ Systemd services created${NC}"
    echo ""
    echo "To enable and start services:"
    echo "  systemctl --user enable --now basepod"
    echo "  systemctl --user enable --now basepod-caddy"
}

# Create launchd service (macOS only)
create_launchd_service() {
    if [ "$OS" != "darwin" ]; then
        return
    fi

    echo -e "${YELLOW}Creating launchd service...${NC}"

    mkdir -p "$HOME/Library/LaunchAgents"

    cat > "$HOME/Library/LaunchAgents/com.basepod.agent.plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.basepod.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>$DEPLOYER_HOME/bin/basepod</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>$DEPLOYER_HOME/logs/basepod.log</string>
    <key>StandardErrorPath</key>
    <string>$DEPLOYER_HOME/logs/basepod.error.log</string>
</dict>
</plist>
EOF

    echo -e "${GREEN}✓ launchd service created${NC}"
    echo ""
    echo "To start the service:"
    echo "  launchctl load ~/Library/LaunchAgents/com.basepod.agent.plist"
}

# Add to PATH
setup_path() {
    echo -e "${YELLOW}Setting up PATH...${NC}"

    SHELL_NAME=$(basename "$SHELL")
    PROFILE_FILE=""

    case "$SHELL_NAME" in
        bash)
            PROFILE_FILE="$HOME/.bashrc"
            ;;
        zsh)
            PROFILE_FILE="$HOME/.zshrc"
            ;;
        fish)
            PROFILE_FILE="$HOME/.config/fish/config.fish"
            ;;
    esac

    if [ -n "$PROFILE_FILE" ] && [ -f "$PROFILE_FILE" ]; then
        if ! grep -q "DEPLOYER_HOME" "$PROFILE_FILE"; then
            echo "" >> "$PROFILE_FILE"
            echo "# Basepod" >> "$PROFILE_FILE"
            echo "export DEPLOYER_HOME=\"$DEPLOYER_HOME\"" >> "$PROFILE_FILE"
            echo "export PATH=\"\$DEPLOYER_HOME/bin:\$PATH\"" >> "$PROFILE_FILE"
            echo -e "${GREEN}✓ Added to $PROFILE_FILE${NC}"
        else
            echo -e "${GREEN}✓ PATH already configured${NC}"
        fi
    fi
}

# Print completion message
print_completion() {
    echo ""
    echo -e "${GREEN}═══════════════════════════════════════${NC}"
    echo -e "${GREEN}  Basepod installed successfully!     ${NC}"
    echo -e "${GREEN}═══════════════════════════════════════${NC}"
    echo ""
    echo "Installation directory: $DEPLOYER_HOME"
    echo ""
    echo -e "${YELLOW}Next steps:${NC}"
    echo ""
    echo "1. Edit your configuration:"
    echo "   nano $DEPLOYER_HOME/config/basepod.yaml"
    echo ""
    echo "2. Set your domain and email for SSL"
    echo ""
    echo "3. Start the services:"
    if [ "$OS" = "linux" ]; then
        echo "   systemctl --user enable --now basepod basepod-caddy"
    else
        echo "   $DEPLOYER_HOME/bin/basepod &"
        echo "   $DEPLOYER_HOME/bin/caddy run --config $DEPLOYER_HOME/caddy/Caddyfile &"
    fi
    echo ""
    echo "4. Access the web UI at:"
    echo "   https://your-domain.com (after DNS setup)"
    echo "   http://localhost:3000 (for local testing)"
    echo ""
    echo "For CLI usage:"
    echo "   basepodctl --help"
    echo ""
    echo -e "${BLUE}Documentation: https://github.com/${GITHUB_REPO}${NC}"
}

# Main installation flow
main() {
    print_banner
    detect_platform
    check_prerequisites
    create_directories
    install_caddy
    install_basepod
    create_config

    if [ "$OS" = "linux" ]; then
        create_systemd_service
    else
        create_launchd_service
    fi

    setup_path
    print_completion
}

# Run main
main "$@"
