# Deployer Developer Documentation

## Overview

Deployer is a self-hosted PaaS (Platform as a Service) that uses Podman for containers and Caddy for reverse proxy/SSL.

**Architecture:**
```
User Browser/CLI
       |
       v
   Caddy (port 80/443) -- SSL termination, domain routing
       |
       v
   Deployer API (port 3000) -- REST API, Web UI
       |
       v
   Podman -- Container runtime
       |
       v
   App Containers (random ports 10000-60000)
```

---

## Directory Structure

```
deployer/
├── cmd/
│   ├── deployer/main.go      # CLI client binary
│   └── deployerd/main.go     # Server daemon binary
├── internal/
│   ├── api/api.go            # REST API (1775 lines)
│   ├── app/app.go            # Data structures
│   ├── auth/auth.go          # Session management
│   ├── caddy/caddy.go        # Caddy admin API client
│   ├── config/config.go      # Configuration management
│   ├── podman/client.go      # Podman API client
│   ├── proxy/                # Proxy coordination
│   ├── storage/storage.go    # SQLite database
│   ├── templates/templates.go # One-click app templates
│   └── web/                  # Embedded frontend
├── web/                      # Nuxt 4 frontend source
└── Makefile
```

---

## Component Details

### 1. API Server (`internal/api/api.go`)

**Server Struct:**
```go
type Server struct {
    storage   *storage.Storage    // SQLite DB
    podman    podman.Client       // Container runtime
    caddy     *caddy.Client       // Reverse proxy
    config    *config.Config      // App config
    auth      *auth.Manager       // Session management
    router    *http.ServeMux      // HTTP router
    staticFS  http.Handler        // Web UI files
    version   string              // Current version
}
```

