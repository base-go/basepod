# CLI Reference

`deployerctl` is the command-line interface for Deployer.

## Installation

```bash
# Install CLI only (local machine)
curl -sSL https://raw.githubusercontent.com/deployer/deployer/main/scripts/install-cli.sh | bash

# Or build from source
go build -o deployerctl ./cmd/deployerctl
```

## Configuration

The CLI stores its configuration in `~/.deployerctl.yaml`:

```yaml
server: https://deployer.example.com
token: ""  # Optional auth token
```

## Commands

### login

Connect to a Deployer server.

```bash
deployerctl login <server>
```

**Examples:**
```bash
deployerctl login https://deployer.example.com
deployerctl login deployer.example.com  # https:// added automatically
```

### apps

List all applications.

```bash
deployerctl apps
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
deployerctl create <name> [flags]
```

**Flags:**
- `--domain, -d` - Custom domain for the app
- `--port, -p` - Container port (default: 8080)
- `--image, -i` - Docker image to use

**Examples:**
```bash
deployerctl create myapp
deployerctl create myapp --domain myapp.example.com
deployerctl create myapp --domain myapp.example.com --port 3000
deployerctl create myapp --image nginx:latest
```

### deploy

Deploy an application.

```bash
deployerctl deploy <name> [flags]
```

**Flags:**
- `--image, -i` - Docker image to deploy
- `--git, -g` - Git repository URL
- `--branch, -b` - Git branch (default: main)

**Examples:**
```bash
deployerctl deploy myapp --image nginx:latest
deployerctl deploy myapp --image ghcr.io/user/myapp:v1.0
deployerctl deploy myapp --git https://github.com/user/repo.git
deployerctl deploy myapp --git https://github.com/user/repo.git --branch develop
```

### logs

View application logs.

```bash
deployerctl logs <name> [flags]
```

**Flags:**
- `--tail, -n` - Number of lines to show (default: 100)

**Examples:**
```bash
deployerctl logs myapp
deployerctl logs myapp --tail 50
deployerctl logs myapp -n 200
```

### start

Start a stopped application.

```bash
deployerctl start <name>
```

### stop

Stop a running application.

```bash
deployerctl stop <name>
```

### restart

Restart an application.

```bash
deployerctl restart <name>
```

### delete

Delete an application.

```bash
deployerctl delete <name>
```

**Aliases:** `rm`

**Example:**
```bash
deployerctl delete myapp
deployerctl rm myapp
```

### info

Show server information.

```bash
deployerctl info
```

**Output:**
```
Server Info:
  version: 0.1.0
  status: running
  containers: 5
  images: 12
```

### version

Show CLI version.

```bash
deployerctl version
deployerctl -v
deployerctl --version
```

### help

Show help information.

```bash
deployerctl help
deployerctl -h
deployerctl --help
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `DEPLOYER_SERVER` | Default server URL |
| `DEPLOYER_TOKEN` | Authentication token |

## Examples

### Complete Workflow

```bash
# 1. Login to server
deployerctl login https://deployer.example.com

# 2. Create an app
deployerctl create myapp --domain myapp.example.com

# 3. Deploy
deployerctl deploy myapp --image nginx:latest

# 4. Check status
deployerctl apps

# 5. View logs
deployerctl logs myapp

# 6. Restart if needed
deployerctl restart myapp
```

### Deploy a Node.js App

```bash
# Create app with Node.js port
deployerctl create api --domain api.example.com --port 3000

# Deploy custom image
deployerctl deploy api --image ghcr.io/myuser/myapi:latest

# Check it's running
deployerctl apps
```

### Quick Nginx Setup

```bash
deployerctl create web --domain www.example.com
deployerctl deploy web --image nginx:alpine
```
