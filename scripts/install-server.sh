#!/bin/bash
set -e

# Basepod Server Install Script
# Usage: curl -fsSL https://pod.base.al/install | sudo bash
# Or with domain: BASEPOD_DOMAIN=example.com curl -fsSL ... | sudo bash

BASEPOD_VERSION="${BASEPOD_VERSION:-latest}"
BASEPOD_DIR="${BASEPOD_DIR:-/opt/basepod}"
BASEPOD_USER="${BASEPOD_USER:-basepod}"
BASEPOD_DOMAIN="${BASEPOD_DOMAIN:-}"
BASEPOD_PASSWORD="${BASEPOD_PASSWORD:-}"
GITHUB_REPO="base-go/basepod"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[x]${NC} $1"; exit 1; }
ask() { echo -e "${BLUE}[?]${NC} $1"; }

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    error "Please run as root: curl -fsSL ... | sudo bash"
fi

# Detect OS
if [ "$(uname)" = "Darwin" ]; then
    OS="macos"
elif [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
else
    error "Cannot detect OS"
fi

log "Detected OS: $OS"

# Interactive domain setup if not provided
setup_domain() {
    # Check if BASEPOD_DOMAIN was passed via sudo
    if [ -z "$BASEPOD_DOMAIN" ] && [ -n "$SUDO_BASEPOD_DOMAIN" ]; then
        BASEPOD_DOMAIN="$SUDO_BASEPOD_DOMAIN"
    fi

    if [ -n "$BASEPOD_DOMAIN" ]; then
        log "Using domain: $BASEPOD_DOMAIN"
        log "Dashboard will be at: d.$BASEPOD_DOMAIN"
        # Generate random password if not provided
        if [ -z "$BASEPOD_PASSWORD" ]; then
            BASEPOD_PASSWORD=$(openssl rand -base64 12 | tr -d '/+=' | head -c 16)
        fi
        log "Admin password generated"
        return
    fi

    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}         Domain Configuration          ${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""
    echo "Basepod needs a root domain for your apps."
    echo ""
    echo "Example: If you enter 'example.com':"
    echo "  - Dashboard:  d.example.com"
    echo "  - Apps:       myapp.example.com, ghost.example.com, etc."
    echo ""
    echo "Prerequisites:"
    echo "  1. You own this domain"
    echo "  2. DNS wildcard A record: *.yourdomain.com -> this server's IP"
    echo ""

    # Check if stdin is a terminal (interactive mode)
    if [ -t 0 ]; then
        ask "Enter your root domain (e.g., example.com):"
        read -r BASEPOD_DOMAIN

        if [ -z "$BASEPOD_DOMAIN" ]; then
            warn "No domain entered. Apps will use IP:port access only."
            warn "You can configure a domain later in: $BASEPOD_DIR/config/basepod.yaml"
        else
            log "Domain set to: $BASEPOD_DOMAIN"
            log "Dashboard will be at: d.$BASEPOD_DOMAIN"

            # Ask for email for SSL certificates
            ask "Enter email for SSL certificates (optional, press Enter to skip):"
            read -r BASEPOD_EMAIL

            if [ -n "$BASEPOD_EMAIL" ]; then
                log "Email set to: $BASEPOD_EMAIL"
            fi

            # Generate random password if not provided
            if [ -z "$BASEPOD_PASSWORD" ]; then
                BASEPOD_PASSWORD=$(openssl rand -base64 12 | tr -d '/+=' | head -c 16)
            fi
            log "Admin password generated"
        fi
    else
        # Non-interactive mode without domain - piping curl without BASEPOD_DOMAIN set
        warn "Non-interactive mode detected."
        warn "To configure domain, use: curl ... | sudo BASEPOD_DOMAIN=example.com bash"
        warn ""
        warn "Proceeding without domain. Dashboard will be accessible via IP:port."
        warn "You can configure domain later in: $BASEPOD_DIR/config/basepod.yaml"
    fi
    echo ""
}

# Install dependencies based on OS
install_deps() {
    log "Installing dependencies..."

    case $OS in
        macos)
            # On macOS, run brew as the original user (not root)
            local brew_user="${SUDO_USER:-$(whoami)}"
            local brew_path="/opt/homebrew/bin/brew"
            [ ! -f "$brew_path" ] && brew_path="/usr/local/bin/brew"

            if [ ! -f "$brew_path" ]; then
                error "Homebrew not found. Install from https://brew.sh"
            fi

            log "Installing podman via Homebrew (as $brew_user)..."
            sudo -u "$brew_user" HOMEBREW_NO_AUTO_UPDATE=1 "$brew_path" install podman 2>/dev/null || true
            ;;
        ubuntu|debian)
            apt-get update -qq
            apt-get install -y -qq curl wget podman sqlite3 ca-certificates
            ;;
        fedora|centos|rhel|rocky|alma)
            dnf install -y podman sqlite curl wget ca-certificates
            ;;
        arch|manjaro)
            pacman -Sy --noconfirm podman sqlite curl wget
            ;;
        *)
            error "Unsupported OS: $OS. Please install manually."
            ;;
    esac
}

