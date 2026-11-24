# AGENTS.md

Guidance for AI agents working in this repository.

## Documents

- `docs/DESIGN.md` — product and behavior design. The source of truth for rules.
- `docs/TECH_STACK.md` — technology choices and configuration.
- `docs/DEPLOYMENT_MCP.md` — homelab configuration for agent access (MCP).
- `docs/API.md` — wire types, the operation protocol and the MCP tool schemas.
  Go (`internal/basket/types.go`) and TypeScript (`web/src/types.ts`) must
  match it exactly.

## Layout

| Path | Contents |
| --- | --- |
| `main.go` | Entry point: config, database, auth, services, HTTP server, backups |
| `internal/config` | Environment configuration |
| `internal/store` | SQLite schema, migrations, items, purchases, change log, applied ops, sessions |
| `internal/basket` | Service layer: validation, similar-title check, recurrence, derived auto-archive, per-actor undo |
| `internal/auth` | OIDC sign-in, sessions, bearer verification |
| `internal/mcp` | MCP tools over `internal/basket`, mounted at `/mcp` |
| `internal/server` | HTTP routing, `/api` handlers, SPA serving |
| `internal/backup` | Periodic `VACUUM INTO` database copies |
| `web/src` | React app, IndexedDB snapshot and op queue |
| `web/src/data` | IndexedDB, operation queue, snapshot handling, sign-out, export |
| `web/src/ui` | React screens |
| `web/public/sw.js` | Service worker (app shell only) |
| `e2e/` | Playwright tests and runner |
| `e2e/mockidp` | Mock OIDC provider used by Playwright |

## Commands

```sh
make web        # build the frontend into web/dist (embedded by the binary)
make test       # Go tests (including -race) and web tests
make lint       # golangci-lint
make test/e2e   # Playwright against the built server and mock OIDC provider
npm --prefix web test        # web unit tests (node --test, TypeScript via type stripping)
npm --prefix web run typecheck
```

TypeScript test files import sources with explicit `.ts` extensions and use
only erasable syntax (no enums, namespaces or parameter properties), so Node
runs them directly.

## Rules

- Amounts are integer kuruş everywhere. Dates are `YYYY-MM-DD` strings; never
  use JavaScript `Date` objects or Go `time.Time` for calendar dates.
- The server is the only place rules run; the client queues intent operations
  and replaces its snapshot after each send.
