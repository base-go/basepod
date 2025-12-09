# Deployer

A self-hosted Platform as a Service (PaaS) built with **Go**, **Podman**, and **Caddy**. Deploy your applications with ease, similar to CapRover but using rootless containers.

## Features

- **Easy Deployments** - Deploy from Docker images or Git repositories
- **Automatic SSL** - Free HTTPS via Let's Encrypt with Caddy
- **Rootless Containers** - Powered by Podman, no root required
- **Modern Web UI** - Built with Nuxt 4 and Nuxt UI 4
- **Powerful CLI** - Full control from the command line
- **OS Agnostic** - Works on Linux and macOS

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Frontend                             │
│              Nuxt 4 + Nuxt UI 4 (Vue 3)                    │
├─────────────────────────────────────────────────────────────┤
│                        REST API                             │
│                    Deployer (Go)                            │
├──────────┬──────────┬───────────┬────────────┬─────────────┤
│   Apps   │  Proxy   │    SSL    │   Deploy   │   Storage   │
│  Manager │ (Caddy)  │  (ACME)   │   Engine   │  (SQLite)   │
├──────────┴──────────┴───────────┴────────────┴─────────────┤
│                      Podman                                 │
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### Server Installation (VPS)

```bash
# Download and run installer
curl -sSL https://raw.githubusercontent.com/deployer/deployer/main/scripts/install.sh | bash

# Configure your domain
nano ~/deployer/config/deployer.yaml

# Start services
systemctl --user enable --now deployer deployer-caddy
```

### CLI Installation (Local)

```bash
# Install CLI only
curl -sSL https://raw.githubusercontent.com/deployer/deployer/main/scripts/install-cli.sh | bash

# Login to your server
deployerctl login https://deployer.yourdomain.com
```

## Usage

### Web UI

Access your deployer dashboard at `https://deployer.yourdomain.com`

- Create and manage apps
- Monitor deployments
- View logs in real-time
- Configure environment variables

### CLI

```bash
# List all apps
deployerctl apps

# Create a new app
deployerctl create myapp --domain myapp.example.com

# Deploy from Docker image
deployerctl deploy myapp --image nginx:latest

# View logs
deployerctl logs myapp --tail 100

# Start/stop/restart
deployerctl start myapp
deployerctl stop myapp
deployerctl restart myapp

# Delete an app
deployerctl delete myapp
```

### REST API

```bash
# List apps
curl https://deployer.example.com/api/apps

# Create app
curl -X POST https://deployer.example.com/api/apps \
  -H "Content-Type: application/json" \
  -d '{"name": "myapp", "domain": "myapp.example.com"}'

# Deploy
curl -X POST https://deployer.example.com/api/apps/myapp/deploy \
  -H "Content-Type: application/json" \
  -d '{"image": "nginx:latest"}'
```

## Configuration

Configuration file: `~/deployer/config/deployer.yaml`

```yaml
server:
  host: "0.0.0.0"
  port: 443
  api_port: 3000
  log_level: "info"

domain:
  root: "deployer.example.com"  # Your deployer domain
  wildcard: true                 # Enable *.deployer.example.com
  email: "admin@example.com"     # For Let's Encrypt

podman:
  socket_path: ""    # Auto-detected
  network: "deployer"

database:
  path: "data/deployer.db"
```

## Directory Structure

```
~/deployer/
├── bin/                    # Binaries
│   ├── deployer            # Server
│   ├── deployerctl         # CLI
│   └── caddy               # Reverse proxy
├── config/
│   └── deployer.yaml       # Configuration
├── data/
│   ├── deployer.db         # SQLite database
│   ├── apps/               # App data
│   └── certs/              # SSL certificates
├── logs/
│   └── deployer.log
├── caddy/
│   └── Caddyfile           # Generated Caddy config
└── tmp/                    # Build artifacts
```

## Requirements

### Server (VPS)
- Linux (Ubuntu 22.04+, Fedora, Debian) or macOS
- Podman 4.0+
- 1GB RAM minimum (2GB+ recommended)
- Domain name with DNS configured

### Local (CLI)
- Linux or macOS
- Go 1.25+ (for building from source)

## Development

```bash
# Clone repository
git clone https://github.com/deployer/deployer.git
cd deployer

# Install Go dependencies
go mod download

# Run server
go run ./cmd/deployer

# Run CLI
go run ./cmd/deployerctl

# Build
make build

# Run web UI (separate terminal)
cd web
npm install
npm run dev
```

## Comparison with CapRover

| Feature | Deployer | CapRover |
|---------|----------|----------|
| Container Runtime | Podman (rootless) | Docker (root) |
| Reverse Proxy | Caddy | Nginx |
| Language | Go | Node.js |
| Installation | User-space (~/) | System-wide |
| SSL | Auto (built-in) | Auto (Let's Encrypt) |
| Web UI | Nuxt 4 | React |
| Multi-node | Planned | Docker Swarm |

## Roadmap

- [ ] Git push deployments
- [ ] Dockerfile builds
- [ ] Environment variable encryption
- [ ] App templates (one-click deploys)
- [ ] Multi-node support
- [ ] Backup & restore
- [ ] Metrics & monitoring
- [ ] Authentication & RBAC

## License

MIT License - see [LICENSE](LICENSE)

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) first.