# Install Caddy
install_caddy() {
    if command -v caddy &> /dev/null; then
        log "Caddy already installed"
        return
    fi

    log "Installing Caddy..."

    case $OS in
        macos)
            local brew_user="${SUDO_USER:-$(whoami)}"
            local brew_path="/opt/homebrew/bin/brew"
            [ ! -f "$brew_path" ] && brew_path="/usr/local/bin/brew"
            log "Installing caddy via Homebrew (as $brew_user)..."
            sudo -u "$brew_user" HOMEBREW_NO_AUTO_UPDATE=1 "$brew_path" install caddy 2>/dev/null || true
            ;;
        ubuntu|debian)
            apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https
            curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
            curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
            apt-get update -qq
            apt-get install -y -qq caddy
            ;;
        fedora|centos|rhel|rocky|alma)
            dnf install -y 'dnf-command(copr)'
            dnf copr enable -y @caddy/caddy
            dnf install -y caddy
            ;;
        arch|manjaro)
            pacman -Sy --noconfirm caddy
            ;;
        *)
            # Fallback: download binary
            curl -fsSL "https://caddyserver.com/api/download?os=linux&arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" -o /usr/local/bin/caddy
            chmod +x /usr/local/bin/caddy
            ;;
    esac
}

# Create basepod user
create_user() {
    # On macOS, use the user who ran sudo
    if [ "$OS" = "macos" ]; then
        BASEPOD_USER="${SUDO_USER:-$(whoami)}"
        log "Using user: $BASEPOD_USER"
        return
    fi

    if id "$BASEPOD_USER" &>/dev/null; then
        log "User $BASEPOD_USER already exists"
    else
        log "Creating user $BASEPOD_USER..."
        useradd -r -m -s /bin/bash "$BASEPOD_USER"
    fi

    # Add to podman group if exists
    usermod -aG podman "$BASEPOD_USER" 2>/dev/null || true

    # Configure subuid/subgid for rootless Podman
    # This is required for Podman to map UIDs/GIDs in containers
    log "Configuring subuid/subgid for rootless Podman..."
    if ! grep -q "^$BASEPOD_USER:" /etc/subuid 2>/dev/null; then
        echo "$BASEPOD_USER:100000:65536" >> /etc/subuid
    fi
    if ! grep -q "^$BASEPOD_USER:" /etc/subgid 2>/dev/null; then
        echo "$BASEPOD_USER:100000:65536" >> /etc/subgid
    fi
}

