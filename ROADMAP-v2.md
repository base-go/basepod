# Basepod v2.0 Roadmap

## Overview
Major release focusing on developer experience, team collaboration, and operational maturity. Removes image generation (Flux) to keep the platform focused on app deployment and LLM serving.

---

## Phase 1: Cleanup & Foundation

### 1.1 Remove Flux/Image Generation
- [ ] Remove `internal/flux/` package
- [ ] Remove Flux API routes from `internal/api/api.go`
- [ ] Remove `flux_generations` and `flux_sessions` tables from storage schema
- [ ] Remove `web/app/pages/images.vue`
- [ ] Remove Flux references from navigation/sidebar
- [ ] Clean up any Flux-related config options
- **Goal:** Leaner codebase, focused on containers + LLM

### 1.2 App Environment Variables UI
- [ ] API: `GET/PUT /api/apps/{id}/env` endpoints
- [ ] Store env vars in database (encrypted at rest)
- [ ] Inject env vars into container on start/deploy
- [ ] Web UI: Env editor on app detail page (key/value pairs, bulk edit)
- [ ] CLI: `bp env list`, `bp env set KEY=VAL`, `bp env unset KEY`
- **Goal:** No more SSH to manage config

### 1.3 App Health Checks
- [ ] Configurable health endpoint per app (default: `/health`)
- [ ] Periodic health polling (configurable interval)
- [ ] Auto-restart on consecutive failures (configurable threshold)
- [ ] Health status indicator on dashboard (green/yellow/red)
- [ ] Health check history/timeline
- **Goal:** Self-healing apps

---

## Phase 2: Deploy Pipeline

### 2.1 Deploy Webhooks (GitHub Auto-Deploy)
- [ ] API: `POST /api/apps/{id}/webhook` — endpoint GitHub calls on push
- [ ] Webhook secret validation (HMAC signature)
- [ ] Branch filtering (only deploy from `main` or configured branch)
- [ ] Auto-trigger redeploy on matching push
- [ ] Webhook status/history in UI
- [ ] Setup instructions in app settings (copy webhook URL + secret)
- **Goal:** Push to GitHub, app deploys automatically

### 2.2 One-Click Rollback
- [ ] Store previous 5 container images per app (tagged by deploy timestamp)
- [ ] API: `POST /api/apps/{id}/rollback` with optional version parameter
- [ ] Deploy history list in app detail page
- [ ] One-click rollback button per deploy entry
- [ ] CLI: `bp rollback [app]` — rollback to previous version
- **Goal:** Instant recovery from bad deploys

### 2.3 Build Logs Streaming
- [ ] WebSocket endpoint for real-time build output
- [ ] Build log persistence (stored per deploy)
- [ ] Build log viewer in UI with ANSI color support
- [ ] Build status indicators (building/success/failed)
- **Goal:** Watch your deploy happen live

### 2.4 Custom Dockerfile Support
- [ ] Detect Dockerfile in repo root
- [ ] Build from Dockerfile via Podman
- [ ] Support `.basepod` config file in repo for build args, port, health check
- [ ] Buildpack fallback when no Dockerfile present
- **Goal:** Deploy any containerized app

---

## Phase 3: Operational Maturity

### 3.1 App Metrics & Monitoring
- [ ] Collect CPU/memory/network per container (via Podman stats API)
- [ ] Store metrics in time-series format (SQLite or separate file)
- [ ] Dashboard: sparkline charts per app (last 1h/24h/7d)
- [ ] App detail page: full metrics graphs
- [ ] API: `GET /api/apps/{id}/metrics`
- [ ] Alert thresholds (optional): notify when CPU/memory exceeds limit
- **Goal:** Know how your apps are performing

### 3.2 App Resource Limits
- [ ] Set CPU/memory caps per container
- [ ] Default limits in global config
- [ ] Per-app override in app settings
- [ ] Show current usage vs limit in dashboard
- **Goal:** Prevent runaway containers

### 3.3 Database Provisioning
- [ ] One-click Postgres, MySQL, Redis from templates
- [ ] Auto-generate credentials
- [ ] Auto-inject `DATABASE_URL` / connection string into linked app env vars
- [ ] Backup/restore for provisioned databases
- [ ] Database status and connection info in UI
- **Goal:** Full-stack deploy without leaving basepod

### 3.4 Cron Jobs
- [ ] API: `POST /api/apps/{id}/cron` — create scheduled task
- [ ] Cron expression support (with human-readable presets)
- [ ] Execute command inside running container
- [ ] Cron execution history with output logs
- [ ] Enable/disable individual cron entries
- **Goal:** Background jobs without external schedulers

---

## Phase 4: Team & DX

### 4.1 Multi-User / Team Access
- [ ] User accounts with email/password
- [ ] Roles: admin (full access), deployer (deploy + view), viewer (read-only)
- [ ] Invite link generation
- [ ] Per-app access control (who can deploy what)
- [ ] Audit log: who did what, when
- **Goal:** Team-friendly platform

### 4.2 Activity Log / Audit Trail
- [ ] Log all actions: deploys, config changes, restarts, rollbacks
- [ ] Store actor (user), action, target, timestamp
- [ ] Activity feed on dashboard
- [ ] Filter by app, user, action type
- [ ] API: `GET /api/activity`
- **Goal:** Full visibility into what happened

### 4.3 Notification Hooks
- [ ] Webhook notifications (generic HTTP POST)
- [ ] Slack integration (incoming webhook)
- [ ] Discord integration (webhook)
- [ ] Configurable events: deploy success/failure, health check failure, resource alerts
- [ ] Per-app and global notification settings
- **Goal:** Stay informed without watching the dashboard

### 4.4 AI Deploy Assistant
- [ ] Paste a GitHub repo URL
- [ ] Analyze repo structure using built-in LLM
- [ ] Auto-detect stack (Node, Go, Python, Ruby, etc.)
- [ ] Generate Dockerfile, suggest port, env vars
- [ ] One-click deploy from suggestion
- **Goal:** Zero-config deploys powered by LLM

### 4.5 CI/CD Integration
- [ ] GitHub Actions template for `bp deploy`
- [ ] Deploy tokens (scoped API keys for CI)
- [ ] CLI: `bp deploy --token` for headless deploys
- [ ] Status badge endpoint for README
- **Goal:** Fit into existing CI pipelines

---

## Release Plan

| Milestone | Phases | Target |
|-----------|--------|--------|
| v2.0-alpha | Phase 1 (cleanup + env vars + health checks) | -- |
| v2.0-beta | Phase 2 (deploy pipeline) | -- |
| v2.0-rc | Phase 3 (ops maturity) | -- |
| v2.0 | Phase 4 (team + DX) | -- |

Each phase can ship independently as features are completed. Priority order within phases is top-to-bottom.
