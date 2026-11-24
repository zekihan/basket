# Basket

A personal shopping list and wishlist for one owner. It is an offline-capable
PWA served by a single Go server, signs in with OIDC, and exposes the lists to
Claude through an MCP endpoint.

Basket only keeps the lists. Anything that reads them, such as a scheduled AI
job looking for deals, works through the same MCP tools as any other client.

See [docs/DESIGN.md](docs/DESIGN.md) for the design,
[docs/TECH_STACK.md](docs/TECH_STACK.md) for the stack and configuration, and
[docs/DEPLOYMENT_MCP.md](docs/DEPLOYMENT_MCP.md) for agent access.

Status: design.

## Run locally

```sh
make dev    # build the frontend and run the server on :8080
```

Copy the environment from the Configuration table below. For local HTTP
development without TLS, set `COOKIE_INSECURE=true`. The container image sets
`DB_PATH=/data/basket.db` and `BACKUP_DIR=/data/backups`.

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

## Development

```sh
make web        # build the frontend
make dev        # build the frontend and run the server
make test       # Go and web tests
make lint       # golangci-lint
make test/e2e   # Playwright against the built server and mock OIDC provider
```