# Download and install basepod
install_basepod() {
    # Set appropriate install dir for macOS
    if [ "$OS" = "macos" ]; then
        BASEPOD_DIR="/usr/local/basepod"
    fi

    log "Installing Basepod to $BASEPOD_DIR..."

    # Create directories with proper permissions (as root)
    mkdir -p "$BASEPOD_DIR"/{bin,config,data,logs}
    chmod 755 "$BASEPOD_DIR" "$BASEPOD_DIR/bin"

    # Detect architecture and OS
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) error "Unsupported architecture: $ARCH" ;;
    esac

    # Determine OS for download
    if [ "$OS" = "macos" ]; then
        DOWNLOAD_OS="darwin"
    else
        DOWNLOAD_OS="linux"
    fi

    # Get latest version info
    LATEST_VERSION=$(curl -fsSL "https://api.github.com/repos/$GITHUB_REPO/releases/latest" 2>/dev/null | grep '"tag_name"' | sed 's/.*"v\(.*\)".*/\1/' || echo "unknown")
    log "Installing version: $LATEST_VERSION"

    # Download basepod binary
    if [ "$BASEPOD_VERSION" = "latest" ]; then
        DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/latest/download/basepod-$DOWNLOAD_OS-$ARCH"
    else
        DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$BASEPOD_VERSION/basepod-$DOWNLOAD_OS-$ARCH"
    fi

    log "Downloading from $DOWNLOAD_URL..."
    rm -f "$BASEPOD_DIR/bin/basepod"
    curl -fsSL "$DOWNLOAD_URL" -o "$BASEPOD_DIR/bin/basepod" || {
        warn "Binary not found, building from source..."
        build_from_source
    }

    chmod +x "$BASEPOD_DIR/bin/basepod"
    ln -sf "$BASEPOD_DIR/bin/basepod" /usr/local/bin/basepod
}

# Build from source if binary not available
build_from_source() {
    log "Building from source..."

    # Install Go if not present
    if ! command -v go &> /dev/null; then
        log "Installing Go..."
        curl -fsSL "https://go.dev/dl/go1.22.0.linux-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" | tar -C /usr/local -xzf -
        export PATH=$PATH:/usr/local/go/bin
    fi

    # Clone and build
    TMPDIR=$(mktemp -d)
    git clone --depth 1 https://github.com/base-go/basepod.git "$TMPDIR"
    cd "$TMPDIR"
    go build -o "$BASEPOD_DIR/bin/basepod" ./cmd/basepod
    cd -
    rm -rf "$TMPDIR"
}

# Create configuration
create_config() {
    log "Creating configuration..."

    # Determine email for SSL
    local ssl_email="${BASEPOD_EMAIL:-}"
    if [ -z "$ssl_email" ] && [ -n "$BASEPOD_DOMAIN" ]; then
        ssl_email="admin@$BASEPOD_DOMAIN"
    fi

    # Generate password hash using sha256
    local password_hash=""
    if [ -n "$BASEPOD_PASSWORD" ]; then
        password_hash=$(echo -n "$BASEPOD_PASSWORD" | sha256sum | cut -d' ' -f1)
    fi

    if [ -n "$BASEPOD_DOMAIN" ]; then
        # Production mode with real domain
        cat > "$BASEPOD_DIR/config/basepod.yaml" <<EOF
server:
  api_port: 3000
  host: 0.0.0.0

auth:
  password_hash: $password_hash

podman:
  socket: /run/podman/podman.sock

caddy:
  admin_url: http://localhost:2019

database:
  path: $BASEPOD_DIR/data/basepod.db

domain:
  root: $BASEPOD_DOMAIN
  wildcard: true
  email: $ssl_email
EOF
    else
        # No domain mode - access via IP:port (no auth required for local)
        cat > "$BASEPOD_DIR/config/basepod.yaml" <<EOF
server:
  api_port: 3000
  host: 0.0.0.0

podman:
  socket: /run/podman/podman.sock

caddy:
  admin_url: http://localhost:2019

database:
  path: $BASEPOD_DIR/data/basepod.db

domain:
  suffix: ""
  wildcard: false
EOF
    fi
}

# Create Caddyfile
create_caddyfile() {
    log "Creating Caddyfile..."

    # Use provided email or default
    local ssl_email="${BASEPOD_EMAIL:-admin@$BASEPOD_DOMAIN}"

    if [ -n "$BASEPOD_DOMAIN" ]; then
        cat > "$BASEPOD_DIR/config/Caddyfile" <<EOF
{
    admin localhost:2019
    email $ssl_email
    on_demand_tls {
        ask http://localhost:3000/api/caddy/check
    }
}

d.$BASEPOD_DOMAIN {
    tls {
        on_demand
    }
    reverse_proxy localhost:3000
}

:443 {
    tls {
        on_demand
    }
    reverse_proxy localhost:3000
}
EOF
    else
        cat > "$BASEPOD_DIR/config/Caddyfile" <<EOF
{
    admin localhost:2019
    auto_https off
}

# Dashboard accessible via IP:80
:80 {
    reverse_proxy localhost:3000
}
EOF
    fi
}

