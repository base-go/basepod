# CLI Quick Start

Deploy your first app in 5 minutes.

## Prerequisites

- [bp CLI installed](installation.md)
- Basepod server running (see [Server Setup](../server/setup.md))

## 1. Login to Your Server

```bash
bp login bp.example.com
```

Enter your password when prompted.

## 2. Initialize Your Project

Navigate to your project and run:

```bash
cd myproject
bp init
```

The wizard detects your project type and creates `basepod.yaml`:

```
$ bp init

Detected: package.json (Node/Bun project)

? App name: (myproject)
? Deployment type:
  > Container (auto-generate Dockerfile)
    Static site

? Runtime:
  > Bun (recommended)
    Node.js

? Start command: (bun run start)
? Port: (3000)
? Domain: (myproject.example.com)

Created:
  - basepod.yaml
  - Dockerfile (generated)

Next: bp push
```

## 3. Deploy

Commit your changes and push:

```bash
git add .
git commit -m "Add basepod config"
bp push
```

Your app is now live at `https://myproject.example.com`

## What's Next?

### Deploy a Static Site

```bash
# Build your site
npm run build

# Initialize as static
bp init
# Select: Static site
# Public dir: dist/

# Deploy
git add . && git commit -m "Deploy"
bp push
```

### Deploy a One-Click App

```bash
# List templates
bp templates

# Deploy PostgreSQL
bp template deploy postgres -e POSTGRES_PASSWORD=secret

# Deploy Redis
bp template deploy redis
```

### Run a Local LLM

```bash
# Download a model
bp model pull Llama-3.2-3B

# Start the server
bp model run Llama-3.2-3B

# Chat
bp chat
```

### Manage Multiple Servers

```bash
# Add servers
bp login bp.staging.example.com
bp login bp.prod.example.com

# List contexts
bp context

# Switch context
bp context bp.prod.example.com

# Deploy
bp push
```

## Common Commands

| Command | Description |
|---------|-------------|
| `bp apps` | List all apps |
| `bp logs <app>` | View app logs |
| `bp restart <app>` | Restart app |
| `bp stop <app>` | Stop app |
| `bp delete <app>` | Delete app |
| `bp templates` | List one-click templates |
| `bp models` | List LLM models |

## Troubleshooting

### "Uncommitted changes"

```bash
git add .
git commit -m "Your message"
bp push
```

### "No basepod.yaml found"

```bash
bp init
```

### "Not logged in"

```bash
bp login bp.example.com
```

## Full Reference

See [CLI Reference](reference.md) for all commands and options.
