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