# Create services (systemd on Linux, launchd on macOS)
create_services() {
    if [ "$OS" = "macos" ]; then
        create_macos_services
        return
    fi

    log "Creating systemd services..."

    # Run podman system migrate for the basepod user (required after subuid/subgid changes)
    log "Running podman system migrate..."
    sudo -u "$BASEPOD_USER" sh -c "cd /tmp && podman system migrate" 2>/dev/null || true

    # Basepod service
    cat > /etc/systemd/system/basepod.service <<EOF
[Unit]
Description=Basepod PaaS
After=network.target podman.socket

[Service]
Type=simple
User=$BASEPOD_USER
Group=$BASEPOD_USER
WorkingDirectory=$BASEPOD_DIR
ExecStart=$BASEPOD_DIR/bin/basepod
Restart=always
RestartSec=5
Environment=BASEPOD_CONFIG=$BASEPOD_DIR/config/basepod.yaml
Environment=PODMAN_SOCKET=/run/podman/podman.sock

[Install]
WantedBy=multi-user.target
EOF

    # Configure Caddy to use our Caddyfile
    # If system Caddy service exists, copy our Caddyfile to /etc/caddy/
    if [ -f /lib/systemd/system/caddy.service ] || [ -f /usr/lib/systemd/system/caddy.service ]; then
        log "Using system Caddy service, updating /etc/caddy/Caddyfile..."
        mkdir -p /etc/caddy
        cp "$BASEPOD_DIR/config/Caddyfile" /etc/caddy/Caddyfile
        chown root:root /etc/caddy/Caddyfile
        chmod 644 /etc/caddy/Caddyfile
    else
        # No system service, create our own
        cat > /etc/systemd/system/caddy.service <<EOF
[Unit]
Description=Caddy Web Server
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/bin/caddy run --config $BASEPOD_DIR/config/Caddyfile
ExecReload=/usr/bin/caddy reload --config $BASEPOD_DIR/config/Caddyfile
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
    fi

    # Enable system-wide podman socket (preferred for server use)
    log "Enabling Podman socket..."
    systemctl enable podman.socket 2>/dev/null || true
    systemctl start podman.socket 2>/dev/null || true

    # Also enable user-level podman socket as fallback
    sudo -u "$BASEPOD_USER" systemctl --user enable podman.socket 2>/dev/null || true
    sudo -u "$BASEPOD_USER" systemctl --user start podman.socket 2>/dev/null || true
    loginctl enable-linger "$BASEPOD_USER" 2>/dev/null || true

    # Add basepod user to podman socket group for access
    if [ -S /run/podman/podman.sock ]; then
        chmod 660 /run/podman/podman.sock 2>/dev/null || true
        chgrp "$BASEPOD_USER" /run/podman/podman.sock 2>/dev/null || true
    fi

    systemctl daemon-reload
}

