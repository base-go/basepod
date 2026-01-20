# CLI Reference

`bp` is the command-line interface for Basepod.

## Installation

```bash
# Install CLI only (local machine)
curl -fsSL https://pod.base.al/cli | bash

# Or build from source
go build -o bp ./cmd/bp
```

## Configuration

The CLI stores its configuration in `~/.basepod.yaml`:

```yaml
current_context: d.example.com
servers:
  d.example.com:
    url: https://d.example.com
    token: "your-auth-token"
```

## Commands

### login

Connect to a Basepod server.

```bash
bp login <server>
```

**Examples:**
```bash
bp login https://d.example.com
bp login d.example.com  # https:// added automatically
```

### logout

Disconnect from a server.

```bash
bp logout [name]
```

### context

List or switch server contexts.

```bash
bp context        # List all contexts
bp context <name> # Switch to named context
```

### apps

List all applications.

```bash
bp apps
```

**Output:**
```
NAME        STATUS    DOMAIN                    IMAGE
myapp       running   myapp.example.com        nginx:latest
api         stopped   api.example.com          node:20
```

### create

Create a new application.

```bash
bp create <name> [flags]
```

**Flags:**
- `--domain, -d` - Custom domain for the app
- `--port, -p` - Container port (default: 8080)
- `--image, -i` - Docker image to use

**Examples:**
```bash
bp create myapp
bp create myapp --domain myapp.example.com
bp create myapp --domain myapp.example.com --port 3000
bp create myapp --image nginx:latest
```

### init

Initialize a `basepod.yaml` configuration file in the current directory.

```bash
bp init
```

This creates a `basepod.yaml` file with default settings based on the current directory name.

### push

Deploy an application from local source code.

```bash
bp push [path]
```

This command:
1. Reads the `basepod.yaml` configuration
2. Creates a tarball of your source code
3. Uploads it to the server
4. Builds and deploys the container

**Examples:**
```bash
bp push              # Deploy current directory
bp push ./myapp      # Deploy specific path
```

### deploy

Deploy an application using a Docker image or Git repository.

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

### logs

View application logs.

```bash
bp logs <name> [flags]
```

**Flags:**
- `--tail, -n` - Number of lines to show (default: 100)

**Examples:**
```bash
bp logs myapp
bp logs myapp --tail 50
bp logs myapp -n 200
```

### start

Start a stopped application.

```bash
bp start <name>
```

### stop

Stop a running application.

```bash
bp stop <name>
```

### restart

Restart an application.

```bash
bp restart <name>
```

### delete

Delete an application.

```bash
bp delete <name>
```

**Aliases:** `rm`

**Example:**
```bash
bp delete myapp
bp rm myapp
```

### info

Show server information.

```bash
bp info
```

**Output:**
```
Server Info:
  version: 1.0.0
  os: darwin
  arch: arm64
  podman_status: connected
  caddy_status: running
```

### status

Show detailed server and app status.

```bash
bp status
```

**Output:**
```
Context: d.example.com
Server: https://d.example.com

System:
  Version: 1.0.0
  Platform: darwin/arm64
  Podman: connected
  Caddy: running

Apps:
  Total: 5 (running: 3, stopped: 2)
```

### version

Show CLI version.

```bash
bp version
bp -v
bp --version
```

### help

Show help information.

```bash
bp help
bp -h
bp --help
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `BASEPOD_SERVER` | Default server URL |
| `BASEPOD_TOKEN` | Authentication token |

## basepod.yaml Configuration

The `basepod.yaml` file configures your application for deployment:

```yaml
name: myapp
server: d.example.com  # Optional: target server context
domain: myapp.example.com  # Optional: custom domain
port: 3000  # Container port
build:
  dockerfile: Dockerfile
  context: .
env:
  NODE_ENV: production
  DATABASE_URL: postgres://...
volumes:
  - /data
```

## Examples

### Complete Workflow

```bash
# 1. Login to server
bp login d.example.com

# 2. Create an app
bp create myapp --domain myapp.example.com

# 3. Deploy
bp deploy myapp --image nginx:latest

# 4. Check status
bp status

# 5. View logs
bp logs myapp

# 6. Restart if needed
bp restart myapp
```

### Deploy from Source

```bash
# Initialize configuration
cd my-project
bp init

# Edit basepod.yaml as needed

# Deploy
bp push
```

### Deploy a Node.js App

```bash
# Create app with Node.js port
bp create api --domain api.example.com --port 3000

# Deploy custom image
bp deploy api --image ghcr.io/myuser/myapi:latest

# Check it's running
bp apps
```

### Multi-Server Deployment

```bash
# Login to multiple servers
bp login d.production.com
bp login d.staging.com

# List contexts
bp context

# Switch to production
bp context d.production.com

# Deploy
bp push
```
