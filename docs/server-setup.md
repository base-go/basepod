# Server Setup Guide

This guide walks you through setting up Deployer on a VPS.

## Prerequisites

### 1. VPS Requirements

- **OS**: Ubuntu 22.04+, Debian 12+, Fedora 39+, or macOS
- **RAM**: 1GB minimum (2GB+ recommended)
- **Storage**: 20GB+ recommended
- **Network**: Public IP address

### 2. Domain Setup

1. Register a domain (e.g., `example.com`)
2. Create DNS records:
   ```
   A    deployer.example.com    → YOUR_VPS_IP
   A    *.deployer.example.com  → YOUR_VPS_IP  (optional, for wildcard)
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

**Verify:**
```bash
podman info
```

## Installation

### Option 1: Automated Install

```bash
curl -sSL https://raw.githubusercontent.com/deployer/deployer/main/scripts/install.sh | bash
```

### Option 2: Manual Install

```bash
# Create directory structure
mkdir -p ~/deployer/{bin,config,data/apps,data/certs,logs,caddy,tmp}

# Download binaries (or build from source)
# ... see Development section

# Download Caddy
curl -fsSL "https://caddyserver.com/api/download?os=linux&arch=amd64" -o ~/deployer/bin/caddy
chmod +x ~/deployer/bin/caddy

# Allow Caddy to bind to ports 80/443 (one-time sudo)
sudo setcap 'cap_net_bind_service=+ep' ~/deployer/bin/caddy
```

## Configuration

Edit `~/deployer/config/deployer.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 443
  api_port: 3000
  log_level: "info"

domain:
  root: "deployer.example.com"    # ← Change this!
  wildcard: true
  email: "your-email@example.com" # ← Change this!

podman:
  socket_path: ""  # Auto-detected
  network: "deployer"

database:
  path: "data/deployer.db"
```

## Starting Services

### Linux (systemd)

```bash
# Enable and start services
systemctl --user enable --now deployer
systemctl --user enable --now deployer-caddy

# Check status
systemctl --user status deployer
systemctl --user status deployer-caddy

# View logs
journalctl --user -u deployer -f
journalctl --user -u deployer-caddy -f
```

### Manual Start

```bash
# Start Deployer API
~/deployer/bin/deployer &

# Start Caddy
~/deployer/bin/caddy run --config ~/deployer/caddy/Caddyfile &
```

## Verification

1. **Check API health:**
   ```bash
   curl http://localhost:3000/api/health
   ```

2. **Access Web UI:**
   Open `https://deployer.example.com` in your browser

3. **Verify SSL:**
   The certificate should be automatically issued by Let's Encrypt

## Troubleshooting

### Podman socket not found

```bash
# Check socket location
echo $XDG_RUNTIME_DIR/podman/podman.sock

# Start socket manually
podman system service --time=0 &
```

### Port 80/443 permission denied

```bash
# Grant capability to Caddy
sudo setcap 'cap_net_bind_service=+ep' ~/deployer/bin/caddy
```

### SSL certificate not issued

1. Verify DNS is pointing to your server
2. Check Caddy logs for ACME errors
3. Ensure ports 80/443 are accessible from the internet

### Database errors

```bash
# Reset database (WARNING: deletes all data)
rm ~/deployer/data/deployer.db
~/deployer/bin/deployer --setup
```

## Security Recommendations

1. **Firewall**: Allow only ports 22 (SSH), 80, 443
2. **SSH**: Disable password auth, use keys only
3. **Updates**: Keep system and Podman updated
4. **Backups**: Regularly backup `~/deployer/data/`

## Updating

```bash
# Stop services
systemctl --user stop deployer deployer-caddy

# Download new binaries or rebuild
make build && make install

# Start services
systemctl --user start deployer deployer-caddy
```