# Create macOS launchd services
create_macos_services() {
    log "Setting up macOS services..."

    # Initialize podman machine if not exists
    log "Initializing Podman machine..."
    local brew_path="/opt/homebrew/bin"
    [ ! -d "$brew_path" ] && brew_path="/usr/local/bin"
    sudo -u "$BASEPOD_USER" "$brew_path/podman" machine init 2>/dev/null || true
    sudo -u "$BASEPOD_USER" "$brew_path/podman" machine start 2>/dev/null || true

    # Get user's home directory
    local USER_HOME=$(eval echo ~$BASEPOD_USER)

    # Create symlink for podman socket (macOS puts it in /var/folders/...)
    log "Setting up Podman socket symlink..."
    local SOCKET_DIR="$USER_HOME/.local/share/containers/podman/machine"
    mkdir -p "$SOCKET_DIR"
    # Find the actual socket location
    local ACTUAL_SOCKET=$(find /var/folders -name "podman-machine-default-api.sock" 2>/dev/null | head -1)
    if [ -n "$ACTUAL_SOCKET" ]; then
        ln -sf "$ACTUAL_SOCKET" "$SOCKET_DIR/podman.sock"
        chown -R "$BASEPOD_USER:staff" "$USER_HOME/.local/share/containers"
    fi

    # Create launchd plist for basepod
    cat > /Library/LaunchDaemons/com.basepod.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.basepod</string>
    <key>UserName</key>
    <string>$BASEPOD_USER</string>
    <key>ProgramArguments</key>
    <array>
        <string>$BASEPOD_DIR/bin/basepod</string>
    </array>
    <key>WorkingDirectory</key>
    <string>$BASEPOD_DIR</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>$USER_HOME</string>
        <key>BASEPOD_CONFIG</key>
        <string>$BASEPOD_DIR/config/basepod.yaml</string>
        <key>PATH</key>
        <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>$BASEPOD_DIR/logs/basepod.log</string>
    <key>StandardErrorPath</key>
    <string>$BASEPOD_DIR/logs/basepod.err</string>
</dict>
</plist>
EOF

    # Create launchd plist for caddy
    local CADDY_PATH=$(which caddy || echo "/opt/homebrew/bin/caddy")
    cat > /Library/LaunchDaemons/com.caddy.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.caddy</string>
    <key>ProgramArguments</key>
    <array>
        <string>$CADDY_PATH</string>
        <string>run</string>
        <string>--config</string>
        <string>$BASEPOD_DIR/config/Caddyfile</string>
    </array>
    <key>WorkingDirectory</key>
    <string>$BASEPOD_DIR</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>$BASEPOD_DIR</string>
        <key>XDG_DATA_HOME</key>
        <string>$BASEPOD_DIR/data</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>$BASEPOD_DIR/logs/caddy.log</string>
    <key>StandardErrorPath</key>
    <string>$BASEPOD_DIR/logs/caddy.err</string>
</dict>
</plist>
EOF

    chmod 644 /Library/LaunchDaemons/com.basepod.plist
    chmod 644 /Library/LaunchDaemons/com.caddy.plist
}

# Set permissions
set_permissions() {
    log "Setting permissions..."

    # Create builds directory for source deployments
    mkdir -p "$BASEPOD_DIR/builds"

    # Create Caddy data directory (for certificates)
    mkdir -p "$BASEPOD_DIR/data/caddy"
    mkdir -p "$BASEPOD_DIR/Library/Application Support/Caddy"

    # macOS uses 'staff' group, Linux uses same as username
    if [ "$OS" = "macos" ]; then
        chown -R "$BASEPOD_USER:staff" "$BASEPOD_DIR"
        # Caddy runs as root, so it needs write access
        chown -R root:wheel "$BASEPOD_DIR/data/caddy"
        chown -R root:wheel "$BASEPOD_DIR/Library"
    else
        chown -R "$BASEPOD_USER:$BASEPOD_USER" "$BASEPOD_DIR"
    fi
    chmod 750 "$BASEPOD_DIR"
    chmod 755 "$BASEPOD_DIR/data/caddy"
    chmod 640 "$BASEPOD_DIR/config/"* 2>/dev/null || true
    chmod 750 "$BASEPOD_DIR/builds"
}

# Start services
start_services() {
    log "Starting services..."

    if [ "$OS" = "macos" ]; then
        launchctl load /Library/LaunchDaemons/com.caddy.plist 2>/dev/null || true
        launchctl load /Library/LaunchDaemons/com.basepod.plist 2>/dev/null || true
        return
    fi

    systemctl enable caddy
    systemctl start caddy

    systemctl enable basepod
    systemctl start basepod
}

