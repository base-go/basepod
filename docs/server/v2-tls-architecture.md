# Basepod v2 — TLS Architecture

Status: Proposed
Target: v2.0
Owner: TBD

## Problem

Basepod today issues TLS certificates via Caddy's **on-demand TLS** with an
HTTP-01 challenge gate (`EnsureBaseConfig` in `internal/caddy/caddy.go:478`).
When the first TLS handshake for a host arrives, Caddy asks basepod's
`/api/caddy/check` endpoint whether it should issue, and if yes, starts an
ACME HTTP-01 flow.

This **silently fails** for any custom domain sitting behind a reverse proxy
(Cloudflare, CloudFront, Vercel, ingress controllers, corporate WAFs). The
sequence:

1. Proxy terminates TLS at its edge, opens its own TLS connection to origin.
2. Caddy sees SNI for the custom host, asks `/api/caddy/check` → OK.
3. Caddy triggers ACME HTTP-01 → LE fetches
   `http://<host>/.well-known/acme-challenge/...`.
4. That HTTP request routes through the proxy again, not to basepod.
5. Proxy can't answer the challenge → LE issuance fails.
6. TLS handshake fails → proxy returns 502 to the user.

We hit this with `construct.delivery` (Cloudflare-proxied) while
`web.construct.delivery` (DNS-only) works fine. The failure mode is hostile:
no user-facing signal, no deploy-time check, and no obvious fix in the
dashboard.

A secondary finding: `caddy.Route.EnableSSL` (`internal/caddy/caddy.go:24`) is
declared on every route but never consumed in `AddRoute`. Dead config that
suggests per-route TLS control exists, which it doesn't.

## Goal

Basepod should issue valid certificates for every declared host, independent
of what sits in front of basepod. "Put Cloudflare in front" should not break
HTTPS.

Secondary goals:

- Declarative TLS config per app — yaml-driven, reviewable in PRs.
- Eager issuance at deploy time, not at first-handshake time.
- Wildcard cert for basepod's default suffix so auto-provisioned subdomains
  come up with HTTPS without a per-app ACME round trip.

## Design

### 1. DNS-01 as a first-class challenge type

DNS-01 is the right default for anything behind a proxy: the challenge
happens at the DNS layer, invisible to HTTP middleboxes. Also unlocks
wildcards, which HTTP-01 cannot issue.

Basepod will support three challenge types, in priority order:

| Type         | When it wins                                                  |
|--------------|---------------------------------------------------------------|
| DNS-01       | Domain behind any proxy (Cloudflare, CDN, WAF, ingress).      |
| TLS-ALPN-01  | Direct-to-origin, no port 80 (only 443 open).                 |
| HTTP-01      | Direct-to-origin, port 80 reachable. Current default.         |

The challenge type is chosen per-app (or inherited from server default),
declared explicitly. No silent fallback — if DNS-01 is requested and the
provider token is missing, deploy fails with a clear error.

### 2. Wildcard for the default suffix

Basepod manages DNS for its own suffix (configured as `Domain.Root` /
`Domain.Suffix` in `internal/config/config.go`). Issue one wildcard cert —
`*.<suffix>` — at server bootstrap via DNS-01 using the basepod operator's
DNS credentials. Every auto-generated `<app>.<suffix>` URL then serves
HTTPS immediately with no per-app ACME call.

Wildcards don't cover the apex itself, so a second cert `<suffix>` is also
issued in the same flow.

### 3. Custom-domain aliases: DNS-01 with per-alias credentials

When a user adds a custom alias (`construct.delivery`, `blog.example.com`),
basepod does not own that DNS. The user registers a DNS provider + API
credentials against the alias:

```yaml
tls:
  challenge: dns-01
  provider: cloudflare
  credentials: $CF_API_TOKEN   # env var reference, never inlined in yaml
```

The credential is stored encrypted (use the existing env-var encryption path
v2 is introducing). Caddy's automation policy for that specific subject uses
the right issuer.

### 4. Eager issuance at deploy time

On `bp deploy` (and on first creation of an app with custom aliases),
basepod drives the ACME flow itself and blocks the deploy until a cert is
in Caddy's storage. If issuance fails, the deploy fails with the LE error
message surfaced directly — not a silent 502 three hours later.

