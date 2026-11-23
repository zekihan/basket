# Basket: Tech Stack

Status — draft

Basket follows `family-tab` and `sdash`: a Go server with an embedded React PWA,
OIDC sign-in and an MCP endpoint, deployed to the homelab with Argo CD.

| Area | Choice |
| --- | --- |
| Server | Go, standard library `net/http`, single binary with the built frontend embedded |
| Shared code | `go-commons` for logging, panic recovery, health and metrics middleware |
| Storage | SQLite via `modernc.org/sqlite` and `sqlx`, WAL, `PRAGMA user_version` migrations, one writer connection |
| Sign-in | `coreos/go-oidc/v3`, `golang.org/x/oauth2`; server-side sessions in SQLite |
| Agent access | `github.com/modelcontextprotocol/go-sdk`, streamable HTTP, Authelia RS256 JWT bearer tokens |
| Config | Environment variables via `sethvargo/go-envconfig` |
| Client | React, Vite, TypeScript, Tailwind CSS |
| Offline | Hand-written service worker (app shell only), IndexedDB via `idb` |
| Tests | `go test -race`, `node --test`, Playwright with `oauth2-proxy/mockoidc` |
| Release | goreleaser, `ghcr.io/zekihan/distroless`, Helm chart in `homelab/apps/basket` |

Compared with family-tab, Basket drops the two-language ledger rules, the
differential test and the revision-based sync protocol. The server is the only
place rules run; the client queues intent operations and replaces its snapshot
after each send.

## Layout

| Path | Contents |
| --- | --- |
| `main.go` | Config, database, auth, services, HTTP server, backups |
| `internal/config` | Environment configuration |
| `internal/store` | Schema, migrations, items, purchases, change log, applied ops, sessions |
| `internal/basket` | Service layer: validation, similar-title check, recurrence, undo |
| `internal/auth` | OIDC sign-in, sessions, bearer verification |
| `internal/mcp` | MCP tools over `internal/basket` |
| `internal/server` | Routing, `/api`, SPA serving |
| `internal/backup` | Periodic `VACUUM INTO` copies |
| `web/src` | React app, IndexedDB snapshot and op queue |
| `web/public/sw.js` | Service worker |

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `PUBLIC_URL` | required | External origin; OIDC callback is `<PUBLIC_URL>/auth/callback` |
| `OIDC_ISSUER` | required | Provider issuer URL |
| `OIDC_CLIENT_ID` | required | PWA client |
| `OIDC_CLIENT_SECRET` | empty | If the client is confidential |
| `OWNER_SUBJECT` | required | `sub` of the only identity allowed in |
| `MCP_CLIENT_ID` | empty | Client whose access tokens may call `/mcp`; MCP disabled when empty |
| `MCP_AUDIENCE` | `PUBLIC_URL` + `/mcp` | Required `aud` of MCP tokens |
| `TIME_ZONE` | `Europe/Istanbul` | Default for "today" and due dates |
| `LISTEN_ADDR` | `:8080` | HTTP listener |
| `METRICS_ADDR` | `:9090` | Internal `/metrics`; empty disables |
| `DB_PATH` | `./basket.db` | SQLite database |
| `BACKUP_DIR` | empty | `VACUUM INTO` target; disabled when empty |
| `BACKUP_INTERVAL` | `6h` | |
| `BACKUP_KEEP` | `28` | |
| `LOG_LEVEL` | `INFO` | |
| `COOKIE_INSECURE` | `false` | Local HTTP development only |

## HTTP API

| Route | Purpose |
| --- | --- |
| `GET /api/snapshot` | All items and purchases, plus `today` and time zone |
| `POST /api/ops` | Ordered batch of `{op_id, type, …}`; returns per-op result and a new snapshot |
| `GET /api/export` | JSON export |
| `/auth/login`, `/auth/callback`, `/auth/logout`, `/auth/me` | Sign-in |
| `/mcp`, `/.well-known/oauth-protected-resource` | Agent access |
| `/healthz` | Liveness and readiness |

Op types: `add_item`, `update_item`, `remove_item`, `mark_bought`,
`reopen_item`, `delete_purchase`.

## Deployment

- Image `ghcr.io/zekihan/basket`, pinned as `edge@sha256:…` in the chart.
- `homelab/apps/basket` cloned from `apps/family-tab`: one replica, `Recreate`,
  1Gi Longhorn PVC with hornkeeper backup annotations, restricted pod security,
  ingress from Traefik only, egress to DNS and Authelia only.
- Host `basket.noqer.com`: per-host certificate and DNS record, a
  `cloudflared` proxyDomains entry, and a Gatus check.
- Authelia clients `basket` (PWA) and `basket-mcp` (Claude); see
  [DEPLOYMENT_MCP.md](DEPLOYMENT_MCP.md).