# Print success message
print_success() {
    local server_ip
    if [ "$OS" = "macos" ]; then
        server_ip=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo "localhost")
    else
        server_ip=$(hostname -I | awk '{print $1}')
    fi

    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}  Basepod installed successfully!${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""

    if [ -n "$BASEPOD_DOMAIN" ]; then
        echo "  Dashboard: https://d.$BASEPOD_DOMAIN"
        echo "  Apps:      https://appname.$BASEPOD_DOMAIN"
        echo ""
        echo "  SSL certificates will be automatically obtained from Let's Encrypt."
        echo ""
        echo -e "${YELLOW}  Login password:${NC}"
        echo "    $BASEPOD_PASSWORD"
        echo ""
        echo -e "${YELLOW}  Save this password! It won't be shown again.${NC}"
    else
        echo "  Dashboard: http://$server_ip:3000"
        echo ""
        echo -e "${YELLOW}  Note: No domain configured.${NC}"
        echo "  Apps will be accessible via direct port mapping only."
        echo ""
        echo "  To add a domain later, edit: $BASEPOD_DIR/config/basepod.yaml"
        echo "  And set: domain.root: yourdomain.com"
    fi

    echo ""
    echo "  Config:    $BASEPOD_DIR/config/basepod.yaml"

    if [ "$OS" = "macos" ]; then
        echo "  Logs:      tail -f $BASEPOD_DIR/logs/basepod.log"
        echo ""
        echo "  Commands:"
        echo "    sudo launchctl list | grep basepod  # Check status"
        echo "    sudo launchctl unload /Library/LaunchDaemons/com.basepod.plist && sudo launchctl load /Library/LaunchDaemons/com.basepod.plist  # Restart"
        echo "    tail -f $BASEPOD_DIR/logs/basepod.log  # View logs"
    else
        echo "  Logs:      journalctl -u basepod -f"
        echo ""
        echo "  Commands:"
        echo "    systemctl status basepod   # Check status"
        echo "    systemctl restart basepod  # Restart"
        echo "    journalctl -u basepod -f   # View logs"
    fi
    echo ""
}

# Main
main() {
    echo ""
    echo "  ____                                _ "
    echo " | __ )  __ _ ___  ___ _ __   ___   __| |"
    echo " |  _ \ / _\` / __|/ _ \ '_ \ / _ \ / _\` |"
    echo " | |_) | (_| \__ \  __/ |_) | (_) | (_| |"
    echo " |____/ \__,_|___/\___| .__/ \___/ \__,_|"
    echo "                      |_|                "
    echo ""
    echo " Self-hosted PaaS with Podman & Caddy"
    echo ""

    # Ask for domain configuration first
    setup_domain

    install_deps
    install_caddy
    create_user
    install_basepod
    create_config
    create_caddyfile
    create_services
    set_permissions
    start_services
    print_success
}

# Uninstall function
uninstall() {
    echo ""
    echo -e "${RED}========================================${NC}"
    echo -e "${RED}       Uninstalling Basepod           ${NC}"
    echo -e "${RED}========================================${NC}"
    echo ""

    # Stop services
    log "Stopping services..."
    systemctl stop basepod 2>/dev/null || true
    systemctl disable basepod 2>/dev/null || true

    # Remove systemd service
    log "Removing systemd service..."
    rm -f /etc/systemd/system/basepod.service
    systemctl daemon-reload

    # Remove binary symlink
    log "Removing binary..."
    rm -f /usr/local/bin/basepod

    # Ask about data
    if [ -t 0 ]; then
        ask "Remove all data and config? (y/N):"
        read -r REMOVE_DATA
    else
        REMOVE_DATA="n"
    fi

    if [ "$REMOVE_DATA" = "y" ] || [ "$REMOVE_DATA" = "Y" ]; then
        log "Removing data directory..."
        rm -rf "$BASEPOD_DIR"

        # Remove user
        log "Removing user..."
        userdel -r "$BASEPOD_USER" 2>/dev/null || true
    else
        warn "Keeping data directory: $BASEPOD_DIR"
        warn "Keeping user: $BASEPOD_USER"
    fi

    echo ""
    echo -e "${GREEN}Basepod uninstalled.${NC}"
    echo ""
    echo "Note: Caddy and Podman were NOT removed."
    echo "To remove them manually:"
    echo "  apt remove caddy podman  # Debian/Ubuntu"
    echo "  dnf remove caddy podman  # Fedora/RHEL"
    echo ""
}

# Check for uninstall argument
if [ "${1:-}" = "uninstall" ] || [ "${1:-}" = "--uninstall" ]; then
    uninstall
    exit 0
fi

main "$@"
