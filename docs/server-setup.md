# Server Setup Guide

This guide walks you through setting up Basepod on a VPS or Mac.

## Prerequisites

### 1. System Requirements

**Linux VPS:**
- **OS**: Ubuntu 22.04+, Debian 12+, Fedora 39+
- **RAM**: 1GB minimum (2GB+ recommended)
- **Storage**: 20GB+ recommended
- **Network**: Public IP address

**macOS (Mac Mini, MacBook):**
- **OS**: macOS 13+ (Ventura or later)
- **Chip**: Apple Silicon (M1/M2/M3/M4) recommended for LLM features
- **RAM**: 8GB minimum (16GB+ for LLMs)
- **Network**: Public IP or port forwarding for external access

### 2. Domain Setup

1. Register a domain (e.g., `example.com`)
2. Create DNS records:
   ```
   A    d.example.com    → YOUR_SERVER_IP
   A    *.d.example.com  → YOUR_SERVER_IP  (for wildcard subdomains)
   ```

### 3. Install Podman

**Ubuntu/Debian:**
```bash
sudo apt update
sudo apt install -y podman
```

**Fedora:**
```bash
sudo dnf install -y podman
```

**macOS:**
```bash
brew install podman
podman machine init
podman machine start
```

### 4. Enable Podman Socket

**Linux (systemd):**
```bash
systemctl --user enable --now podman.socket
```

**macOS:**
The socket is automatically started with `podman machine start`.

**Verify:**
```bash
podman info
```

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://pod.base.al/install | sudo bash
```

The installer will prompt for:
- **Domain**: Your server's domain (e.g., `d.example.com`)
- **Email**: For Let's Encrypt SSL certificates
- **Password**: Admin password for the web UI

### Manual Install

```bash
# Create directory structure
sudo mkdir -p /usr/local/basepod/{bin,config,data/apps,data/certs,logs,caddy,tmp}

# Download binaries
ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
curl -fsSL "https://github.com/base-go/basepod/releases/latest/download/basepod-${OS}-${ARCH}" -o /usr/local/basepod/bin/basepod
chmod +x /usr/local/basepod/bin/basepod

# Download Caddy
curl -fsSL "https://caddyserver.com/api/download?os=${OS}&arch=${ARCH}" -o /usr/local/basepod/bin/caddy
chmod +x /usr/local/basepod/bin/caddy

# Allow Caddy to bind to ports 80/443 (Linux only)
sudo setcap 'cap_net_bind_service=+ep' /usr/local/basepod/bin/caddy
```

## Configuration

Edit `/usr/local/basepod/config/basepod.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 443
  api_port: 3000
  log_level: "info"

domain:
  root: "d.example.com"    # ← Change this!
  wildcard: true
  email: "your-email@example.com" # ← Change this!

podman:
  socket_path: ""  # Auto-detected
  network: "basepod"

database:
  path: "data/basepod.db"
```

## Starting Services

### Linux (systemd)

```bash
# Enable and start services
sudo systemctl enable --now basepod
sudo systemctl enable --now basepod-caddy

# Check status
sudo systemctl status basepod
sudo systemctl status basepod-caddy

# View logs
sudo journalctl -u basepod -f
sudo journalctl -u basepod-caddy -f
```

### macOS (launchd)

```bash
# Load services
sudo launchctl load /Library/LaunchDaemons/al.base.basepod.plist
sudo launchctl load /Library/LaunchDaemons/al.base.caddy.plist

# Check status
sudo launchctl list | grep basepod
sudo launchctl list | grep caddy

# View logs
tail -f /usr/local/basepod/logs/basepod.log
tail -f /usr/local/basepod/logs/caddy.log
```

## Verification

1. **Check API health:**
   ```bash
   curl http://localhost:3000/api/health
   ```

2. **Access Web UI:**
   Open `https://d.example.com` in your browser

3. **Verify SSL:**
   The certificate should be automatically issued by Let's Encrypt

## Troubleshooting

### Podman socket not found

**Linux:**
```bash
# Check socket location
echo $XDG_RUNTIME_DIR/podman/podman.sock

# Start socket manually
podman system service --time=0 &
```

**macOS:**
```bash
# Check if machine is running
podman machine list

# Start machine if needed
podman machine start

# Get socket path
podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}'
```

### Port 80/443 permission denied

**Linux:**
```bash
# Grant capability to Caddy
sudo setcap 'cap_net_bind_service=+ep' /usr/local/basepod/bin/caddy
```

**macOS:**
Caddy runs as root via launchd, so this shouldn't be an issue.

### SSL certificate not issued

1. Verify DNS is pointing to your server
2. Check Caddy logs for ACME errors
3. Ensure ports 80/443 are accessible from the internet

### Caddy TLS errors on macOS

Ensure the Caddy plist has the correct environment variables:
```bash
sudo launchctl unload /Library/LaunchDaemons/al.base.caddy.plist
# Edit the plist to include HOME and XDG_DATA_HOME
sudo launchctl load /Library/LaunchDaemons/al.base.caddy.plist
```

### Reset admin password

If you forgot your admin password:

1. Edit the config file:
   ```bash
   sudo nano /usr/local/basepod/config/basepod.yaml
   ```

2. Clear the password hash:
   ```yaml
   auth:
     password_hash: ""
   ```

3. Restart basepod:

   **Linux:**
   ```bash
   sudo systemctl restart basepod
   ```

   **macOS:**
   ```bash
   sudo launchctl unload /Library/LaunchDaemons/com.basepod.plist
   sudo launchctl load /Library/LaunchDaemons/com.basepod.plist
   ```

4. Visit your dashboard URL - you'll be prompted to set a new password.

## Security Recommendations

1. **Firewall**: Allow only ports 22 (SSH), 80, 443
2. **SSH**: Disable password auth, use keys only
3. **Updates**: Keep system and Podman updated
4. **Backups**: Regularly backup `/usr/local/basepod/data/`

## Updating

### Via Web UI

1. Go to Settings
2. Click "Check for Updates"
3. If available, click "Update Now"

### Via CLI

```bash
bp status  # Check current version
# Download and replace binary manually, then restart services
```

## Uninstall

**Linux:**
```bash
sudo systemctl stop basepod basepod-caddy
sudo systemctl disable basepod basepod-caddy
sudo rm -rf /usr/local/basepod
sudo rm /etc/systemd/system/basepod*.service
```

**macOS:**
```bash
sudo launchctl unload /Library/LaunchDaemons/al.base.basepod.plist
sudo launchctl unload /Library/LaunchDaemons/al.base.caddy.plist
sudo rm -rf /usr/local/basepod
sudo rm /Library/LaunchDaemons/al.base.*.plist
```
