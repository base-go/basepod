# CLI Reference

`bp` is the command-line interface for Basepod.

## Installation

```bash
# Install CLI only (local machine)
curl -fsSL https://pod.base.al/cli | bash

# Or build from source
go build -o bp ./cmd/bp
```

## Quick Start

```bash
# 1. Login to your server
bp login bp.example.com

# 2. Initialize your project
cd myproject
bp init

# 3. Deploy
bp push
```

---

## Configuration

### CLI Config (~/.basepod.yaml)

Stores server connections:

```yaml
current_context: bp.example.com
servers:
  bp.example.com:
    url: https://bp.example.com
    token: "your-auth-token"
```

### App Config (basepod.yaml)

Every project needs a `basepod.yaml`. Create with `bp init`.

**Static site:**
```yaml
name: mysite
type: static
public: dist/
domain: mysite.example.com
```

**Container app:**
```yaml
name: myapi
type: container
port: 3000
domain: myapi.example.com
build:
  dockerfile: Dockerfile
  context: .
env:
  NODE_ENV: production
  DATABASE_URL: postgres://...
volumes:
  - /data
```

---

## Commands

### Connection

#### login

Connect to a Basepod server.

```bash
bp login <server>
```

**Examples:**
```bash
bp login bp.example.com
bp login https://bp.example.com
```

#### logout

Disconnect from a server.

```bash
bp logout [name]
```

#### context

List or switch server contexts.

```bash
bp context           # List all contexts
bp context <name>    # Switch to context
```

**Aliases:** `ctx`

---

### Project Setup

#### init

Initialize a project for deployment. Interactive wizard that detects your project type.

```bash
bp init
```

**Detection & auto-configuration:**

| Detected | Type | Action |
|----------|------|--------|
| `Dockerfile` | Container | Use existing Dockerfile |
| `package.json` | Node/Bun | Generate Dockerfile |
| `go.mod` / `main.go` | Go | Generate Dockerfile |
| `requirements.txt` | Python | Generate Dockerfile |
| `Cargo.toml` | Rust | Generate Dockerfile |
| `*.html` files only | Static | Configure Caddy serving |

**Interactive flow:**

```
$ bp init

Detected: package.json (Node/Bun project)

? App name: (myapi)
? Deployment type:
  > Container (auto-generate Dockerfile)
    Static site

? Runtime:
  > Bun (recommended)
    Node.js

? Start command: (bun run start)
? Port: (3000)
? Domain: (myapi.example.com)

Created:
  - basepod.yaml
  - Dockerfile (generated)

Next: bp push
```

**Generated Dockerfile templates:**

Node/Bun:
```dockerfile
FROM oven/bun:latest
WORKDIR /app
COPY package.json bun.lockb* ./
RUN bun install --frozen-lockfile
COPY . .
EXPOSE 3000
CMD ["bun", "run", "start"]
```

Go:
```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o app .

FROM alpine:latest
COPY --from=build /app/app /app
EXPOSE 8080
CMD ["/app"]
```

Python:
```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]
```

---

### Deployment

#### push

Deploy your project to the server.

```bash
bp push [path]
```

**Prerequisites:**
- Must have `basepod.yaml` (run `bp init` first)
- Must have clean git working tree (no uncommitted changes)

**Checks before deploy:**

| Check | Result |
|-------|--------|
| No `basepod.yaml` | Error: run `bp init` first |
| Uncommitted changes | Error: commit first |
| Not a git repo | Warning, allows push |

**Examples:**
```bash
bp push              # Deploy current directory
bp push ./myapp      # Deploy specific path
bp push --force      # Skip git check (not recommended)
```

**What happens:**

For **static sites** (type: static):
1. Uploads public directory
2. Caddy serves files directly
3. No container needed

For **container apps** (type: container):
1. Creates tarball of source
2. Uploads to server
3. Builds Docker image
4. Starts container
5. Configures Caddy proxy

#### deploy

Deploy using a Docker image or Git repository.

```bash
bp deploy <name> [flags]
```

**Flags:**
- `--image, -i` - Docker image to deploy
- `--git, -g` - Git repository URL
- `--branch, -b` - Git branch (default: main)

**Examples:**
```bash
bp deploy myapp --image nginx:latest
bp deploy myapp --image ghcr.io/user/myapp:v1.0
bp deploy myapp --git https://github.com/user/repo.git
bp deploy myapp --git https://github.com/user/repo.git --branch develop
```

