# Findings

## Goal

Run Deployer reliably on a macOS Mac mini (Apple Silicon) as a self-hosted PaaS (CapRover-like), using Podman instead of Docker, and optionally host local LLMs.

## High-level architecture

- **deployerd** (Go) provides:
  - REST API (`/api/*`)
  - App lifecycle management (create/deploy/start/stop/logs)
  - A built-in reverse proxy for app domains via `ServeHTTP` (host-based routing)
  - Static web UI serving (disk-first, embedded fallback)
- **Podman** runs containers.
  - On macOS: Podman runs in a VM (podman-machine), so container IP routing is unreliable from host.
- **Caddy** provides:
  - TLS termination (optionally on-demand TLS)
  - Hostname routing to `deployerd` and app backends

## What already works for macOS

- **Podman machine auto-start (best-effort)**
  - `cmd/deployerd/main.go` checks podman machine status on macOS and runs `podman machine start` when needed.
- **Podman socket detection on macOS**
  - `internal/config/config.go:GetPodmanSocket()` uses `podman machine inspect` and fallbacks.
  - `install.sh` includes a symlink strategy for the podman socket (macOS socket lives under `/var/folders/...`).
- **App routing uses host ports on macOS**
  - Deployment code prefers `localhost:<hostPort>` for Caddy upstreams.
  - This is correct for macOS because container IP routing from host is not reliable with Podman VM.

## Local LLM support (MLX)

The repo already includes an MLX-based local LLM "app type".

- **App type:** `internal/app/app.go` defines `AppTypeMLX`.
- **API:** `internal/api/api.go`
  - `GET /api/mlx/models`
  - `GET /api/mlx/status`
  - Create/start/stop/delete flows branch on `app.Type == AppTypeMLX`.
- **Implementation:** `internal/mlx/mlx.go`
  - Creates a per-app Python venv
  - Installs `mlx-lm`
  - Downloads the model into an app-local cache
  - Runs `python -m mlx_lm.server --host 127.0.0.1 --port <port>`
- **Routing:** MLX apps are exposed the same way as container apps:
  - Caddy route points to `localhost:<port>`

## Notable gaps / risks for macmini "production"

### 1) MLX filesystem base directory permissions

- `mlx.NewManager("")` defaults to `/usr/local/deployer/mlx`.
- If `deployerd` runs as a non-root user (typical), writing to `/usr/local/deployer` may fail.
- Recommendation: move MLX base dir under the existing deployer home structure (e.g. `~/deployer/mlx` or `~/deployer/data/mlx`).

### 2) MLX process persistence across deployerd restarts

- Running MLX processes are tracked in-memory in `mlx.Manager.processes`.
- App record stores `a.MLX.PID`, but there is no startup reconciliation logic.
- After a `deployerd` restart, MLX apps may still be running but not tracked.

### 3) Caddy route removal consistency

There appears to be inconsistency between:
- How routes are created (ID patterns such as `deployer-<appname>`)
- How routes are removed (sometimes using app ID or domain)

Recommendation: standardize route IDs and always remove by the same identifier used for creation.

### 4) Source deploy shells out to `podman build`

- `handleSourceDeploy` uses `sh -c "cd ... && podman build ..."`.
- Works, but is environment-dependent and harder to sandbox.

## Recommended macmini deployment approach

- Run `deployerd` bound to localhost (`127.0.0.1:3000`).
- Run **Caddy** as the public entrypoint (ports `:80` and `:443`).
- Use wildcard domains for apps when possible.
- For LAN-only installs, consider internal DNS and either:
  - no public ACME, or
  - internal CA / trusted cert strategy.

## Notes

- This repo contains multiple prebuilt binaries and `web/node_modules`, which can significantly increase repo size and build/pull time.
