# Deployer Roadmap

## Completed

### Core Features
- [x] Single-container app deployment via Podman
- [x] Automatic SSL certificates via Caddy (on-demand TLS)
- [x] Wildcard subdomain routing (*.example.com)
- [x] Web UI dashboard with app management
- [x] App lifecycle management (start, stop, restart, delete)
- [x] Container logs viewing
- [x] Environment variable configuration
- [x] Volume persistence for data
- [x] Port mapping and exposure

### One-Click Templates
- [x] Databases: MySQL, MariaDB, PostgreSQL, MongoDB, Redis
- [x] Admin Tools: phpMyAdmin, Adminer, pgAdmin
- [x] CMS: WordPress, Ghost, Strapi, Nextcloud, Directus, Drupal, MediaWiki, BookStack, PocketBase
- [x] Dev Tools: Gitea, Portainer, Uptime Kuma, Code Server
- [x] Communication: Mattermost, Rocket.Chat
- [x] Automation: n8n
- [x] Analytics: Plausible, Grafana
- [x] Storage: MinIO, File Browser
- [x] Business: NocoDB, Listmonk, Chatwoot, Invoice Ninja, Cal.com
- [x] AI/ML: Ollama, Open WebUI, Flowise

### Image Management
- [x] Docker Hub tag fetching with caching
- [x] Version selector in deploy modal
- [x] Alpine variant support detection
- [x] Architecture-aware templates (ARM64/AMD64)

### System
- [x] Auto-update mechanism
- [x] SQLite storage backend
- [x] CLI tool (deployer)
- [x] Authentication system
- [x] Settings management

---

## In Progress

### v0.2.x - Enhanced Deployment
- [ ] Custom domain support per app (not just subdomains)
- [ ] Health checks and auto-restart
- [ ] Resource limits (CPU/Memory) via UI
- [ ] Backup/restore functionality

---

## Planned

### v0.3.x - Multi-Container Support
- [ ] **Podman Compose / Stack support**
  - Deploy docker-compose.yml files
  - Manage multi-container applications as a unit
  - Support for Colanode, Appwrite, Supabase, etc.
- [ ] Service linking UI (visual connection between containers)
- [ ] Shared networks between containers
- [ ] Dependency ordering (start DB before app)

### v0.4.x - Advanced Features
- [ ] Git-based deployments (deploy from repo)
- [ ] Dockerfile builds
- [ ] Deployment webhooks
- [ ] Rolling updates / zero-downtime deploys
- [ ] Cron job / scheduled task support

### v0.5.x - Monitoring & Observability
- [ ] Built-in metrics dashboard
- [ ] Container resource usage graphs
- [ ] Alert notifications (email, webhook, Slack)
- [ ] Log aggregation and search
- [ ] Uptime monitoring integration

### v0.6.x - Multi-Server
- [ ] Agent mode for remote servers
- [ ] Central management of multiple deployer instances
- [ ] Load balancing between instances
- [ ] Distributed deployments

### Future Ideas
- [ ] Kubernetes export (generate K8s manifests)
- [ ] Terraform provider
- [ ] API for external integrations
- [ ] Plugin system for custom templates
- [ ] Team/user management with RBAC
- [ ] Audit logging
- [ ] Secret management (Vault integration)
- [ ] Container registry management
- [ ] Image vulnerability scanning

---

## Template Wishlist

### To Add
- [ ] Colanode (requires compose support)
- [ ] Appwrite (requires compose support)
- [ ] Supabase (requires compose support)
- [ ] Outline (requires Postgres + Redis)
- [ ] Metabase
- [ ] Paperless-ngx
- [ ] Immich (photo management)
- [ ] Vaultwarden (Bitwarden)
- [ ] Authentik (SSO)
- [ ] Keycloak
- [ ] Umami Analytics
- [ ] Docmost
- [ ] Typebot
- [ ] Dify (AI platform)
- [ ] LocalAI

---

## Contributing

Want to contribute? Pick an item from the roadmap and open a PR!

For template requests, open an issue with:
- App name and Docker image
- Required environment variables
- Port and volume configuration
- Any special requirements (needs DB, ARM support, etc.)
