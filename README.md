# Deployer

A self-hosted Platform as a Service (PaaS) built with **Go**, **Podman**, and **Caddy**. Deploy your applications with ease, similar to CapRover but using rootless containers.

## Features

- **Easy Deployments** - Deploy from Docker images or source code (Dockerfile)
- **Automatic SSL** - Free HTTPS via Let's Encrypt with Caddy
- **Rootless Containers** - Powered by Podman, no root required
- **Modern Web UI** - Built with Nuxt 4 and Nuxt UI 4
- **Multi-Server CLI** - Deploy to multiple servers with context switching
- **One-Click Apps** - Pre-configured templates for popular services

## Architecture

```
+---------------------------------------------------------+
|                     Clients                              |
|        deployer CLI (macOS/Linux)                       |
+---------------------------------------------------------+
                          |
                          v
+---------------------------------------------------------+
|                  Deployer Server                         |
|              (deployerd on VPS)                         |
+--------+--------+-----------+------------+--------------+
|  Apps  | Proxy  |    SSL    |   Deploy   |   Storage    |
| Manager| (Caddy)|   (ACME)  |   Engine   |   (SQLite)   |
+--------+--------+-----------+------------+--------------+
|                      Podman                              |
+----------------------------------------------------------+
```

---

## Server Install (VPS/Linux)

Install Deployer on your Linux VPS with one command:

```bash
curl -fsSL https://raw.githubusercontent.com/base-go/deployer/main/install.sh | sudo bash
```

With a custom domain (recommended - enables automatic SSL):

```bash
DEPLOYER_DOMAIN=example.com curl -fsSL https://raw.githubusercontent.com/base-go/deployer/main/install.sh | sudo bash
```

### What Gets Installed

- **deployerd** - The server binary at `/opt/deployer/bin/deployer`
- **Podman** - Container runtime (rootless)
- **Caddy** - Reverse proxy with automatic SSL
- **SQLite** - App database at `/opt/deployer/data/`

### After Install

| Item | Location |
|------|----------|
| Dashboard | `https://d.example.com` |
| Apps | `https://appname.example.com` |
| Config | `/opt/deployer/config/deployer.yaml` |
| Logs | `journalctl -u deployer -f` |

**Save the password shown after install - it won't be displayed again.**

### DNS Setup

Point a wildcard DNS record to your server:

```
*.example.com  ->  YOUR_SERVER_IP
```

Or for specific subdomains:
```
d.example.com     ->  YOUR_SERVER_IP  (dashboard)
myapp.example.com ->  YOUR_SERVER_IP  (apps)
```

### Supported OS

Ubuntu, Debian, Fedora, CentOS, Rocky Linux, Alma Linux, Arch Linux

---

## Client Install (CLI)

The `deployer` CLI lets you deploy from your local machine to any Deployer server.

### macOS

```bash
# Using Homebrew (coming soon)
brew install base-go/tap/deployer

# Or download manually
curl -fsSL https://github.com/base-go/deployer/releases/latest/download/deployer-darwin-arm64 -o /usr/local/bin/deployer
chmod +x /usr/local/bin/deployer
```

### Linux

```bash
# AMD64
curl -fsSL https://github.com/base-go/deployer/releases/latest/download/deployer-linux-amd64 -o /usr/local/bin/deployer
chmod +x /usr/local/bin/deployer

# ARM64
curl -fsSL https://github.com/base-go/deployer/releases/latest/download/deployer-linux-arm64 -o /usr/local/bin/deployer
chmod +x /usr/local/bin/deployer
```

### CLI Commands

```bash
# Login to a server (saves token for future use)
deployer login d.example.com

# List configured servers
deployer context

# Switch active server
deployer context use production

# Deploy current directory (requires Dockerfile)
deployer push myapp

# Deploy to specific server
deployer push myapp --server d.example.com
```

### Project Configuration

Create `deployer.yaml` in your project root:

```yaml
# Optional: specify which server to deploy to
server: d.example.com

# App settings (optional)
name: myapp
port: 3000
```

### Deploy Workflow

```bash
# 1. Login once
deployer login d.example.com
# Enter password when prompted

# 2. Create Dockerfile in your project
# 3. Deploy
cd myproject
deployer push myapp

# Your app will be live at https://myapp.example.com
```

---

## One-Click Apps

Pre-configured templates available from the dashboard:

| Category | Apps |
|----------|------|
| Databases | MySQL, PostgreSQL, MariaDB, MongoDB, Redis |
| Admin Tools | phpMyAdmin, Adminer, pgAdmin |
| Web Servers | Nginx, Apache, Caddy |
| CMS | WordPress, Ghost |
| Dev Tools | Gitea, Portainer, Code Server, Uptime Kuma |
| Analytics | Grafana, Plausible Analytics |
| Storage | MinIO, File Browser |
| Automation | n8n |

---

## REST API

All endpoints require authentication (token from login).

```bash
# List apps
curl -H "Authorization: Bearer TOKEN" https://d.example.com/api/apps

# Create app from image
curl -X POST https://d.example.com/api/apps \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "myapp", "image": "nginx:latest"}'

# Deploy from template
curl -X POST https://d.example.com/api/apps/from-template \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"template_id": "postgres", "name": "mydb"}'

# Get templates
curl https://d.example.com/api/templates
```

---

## Local Development (macOS)

For developing Deployer itself or testing locally with `*.pod` domains.

### Setup

```bash
# Install dependencies
brew install podman caddy dnsmasq

# Initialize Podman
podman machine init
podman machine start

# Configure local DNS for *.pod domains
echo -e "address=/pod/127.0.0.2\nlisten-address=127.0.0.2\nport=53" | sudo tee /opt/homebrew/etc/dnsmasq.conf
sudo ifconfig lo0 alias 127.0.0.2
sudo mkdir -p /etc/resolver
sudo bash -c 'echo "nameserver 127.0.0.2" > /etc/resolver/pod'
sudo /opt/homebrew/sbin/dnsmasq

# Port forward 80 to 8080
echo "rdr pass on lo0 inet proto tcp from any to 127.0.0.2 port 80 -> 127.0.0.2 port 8080" | sudo pfctl -ef -
```

### Build and Run

```bash
# Backend
go build -o deployer ./cmd/deployer
./deployer

# Frontend (in another terminal)
cd web
bun install
bun dev
```

Access at http://localhost:3000. Apps get `*.pod` domains locally.

---

## Comparison with CapRover

| Feature | Deployer | CapRover |
|---------|----------|----------|
| Container Runtime | Podman (rootless) | Docker (root) |
| Reverse Proxy | Caddy | Nginx |
| Language | Go | Node.js |
| SSL | Auto (Caddy/ACME) | Auto (Let's Encrypt) |
| Multi-server CLI | Yes | No |
| Web UI | Nuxt 4 | React |

---

## Upgrade

```bash
# On the server
curl -fsSL https://raw.githubusercontent.com/base-go/deployer/main/upgrade.sh | sudo bash
```

## Uninstall

```bash
# On the server
curl -fsSL https://raw.githubusercontent.com/base-go/deployer/main/install.sh | sudo bash -s -- --uninstall
```

---

## License

MIT License - see [LICENSE](LICENSE)