On-demand TLS is still supported, but only as an explicit opt-in (e.g. for
true multi-tenant SaaS that doesn't know the domain set at deploy time).
It is no longer the default.

### 5. Explicit per-app TLS config in `basepod.yaml`

Extend `AppConfig` (`cmd/bp/main.go:825`):

```yaml
name: web
type: static
public: dist
domain: web.construct.delivery
aliases:
  - construct.delivery
tls:
  # One of: auto | dns-01 | tls-alpn-01 | http-01 | upload | none
  # 'auto' picks: DNS-01 if provider configured, else HTTP-01 (legacy path).
  strategy: dns-01
  provider: cloudflare
  credentials: $CF_API_TOKEN
  # Optional: upload a pre-issued cert (CF Origin Cert, corporate CA, etc.)
  cert_file: ./origin.crt
  key_file:  ./origin.key
```

Defaults:

- No `tls:` block → v1 behavior (on-demand HTTP-01) for backwards compat.
- Must be explicitly opted into the new pipeline via `tls.strategy`.

v3 can flip the default once all existing apps have migrated.

### 6. Dead-code cleanup

Remove `Route.EnableSSL` and `Route.ForceHTTPS` from
`internal/caddy/caddy.go:24–25` — neither is consumed. If per-route policy
becomes a real feature, re-introduce with a clear contract.

### 7. Dashboard surface

On the app Settings page (alongside Domain Aliases), show:

- Cert source: "Let's Encrypt (DNS-01 via Cloudflare)" / "Uploaded" / "On-demand"
- Issued: 2026-03-15 · Expires: 2026-06-13 · Auto-renews
- Last issuance attempt + error if failed
- "Rotate now" button

## Implementation sequence

Split into four PRs, ship incrementally.

### PR 1 — Custom Caddy build with DNS providers

- Move from stock Caddy to a Caddy built with
  `xcaddy build` + `github.com/caddy-dns/cloudflare`,
  `caddy-dns/route53`, `caddy-dns/digitalocean`, `caddy-dns/gandi`,
  `caddy-dns/godaddy`, `caddy-dns/cloudns` (pick the initial set).
- Bake into basepod's install script / Docker image.
- No behavior change yet — plugins just available.

### PR 2 — Schema + storage

- Extend `AppConfig` yaml schema with `tls:` block (`cmd/bp/main.go`).
- Add `tls_config` column on `apps` table (encrypted JSON).
- Extend `CreateAppRequest` / `UpdateAppRequest` in `internal/app/app.go`.
- Kill `Route.EnableSSL` / `Route.ForceHTTPS`.

### PR 3 — Eager issuance pipeline

- Add `internal/tls/` package with:
  - `type Strategy interface { Issue(ctx, domains) error; Renew(ctx, domains) error }`
  - `DNSChallengeStrategy` (per-provider),
    `HTTPChallengeStrategy` (current path),
    `UploadedCertStrategy` (read from disk).
- Call `Strategy.Issue` synchronously from `handleDeployApp`
  before returning success. Surface LE error verbatim on failure.
- Configure Caddy `tls.automation.policies[]` per subject with the right
  issuer + challenge config (not on-demand).

### PR 4 — Wildcard + dashboard UX

- At server bootstrap, issue `*.<suffix>` and `<suffix>` via DNS-01.
- Dashboard panel on app Settings with cert source / expiry / rotate button.
- CLI `bp cert status <app>`, `bp cert rotate <app>`.

## Migration

- v1.x apps with no `tls:` block keep the existing on-demand HTTP-01 flow.
  Zero behavior change for direct-to-origin deployments.
- Users behind Cloudflare/CDN must add a `tls:` block to opt into DNS-01.
  Migration guide in `docs/server/custom-domains-behind-proxy.md` with
  concrete Cloudflare + Route53 examples.
- v2 dashboard surfaces a warning on apps with aliases lacking a `tls:`
  block: "This alias may fail TLS issuance if the domain is proxied."

## Related cleanup (ships with v2)

### Remove the "Restart Caddy" dashboard action

Currently exposed at `POST /api/system/restart/{service}` and wired to a
button on the dashboard. It shells out to `systemctl restart caddy` (Linux)
or `launchctl` (macOS). The basepod daemon runs as user `basepod` without
sudoers for systemctl, so the call exits 1 and the dashboard surfaces
`{"error":"Failed to restart Caddy: exit status 1"}`. The error is
swallowed stderr — no signal about what went wrong.

Users reach for this button when TLS misbehaves. Once v2's DNS-01 pipeline
lands, there is nothing a real restart fixes that Caddy's admin-API reload
(`POST http://localhost:2019/load`) doesn't already handle — and the admin
API call needs no extra privileges, because basepod already speaks to it
for every route mutation.

Plan:

- Remove the `case "caddy":` branch from `handleServiceRestart`
  (`internal/api/api.go:3169`).
- Remove the dashboard action.
- Keep `case "basepod":` and `case "podman":` (they serve distinct
  purposes).
- If a future operator genuinely needs a hot reload, expose
  `POST /api/system/reload/caddy` that calls the admin API directly. No
  systemctl, no sudoers, no swallowed stderr.

Also drop dead fields while we're in there:

- `caddy.Route.EnableSSL` and `caddy.Route.ForceHTTPS`
  (`internal/caddy/caddy.go:24-25`) — declared on every route, never
  consumed in `AddRoute`. Per-route TLS control belongs in
  `tls.automation.policies[]` per the design above, not a boolean on the
  route struct.

## Non-goals for v2

- Becoming a general-purpose ACME client. Only Let's Encrypt + ZeroSSL;
  others can come later.
- Rich cert-management UI (bulk rotation, cert groups, SCT logs). v2 only
  needs per-app visibility; fleet-level tools are v3.

## Open questions

1. Where do DNS provider credentials live — per-app (flexible, secrets
   duplicated) or per-basepod-instance (one credential, all apps share)?
   Probably start per-app with a "server default" fallback.
2. Rate-limit handling: LE's production environment has issuance caps. For
   wildcard + apex + per-alias in an active install, we could trip them
   during load testing. Caddy handles this internally, but surfacing
   "rate-limited, retrying at 15:00 UTC" in the dashboard is user-critical.
3. DNS-01 propagation delay varies by provider (CF fast, some registrars
   minutes). Deploy blocks on issuance — what's the acceptable timeout
   (60s? 5min?) and how do we surface "still waiting for DNS" vs
   "permanent failure"?

## Risk the plan reduces

Today a user can deploy an app, add an alias, have it appear healthy in
the dashboard, and not find out for hours that HTTPS never worked for that
domain. v2 makes the failure loud, local to the deploy, and fixable from
yaml instead of from DNS console guesses.