**Request Flow in ServeHTTP:**
1. Add CORS headers
2. Check if app domain (e.g., myapp.common.al) -> proxy to container
3. Check if API route (/api/*) -> route to handler
4. Otherwise -> serve static files (Web UI)

**Key Methods:**

| Method | Purpose |
|--------|---------|
| `handleLogin()` | Password auth, create session, set cookie |
| `handleLogout()` | Delete session, clear cookie |
| `handleListApps()` | List all apps from DB |
| `handleCreateApp()` | Create app record |
| `handleGetApp()` | Get single app details |
| `handleUpdateApp()` | Update app config (env, ports, etc.) |
| `handleDeleteApp()` | Stop container, delete from DB |
| `handleDeployApp()` | Deploy from image |
| `handleSourceDeploy()` | Deploy from source tarball (CLI push) |
| `handleDeployTemplate()` | Deploy from template |
| `handleStartApp()` | Start stopped container |
| `handleStopApp()` | Stop running container |
| `handleRestartApp()` | Restart container |
| `handleAppLogs()` | Stream container logs |
| `handleListContainers()` | List all Podman containers |
| `handleSystemInfo()` | Get system stats |
| `handleVersion()` | Check for updates |
| `handleUpdate()` | Self-update binary |
| `handlePrune()` | Cleanup unused resources |
| `proxyToApp()` | Reverse proxy to app container |

### 2. Podman Client (`internal/podman/client.go`)

**Interface:**
```go
type Client interface {
    Ping(ctx) error
    CreateContainer(ctx, CreateContainerOpts) (string, error)
    StartContainer(ctx, id) error
    StopContainer(ctx, id, timeout) error
    RemoveContainer(ctx, id, force) error
    ListContainers(ctx, all) ([]Container, error)
    InspectContainer(ctx, id) (*ContainerInspect, error)
    ContainerLogs(ctx, id, LogOpts) (io.ReadCloser, error)
    PullImage(ctx, image) error
    BuildImage(ctx, BuildOpts) (string, error)
    ListImages(ctx) ([]Image, error)
    RemoveImage(ctx, id, force) error
}
```

**CreateContainerOpts:**
```go
type CreateContainerOpts struct {
    Name    string
    Image   string
    Env     map[string]string
    Command []string              // Custom command override
    Ports   map[string]string     // "containerPort": "hostPort"
    Labels  map[string]string
}
```

**Socket Detection (`config.go:GetPodmanSocket()`):**
- Linux rootful: `/run/podman/podman.sock`
- Linux rootless: `/run/user/{uid}/podman/podman.sock`
- macOS: Query `podman machine inspect` for socket path
- Env override: `PODMAN_SOCKET`

### 3. Caddy Client (`internal/caddy/caddy.go`)

**Purpose:** Configure reverse proxy routes via Caddy Admin API

**Key Methods:**
```go
NewClient(adminURL string) *Client     // Default: http://localhost:2019
Ping() error                           // Check connection
AddRoute(ctx, Route) error             // Add domain->upstream mapping
RemoveRoute(ctx, id string) error      // Remove route by ID
```

**Route Structure:**
```go
type Route struct {
    ID        string  // "deployer-{appname}"
    Domain    string  // "myapp.common.al"
    Upstream  string  // "localhost:32456"
    EnableSSL bool
}
```

### 4. Storage (`internal/storage/storage.go`)

**Database:** SQLite at `~/deployer/data/deployer.db`

**Tables:**
```sql
apps (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE,
    domain TEXT UNIQUE,
    container_id TEXT,
    image TEXT,
    status TEXT DEFAULT 'pending',
    env TEXT,           -- JSON
    ports TEXT,         -- JSON
    volumes TEXT,       -- JSON
    resources TEXT,     -- JSON
    deployment TEXT,    -- JSON
    ssl TEXT,           -- JSON
    created_at, updated_at
)

deployments (
    id, app_id, status, source, image, logs, started_at, finished_at
)

settings (
    key PRIMARY KEY, value, updated_at
)
```

### 5. Templates (`internal/templates/templates.go`)

**Template Structure:**
```go
type Template struct {
    ID          string
    Name        string
    Description string
    Image       string            // Default image
    ImageARM    string            // ARM64-specific image
    Port        int               // Container port
    Env         map[string]string // Default env vars
    Command     []string          // Custom command
    Category    string
    Icon        string
    Arch        []string          // Supported architectures
}
```

**Available Templates:**
- Databases: mysql, mariadb, postgres, mongodb, redis
- Admin: phpmyadmin, adminer, pgadmin
- Web servers: nginx, apache, caddy
- CMS: wordpress, ghost, strapi
- DevTools: gitea, portainer, uptime-kuma, code-server
- Others: mattermost, n8n, plausible, grafana, minio, filebrowser

### 6. Auth (`internal/auth/auth.go`)

**Session Management:**
- In-memory sessions (map[token]*Session)
- 32-byte random token
- 24-hour expiry
- SHA256 password hashing

**Cookie Settings:**
```go
http.Cookie{
    Name:     "deployer_token",
    HttpOnly: true,
    Secure:   isSecure,  // Based on X-Forwarded-Proto
    SameSite: SameSiteLaxMode,
    Expires:  24h,
}
```

---

## API Endpoints

### Authentication
| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | /api/auth/login | No | Login with password |
| POST | /api/auth/logout | No | Logout |
| GET | /api/auth/status | No | Check if auth required |
| POST | /api/auth/change-password | Yes | Change password |

### Apps
| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | /api/apps | Yes | List all apps |
| POST | /api/apps | Yes | Create new app |
| GET | /api/apps/{id} | Yes | Get app details |
| PUT | /api/apps/{id} | Yes | Update app |
| DELETE | /api/apps/{id} | Yes | Delete app |
| POST | /api/apps/{id}/deploy | Yes | Deploy app |
| POST | /api/apps/{id}/start | Yes | Start app |
| POST | /api/apps/{id}/stop | Yes | Stop app |
| POST | /api/apps/{id}/restart | Yes | Restart app |
| GET | /api/apps/{id}/logs | Yes | Stream logs |

### Templates
| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | /api/templates | Yes | List templates |
| POST | /api/templates/{id}/deploy | Yes | Deploy from template |

### System
| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | /api/system/info | Yes | System stats |
| GET | /api/system/config | No | Domain config |
| GET | /api/system/version | Yes | Version info |
| POST | /api/system/update | Yes | Self-update |
| POST | /api/system/prune | Yes | Cleanup resources |

### Other
| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | /api/containers | Yes | List containers |
| POST | /api/deploy | Yes | Deploy from source (CLI) |
| GET | /api/caddy/check | No | On-demand TLS check |
| GET | /health | No | Health check |

---

## Known Issues & Status

### WORKING
- [x] Dashboard loads and shows stats
- [x] Login/logout with password
- [x] List apps
- [x] Create app from image
- [x] Start/stop/restart containers
- [x] View container logs
- [x] Templates page loads
- [x] Deploy from template (creates container)
- [x] Caddy routes apps by domain
- [x] Ghost app accessible at ghost.common.al

### BROKEN / NEEDS FIX
- [ ] **Proxy 302/cookies not forwarded** - `proxyToApp()` follows redirects instead of forwarding them
  - **Fix:** Add `CheckRedirect` to return `http.ErrUseLastResponse`
  - **Impact:** code-server login fails (401)

- [ ] **Template Command not passed** - `deployFromTemplate()` doesn't pass `tmpl.Command` to container
  - **Fix:** Add `Command: tmpl.Command` to CreateContainerOpts
  - **Impact:** code-server binds to localhost only

- [ ] **Container count shows 0** - Dashboard shows 0 containers when there are running containers
  - **Possible cause:** Podman socket detection on rootful vs rootless
  - **Fix:** Added rootful socket check in config.go

- [ ] **Gitea login redirect loop** - After registration, login redirects back to register
  - **Cause:** Session cookies not persisting (related to proxy issue above)

### NEEDS TESTING
- [ ] CLI push deployment
- [ ] Build from Dockerfile
- [ ] Self-update mechanism
- [ ] Prune unused resources
- [ ] Multiple server contexts

---

## Build & Deploy

### Local Development
```bash
# Build server
go build -o deployerd ./cmd/deployerd

# Build CLI
go build -o deployer ./cmd/deployer

# Build frontend
cd web && bun install && bun run build
```

### Production Build (ARM64 Linux)
```bash
# Requires CGO for SQLite
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o deployerd ./cmd/deployerd

# Or build on target server
ssh server "cd /path/to/deployer && CGO_ENABLED=1 go build -o deployerd ./cmd/deployerd"
```

### Deploy to Server
```bash
# Copy binary
scp deployerd root@server:/opt/deployer/bin/deployer

# Restart service
ssh root@server "systemctl restart deployer"

# Verify
ssh root@server "/opt/deployer/bin/deployer version"
```

### Release Process
1. Bump version in `cmd/deployerd/main.go`
2. Commit and push to base-go/deployer
3. Build binaries for all platforms
4. Create release on base-go/basepod with binaries

---

## Configuration Files

### Server Config (`~/deployer/config/deployer.yaml`)
```yaml
server:
  host: 0.0.0.0
  port: 443
  api_port: 3000
  log_level: info

auth:
  password_hash: "<sha256>"

domain:
  root: ""
  base: common.al
  suffix: .pod
  wildcard: true
  email: ""

podman:
  socket_path: ""
  network: deployer

database:
  path: /opt/deployer/data/deployer.db
```

### Systemd Service (`/etc/systemd/system/deployer.service`)
```ini
[Unit]
Description=Deployer PaaS
After=network.target

[Service]
Type=simple
User=deployer
ExecStart=/opt/deployer/bin/deployer
Restart=always

[Install]
WantedBy=multi-user.target
```

### Caddy Config (`/etc/caddy/Caddyfile`)
```
{
    on_demand_tls {
        ask http://localhost:3000/api/caddy/check
    }
}

d.common.al {
    reverse_proxy localhost:3000
}

*.common.al {
    tls {
        on_demand
    }
    reverse_proxy localhost:3000
}
```

---

## Debugging

### Check Deployer Logs
```bash
journalctl -u deployer -f
```

### Check Container Status
```bash
podman ps -a
podman logs deployer-{appname}
```

### Check Caddy Routes
```bash
curl http://localhost:2019/config/apps/http/servers/srv0/routes
```

### Test Proxy Directly
```bash
# Direct to container
curl -X POST http://localhost:31281/login -d 'password=changeme' -v

# Through deployer proxy
curl -X POST -H 'Host: code.common.al' http://localhost:3000/login -d 'password=changeme' -v
```

### Check Database
```bash
sqlite3 ~/deployer/data/deployer.db
.tables
SELECT * FROM apps;
```

---

## Version History

- **0.1.29** - Fix proxy redirect/cookie forwarding, template command support
- **0.1.28** - Cookie SameSite fix, rootful podman detection
- **0.1.27** - Previous stable