---

### App Management

#### apps

List all applications.

```bash
bp apps
```

**Aliases:** `app`, `list`, `ls`

**Output:**
```
NAME        STATUS    DOMAIN                IMAGE
myapp       running   myapp.example.com     nginx:latest
api         stopped   api.example.com       node:20
```

#### create

Create a new application.

```bash
bp create <name> [flags]
```

**Flags:**
- `--domain, -d` - Custom domain
- `--image, -i` - Docker image

**Examples:**
```bash
bp create myapp
bp create myapp --domain myapp.example.com
bp create myapp --image nginx:latest
```

#### update

Update an existing application's configuration.

```bash
bp update <name> [flags]
```

**Flags:**
- `--domain, -d` - Change domain
- `--port, -p` - Change port
- `--env, -e` - Set environment variable (KEY=value)
- `--image, -i` - Change image

**Examples:**
```bash
bp update myapp --domain newdomain.example.com
bp update myapp --env DATABASE_URL=postgres://...
bp update myapp --env DEBUG=true --env LOG_LEVEL=info
```

#### delete

Delete an application.

```bash
bp delete <name>
```

**Aliases:** `rm`

#### logs

View application logs.

```bash
bp logs <name> [flags]
```

**Flags:**
- `--tail, -n` - Number of lines (default: 100)
- `--follow, -f` - Stream logs in real-time

**Examples:**
```bash
bp logs myapp
bp logs myapp -n 50
bp logs myapp -f
```

#### start / stop / restart

Control application lifecycle.

```bash
bp start <name>
bp stop <name>
bp restart <name>
```

---

### One-Click Templates

#### templates

List available one-click app templates.

```bash
bp templates
```

**Output:**
```
CATEGORY      NAME            DESCRIPTION
databases     mysql           MySQL Database Server
databases     postgres        PostgreSQL Database
databases     redis           Redis Cache
cms           wordpress       WordPress CMS
cms           ghost           Ghost Publishing Platform
devtools      gitea           Self-hosted Git Service
devtools      portainer       Container Management UI
...
```

**Filter by category:**
```bash
bp templates --category databases
bp templates -c devtools
```

#### template info

Show template details.

```bash
bp template info <name>
```

**Output:**
```
Name: postgres
Category: databases
Description: PostgreSQL Database

Available versions: 17, 16, 15, 14, 13
Default port: 5432
Volumes: /var/lib/postgresql/data

Environment variables:
  POSTGRES_USER     - Database user (default: postgres)
  POSTGRES_PASSWORD - Database password (required)
  POSTGRES_DB       - Database name (default: postgres)
```

#### template deploy

Deploy a one-click template.

```bash
bp template deploy <name> [flags]
```

**Flags:**
- `--name, -n` - App name (default: template name)
- `--version, -v` - Image version (default: latest)
- `--env, -e` - Environment variable

**Examples:**
```bash
bp template deploy postgres
bp template deploy postgres --name mydb --version 16
bp template deploy postgres -e POSTGRES_PASSWORD=secret -e POSTGRES_DB=myapp
bp template deploy redis --name cache
```

---

### LLM Management (Apple Silicon)

MLX-powered local LLMs for macOS with Apple Silicon.

#### models

List available and downloaded LLM models.

```bash
bp models
```

**Output:**
```
DOWNLOADED:
  NAME                           SIZE    RAM
  mlx-community/Llama-3.2-3B     2.1GB   3GB
  mlx-community/Mistral-7B       4.5GB   5GB

AVAILABLE:
  mlx-community/Llama-3.2-1B     0.8GB   1GB
  mlx-community/Phi-4            8.5GB   9GB
  mlx-community/Qwen2.5-Coder    4.2GB   5GB
  ...
```

**Filter:**
```bash
bp models --downloaded      # Show only downloaded
bp models --category code   # Filter by category
```

#### model pull

Download an LLM model.

```bash
bp model pull <model>
```

**Examples:**
```bash
bp model pull mlx-community/Llama-3.2-3B-Instruct-4bit
bp model pull Mistral-7B    # Short name lookup
```

**Progress output:**
```
Pulling mlx-community/Llama-3.2-3B-Instruct-4bit...
Downloading: 45% [=====>      ] 1.2GB/2.1GB  12.5 MB/s  ETA: 1m 12s
```

