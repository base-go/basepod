# Basepod

Turn your Mac Mini into a personal server. Deploy apps, host websites, and run local LLMs on Apple Silicon - all in one place.

Built with Go, Podman, and Caddy.

## Features

- **One-click deployments** - Deploy from Docker images, Git repos, or local source
- **Automatic SSL** - Free TLS certificates via Caddy
- **Local LLM support** - Run MLX models on Apple Silicon with OpenAI-compatible API
- **Simple CLI** - `bp push` to deploy your app
- **Web UI** - Modern dashboard for managing apps
- **Rootless** - Runs entirely in userspace with Podman
- **Templates** - 25+ pre-configured app templates

## Quick Start

### Install Server

```bash
curl -fsSL https://pod.base.al/install | bash
```

### Install CLI

```bash
curl -fsSL https://pod.base.al/cli | bash
```

Or download manually:

```bash
# macOS (Apple Silicon)
curl -fsSL https://github.com/base-go/basepod/releases/latest/download/bp-darwin-arm64 -o /usr/local/bin/bp
chmod +x /usr/local/bin/bp

# Linux (AMD64)
curl -fsSL https://github.com/base-go/basepod/releases/latest/download/bp-linux-amd64 -o /usr/local/bin/bp
chmod +x /usr/local/bin/bp
```

## Usage

### Login to your server

```bash
bp login your-server.com
```

### Deploy an app

```bash
# Initialize a new app
cd myapp
bp init

# Deploy
bp push
```

### Using Docker images

```bash
bp create myapp
bp deploy myapp --image nginx:latest
```

### CLI Commands

```
bp login <server>    Login to a Basepod server
bp logout            Logout from current server
bp context           List or switch server contexts
bp apps              List all apps
bp create <name>     Create a new app
bp push              Deploy from local source
bp deploy <name>     Deploy with Docker image
bp logs <name>       View app logs
bp start <name>      Start an app
bp stop <name>       Stop an app
bp restart <name>    Restart an app
bp delete <name>     Delete an app
bp info              Show server info
```

## Configuration

### App Configuration (basepod.yaml)

```yaml
name: myapp
port: 3000
build:
  dockerfile: Dockerfile
  context: .
env:
  NODE_ENV: production
```

### Server Configuration (~/.basepod/config/basepod.yaml)

```yaml
server:
  api_port: 3000

domain:
  base: apps.example.com

podman:
  network: basepod

database:
  path: data/basepod.db
```

## Architecture

```
+-------------+     +-------------+     +-------------+
|   bp CLI    |---->|   Basepod   |---->|   Podman    |
+-------------+     |   Server    |     |  Containers |
                    +------+------+     +-------------+
                           |
                    +------v------+
                    |    Caddy    |
                    |   (Proxy)   |
                    +-------------+
```

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

## Requirements

- **Server**: Linux or macOS with Podman and Caddy
- **CLI**: Any OS (macOS, Linux)
- **LLM**: macOS with Apple Silicon (M1/M2/M3/M4) for MLX support

## Development

### Prerequisites

- Go 1.24+
- Node.js 20+ / Bun
- Podman
- Caddy

### Build from source

```bash
git clone https://github.com/base-go/basepod.git
cd basepod

# Build server
go build -o basepod ./cmd/basepod

# Build CLI
go build -o bp ./cmd/bp

# Build web UI
cd web && bun install && bun run generate
```

### Run in development

```bash
./scripts/dev.sh
```

## License

MIT License
