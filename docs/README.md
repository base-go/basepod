# Basepod Documentation

Basepod turns your Mac Mini or VPS into a personal PaaS. Deploy apps, host websites, and run local LLMs.

---

## Installation

- [Server Installation](server/setup.md) - Install Basepod on your server
- [CLI Installation](cli/installation.md) - Install `bp` on your local machine

---

## Server

- [Setup Guide](server/setup.md) - Complete server setup walkthrough
- [Configuration](server/configuration.md) - Server configuration options
- [One-Click Templates](server/templates.md) - Deploy 50+ pre-configured apps
- [Local LLMs](server/llm.md) - Run AI models on Apple Silicon

---

## Client (bp CLI)

- [Quick Start](cli/quickstart.md) - Deploy your first app in 5 minutes
- [Command Reference](cli/reference.md) - Complete command documentation

---

## Architecture

```
┌─────────────┐     ┌─────────────────────────────────┐
│   bp CLI    │────▶│         Basepod Server          │
└─────────────┘     │  ┌─────────┐    ┌───────────┐   │
                    │  │   API   │    │  Podman   │   │
                    │  └────┬────┘    └─────┬─────┘   │
                    │       │               │         │
                    │  ┌────▼────┐    ┌─────▼─────┐   │
                    │  │  Caddy  │    │ Containers│   │
                    │  │ (Proxy) │    │   (Apps)  │   │
                    │  └─────────┘    └───────────┘   │
                    └─────────────────────────────────┘
```

## Domain Structure

| Subdomain | Purpose |
|-----------|---------|
| `bp.example.com` | Basepod dashboard |
| `llm.example.com` | LLM API endpoint |
| `*.example.com` | Your apps |

## Deployment Types

| Type | Use Case | Container |
|------|----------|-----------|
| Static | HTML/CSS/JS sites | No (Caddy serves) |
| Container | Node, Go, Python, etc. | Yes |
| Template | One-click apps | Yes |
| LLM | AI models (Apple Silicon) | No (native MLX) |
