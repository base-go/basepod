# Server Configuration

Basepod server configuration is stored in `/usr/local/basepod/config/basepod.yaml`.

## Configuration File

```yaml
server:
  host: "0.0.0.0"
  port: 443
  api_port: 3000
  log_level: "info"

domain:
  root: "example.com"       # Base domain for apps
  dashboard: "bp"           # Dashboard at bp.example.com
  wildcard: true
  email: "admin@example.com" # For Let's Encrypt

auth:
  password_hash: "..."      # Set via UI or install script

podman:
  socket_path: ""           # Auto-detected
  network: "basepod"

database:
  path: "data/basepod.db"
```

## Options

### server

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `host` | string | `0.0.0.0` | Listen address |
| `port` | int | `443` | HTTPS port |
| `api_port` | int | `3000` | API port (internal) |
| `log_level` | string | `info` | Log level: debug, info, warn, error |

### domain

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `root` | string | - | Base domain (e.g., `example.com`) |
| `dashboard` | string | `bp` | Dashboard subdomain |
| `wildcard` | bool | `true` | Enable wildcard subdomains |
| `email` | string | - | Email for SSL certificates |

### podman

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `socket_path` | string | auto | Podman socket path |
| `network` | string | `basepod` | Container network name |

### database

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `path` | string | `data/basepod.db` | SQLite database path |

## Directory Structure

```
/usr/local/basepod/
├── bin/
│   ├── basepod         # Server binary
│   └── caddy           # Caddy binary
├── config/
│   ├── basepod.yaml    # Server config
│   └── Caddyfile       # Caddy config
├── data/
│   ├── apps/           # App data
│   ├── builds/         # Build artifacts
│   ├── certs/          # SSL certificates
│   └── basepod.db      # Database
├── logs/
│   ├── basepod.log
│   └── caddy.log
└── caddy/              # Caddy state
```

## Environment Variables

Override config with environment variables:

| Variable | Description |
|----------|-------------|
| `BASEPOD_CONFIG` | Config file path |
| `BASEPOD_LOG_LEVEL` | Log level |

## Updating Configuration

### Via CLI

```bash
bp config set domain.root=example.com
bp config set domain.email=admin@example.com
```

### Manually

1. Edit `/usr/local/basepod/config/basepod.yaml`
2. Restart services:

**Linux:**
```bash
sudo systemctl restart basepod
```

**macOS:**
```bash
sudo launchctl unload /Library/LaunchDaemons/al.base.basepod.plist
sudo launchctl load /Library/LaunchDaemons/al.base.basepod.plist
```