#### model run

Start the LLM server with a model.

```bash
bp model run <model>
```

**Examples:**
```bash
bp model run Llama-3.2-3B
bp model run Mistral-7B
```

**Output:**
```
Starting LLM server with Llama-3.2-3B...
Server running at: https://llm.example.com
API endpoint: https://llm.example.com/v1/chat/completions

Compatible with OpenAI API format.
```

#### model stop

Stop the running LLM server.

```bash
bp model stop
```

#### model rm

Delete a downloaded model.

```bash
bp model rm <model>
```

#### chat

Interactive chat with the running LLM.

```bash
bp chat
```

**Interactive session:**
```
Connected to Llama-3.2-3B at llm.example.com

You: What is the capital of France?
AI: The capital of France is Paris.

You: /exit
Goodbye!
```

**Flags:**
```bash
bp chat                    # Use running model
bp chat --model Mistral    # Start specific model if not running
```

---

### System Administration

#### info

Show server information.

```bash
bp info
```

**Output:**
```
Server: https://bp.example.com
Version: 1.0.5
Platform: darwin/arm64
Podman: connected
Caddy: running
Apps: 5 (running: 3, stopped: 2)
```

#### status

Show detailed status of server and apps.

```bash
bp status
```

#### config

View or update server configuration.

```bash
bp config                  # Show current config
bp config set <key>=<val>  # Update config
```

**Examples:**
```bash
bp config
bp config set domain.root=example.com
bp config set domain.email=admin@example.com
```

#### prune

Clean up unused containers, images, and volumes.

```bash
bp prune
```

**Output:**
```
Removing unused containers... 3 removed
Removing unused images... 5 removed (1.2GB freed)
Removing unused volumes... 2 removed (500MB freed)

Total space freed: 1.7GB
```

**Flags:**
```bash
bp prune --all        # Include tagged images
bp prune --volumes    # Only prune volumes
bp prune --dry-run    # Show what would be removed
```

#### upgrade

Check for updates and upgrade Basepod.

```bash
bp upgrade
```

**Output:**
```
Current version: 1.0.5
Latest version: 1.0.6

Changelog:
  - Added static site support
  - Improved bp init detection
  - Bug fixes

Upgrade now? (y/N)
```

---

## Workflows

### Deploy a Static Site

```bash
# Build your site
npm run build

# Initialize (detects static site)
bp init
# Select: Static site
# Public dir: dist/

# Commit and deploy
git add . && git commit -m "Build"
bp push
```

### Deploy a Node.js App

```bash
# Initialize (detects package.json)
bp init
# Select: Container
# Runtime: Bun
# Port: 3000

# Commit and deploy
git add . && git commit -m "Initial deploy"
bp push
```

### Deploy a Go API

```bash
# Initialize (detects go.mod)
bp init
# Select: Container
# Port: 8080

# Commit and deploy
git add . && git commit -m "Initial deploy"
bp push
```

### Deploy One-Click Database

```bash
# Deploy PostgreSQL
bp template deploy postgres -e POSTGRES_PASSWORD=secret

# Check it's running
bp apps

# Get connection string
bp logs postgres
```

### Run Local LLM

```bash
# Download a model
bp model pull Llama-3.2-3B

# Start the server
bp model run Llama-3.2-3B

# Chat interactively
bp chat

# Or use the API
curl https://llm.example.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "default", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Multi-Environment Deployment

```bash
# Login to multiple servers
bp login bp.staging.example.com
bp login bp.prod.example.com

# List contexts
bp context

# Deploy to staging
bp context bp.staging.example.com
bp push

# Deploy to production
bp context bp.prod.example.com
bp push
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Configuration error |
| 3 | Git error (uncommitted changes) |

---

## Troubleshooting

### "No basepod.yaml found"

Run `bp init` to create the configuration file.

### "Uncommitted changes detected"

Commit your changes before deploying:
```bash
git add .
git commit -m "Your message"
bp push
```

Or force deploy (not recommended):
```bash
bp push --force
```

### "Not logged in"

Login to your server:
```bash
bp login bp.example.com
```

### "Connection refused"

Check that the Basepod server is running and accessible:
```bash
curl https://bp.example.com/api/health
```

### "Build failed"

Check the build output for errors. Common issues:
- Missing dependencies in Dockerfile
- Wrong port configuration
- Build context missing files (check .dockerignore)