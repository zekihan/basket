# Basket: Design Doc

Status — draft

## 1. Problem

The owner keeps two kinds of lists: things to pick up soon (shampoo, a charger
cable) and things to buy when the price is right (a table, a 15 m Cat6 cable).
Today they live in notes and chat history. Basket is one place for both, usable
from a phone in a store and from Claude in conversation.

Deal hunting happens outside Basket. A scheduled AI job reads the list through
the same MCP tools as any other client and decides on its own what matches.
Basket has no notion of deals, schedules or matching.

## 2. Goals and non-goals

Goals:

- Add, change, check off and remove items from Claude (MCP) and from a PWA.
- Free-text requirements per item ("15m, Cat6 or higher") that any reader can
  interpret.
- Purchase log with optional price, and recurring items that come due again.
- Works in a store without signal: the PWA opens from cache and queues
  check-offs.
- Single owner, signed in through OIDC.

Non-goals:

- Deals, price tracking, scraping, matching, notifications or schedules.
- Sharing, multiple users, households.
- Structured product specs. Requirements stay free text in `notes`.
- Categories, stores or aisle ordering (later, if missed).

## 3. Use cases

| Said to Claude | Result |
| --- | --- |
| "add a 15m cat6 or higher cable" | Wishlist item *Ethernet cable*, notes "15 m, Cat6 or higher" |
| "add shampoo" | Shopping item *Shampoo* |
| "add clear men shampoo" while *Shampoo* is open | Claude is told about the similar item and asks: replace, keep both, or skip |
| "add a table" | Wishlist item *Table* |
| "add a charger cable, usb-c to usb-c, 60W" | Shopping item with notes |
| "remove the table" | Item removed; an ambiguous query returns candidates instead |
| "list" / "what's on my wishlist" | Open items, grouped by list |
| "I bought the shampoo for 189,90 at Trendyol" | Item checked off, purchase logged |
| "what did I pay for shampoo last time" | Purchase history |
| "remind me to rebuy shampoo every 2 months" | `recurring_days = 60` |
| "what's due again" | Recurring items whose interval has passed |
| "undo that" | Last MCP change reverted |
| "move the cable to the shopping list", "make it 2" | Item updated |

The AI deal job uses `list_items` (open wishlist and shopping items with notes
and target price) and `price_history`. Nothing in Basket is specific to it.

## 4. Data model

### Item

| Field | Type | Rules |
| --- | --- | --- |
| `id` | ULID string | Server or client generated |
| `list` | `shopping` \| `wishlist` | Default `shopping` |
| `title` | string | 1–200 characters after trimming |
| `quantity` | integer | 1–999, default 1; units belong in the title or notes |
| `notes` | string | 0–2000 characters; requirements, preferred brand, units ("2 kg"), links |
| `target_price` | integer kuruş or null | The price the owner would pay; readable by any client, such as the AI deal job |
| `recurring_days` | integer or null | 1–730; shopping items only |
| `status` | `open` \| `bought` \| `archived` | Stored status; see Auto-archive for the status clients see |
| `created_at`, `updated_at` | RFC 3339 | |
| `version` | integer | Incremented on every change |

Removing an item deletes it and its purchases; undo can restore both for 30
days, from the change log. `archived` is a separate, explicit action for items
the owner no longer wants but whose purchase history should stay.

There is no priority field and no separate links field. Links go in `notes`,
and the PWA renders URLs in notes as links.

### Purchase

| Field | Type | Rules |
| --- | --- | --- |
| `id` | ULID string | |
| `item_id` | Item id | |
| `date` | `YYYY-MM-DD` | Owner's time zone, default today |
| `price` | integer kuruş or null | Total paid |
| `quantity` | integer | Default the item's quantity |
| `store` | string or null | Free text, up to 100 characters |

Purchases of a deleted item are deleted with it.

### Change log

Every mutation writes a row with the actor (`pwa` or `mcp`), the operation, and
the item and purchase state before and after. It backs `undo` and is pruned
after 30 days.

## 5. Rules

### Amounts

Turkish lira only, stored as integer kuruş. Input and output use the
family-tab format: `"189,90"`, `"1.299,90"`, `"1299.90"` and `"1299"` parse;
output is `"1.299,90"`. Results also carry the integer `…Minor` value.

### Similar items

Titles are normalized: Turkish-aware lower case (`İ`→`i`, `I`→`ı`), diacritics
folded (`ş`→`s`, `ı`→`i` and so on), punctuation dropped, whitespace collapsed.
An open item on either list is similar to a new title when the normalized token
sets overlap by at least one token of three or more characters and either set
contains the other, or the normalized titles are equal.

`add_item` with a similar open item does not add. It returns the similar items
and Claude asks the owner whether to replace the existing item, keep both or
skip. Calling it again with `allow_similar: true` adds. Nothing is ever merged
automatically.
The PWA shows the same check as an inline hint without blocking; it asks the
server (`GET /api/similar`), so the rule runs in one place and the hint is
online only.

### Buying and recurrence

`mark_bought` sets `status = bought` and logs a purchase. Price and store are
optional; a purchase without them still records the date.

Only shopping items can recur. Setting `recurring_days` on a wishlist item is
rejected, and moving a recurring item to the wishlist clears `recurring_days`.
 For a recurring item,
the next due date is the latest purchase date plus `recurring_days`. A bought
recurring item is due when that date is today or earlier. Due items appear in a
"Due again" section of the shopping tab and in `due_items`; reopening is an
explicit action (`reopen_item`, or a tap). Nothing changes on its own.

Reopening a non-recurring bought item is allowed and keeps its history.

### Auto-archive

A bought non-recurring item whose latest purchase is more than 30 days old is
shown as `archived` everywhere (`/api`, MCP, PWA). The status is derived when
the list is read, so no background job runs; the stored row is rewritten to
`archived` the next time the item is written. Recurring bought items are never
auto-archived, because they come due again.

### Undo

Undo is separate per actor: Claude's `undo` only sees MCP changes, and the
PWA's undo only sees PWA changes. Neither can undo the other's last action.

`undo` reverts the most recent MCP change if every row it touched still has the
version that change produced. Otherwise it refuses and names the item that has
changed since. Undo is itself logged and can be undone: undoing twice
re-applies the change. The PWA has its own undo snackbar for its last action,
with the same version check.

## 6. Screens

- **Shopping** and **Wishlist** tabs. Open items, newest first. Wishlist rows
  show the target price. A quick-add field at the top splits on the first
  comma: "charger cable, usb-c 60W" becomes title *charger cable*, notes
  *usb-c 60W*. Titles that contain a comma are entered in the item sheet.
- Swipe right to mark bought immediately. A dismissible sheet then offers price
  and store; ignoring it keeps the bare purchase. Swipe left to delete, with
  undo.
- **Due again** section at the top of Shopping when any recurring item is due.
- **Item sheet**: all fields, move between lists, purchase history, reopen,
  archive.
- **Bought** view: bought items (non-recurring ones for 30 days, then they
  move to archived) and an Archived filter.
- **Settings**: sign out, export JSON, time zone. The time zone is stored on
  the server (`set_time_zone` op) and is not part of undo.

## 7. Platform

### PWA and server

One Go binary serves the PWA, `/api`, `/auth`, `/mcp`, `/healthz` and, on a
separate listener, `/metrics`. SQLite on `/data`.

### Sign-in

Single owner. OIDC authorization code with PKCE `S256` against Authelia; only
`OWNER_SUBJECT` (with the configured issuer) may sign in. Server-side sessions
in SQLite behind an opaque `HttpOnly`, `Secure`, `SameSite=Lax` cookie, as in
family-tab.

### Offline

The service worker caches the app shell only. The client keeps the last item
and purchase snapshot in IndexedDB and renders from it. Mutations are queued as
operations with a client-generated `op_id`, applied optimistically, and sent in
order with `POST /api/ops`. The server records applied `op_id`s, so a resend is
a no-op. Conflicts resolve per field, last write wins by server arrival; a
delete wins over any edit of the same item, earlier or later. There is no
conflict UI. After every send the client
replaces its snapshot with the server's.

### Agent access (MCP)

Streamable HTTP at `/mcp`, enabled when `MCP_CLIENT_ID` is set. Bearer JWT
access tokens from Authelia's `basket-mcp` client are verified for issuer,
audience, expiry, client and owner subject. Tools go through the same service
layer as `/api/ops`. Details in [DEPLOYMENT_MCP.md](DEPLOYMENT_MCP.md). Tool schemas and wire
shapes are in [API.md](API.md).

Tools:

| Tool | Input | Effect |
| --- | --- | --- |
| `add_item` | `title, list?, quantity?, notes?, target_price?, recurring_days?, allow_similar?` | Adds, or returns similar open items |
| `add_items` | `items[]` (same fields) | Adds each; reports added and similar per entry |
| `list_items` | `list?, status? (default open), query?` | Items with due dates and last purchase |
| `get_item` | `id` | Item with purchase history |
| `update_item` | `id, fields…` | Partial update; `null` clears an optional field |
| `remove_item` | `id` or `query` | Deletes by id; a query that matches more than one item returns candidates |
| `mark_bought` | `id, price?, store?, date?, quantity?` | Bought plus purchase row |
| `reopen_item` | `id` | Back to open |
| `price_history` | `id` or `query` | Purchases, newest first |
| `due_items` | `within_days?` (default 0) | Recurring items due by then |
| `undo` | — | Reverts the last MCP change (never a PWA change) |

Tool descriptions tell Claude to put requirements, units and links in `notes`
rather than the title, keep titles short and generic ("Ethernet cable"), and ask
the owner when `add_item` reports similar items.

### Backup

`VACUUM INTO` at startup and every `BACKUP_INTERVAL` into `BACKUP_DIR`, keeping
`BACKUP_KEEP` copies, plus Longhorn backups of the volume. JSON export from
Settings.

## 8. Edge cases

- Title only whitespace, quantity 0, negative price: rejected with a message
  naming the field.
- `remove_item` by query with zero matches: error, no change.
- `mark_bought` on a bought item: logs another purchase, keeps `bought`.
- Purchase without a price: shown as "—", skipped by price history averages.
- Changing `recurring_days` to null: the item stops being due, and if bought
  it auto-archives 30 days after its latest purchase.
- Undo of a removal after 30 days: refused, the change log row is gone.
- Offline delete, then an MCP edit of the same item before sync: the delete
  wins; the client shows the item as removed.
- Time zone change: due dates use the new zone from then on.

## 9. Roadmap

1. Items, lists, PWA online, OIDC, MCP add/list/update/remove.
2. Purchases, recurrence, due items, auto-archive, undo.
3. Offline queue.
4. Later: categories or store tags, sharing a read-only list link.

## 10. Decisions

| Decision | Choice |
| --- | --- |
| Offline | Queue changes and send them in order on reconnect |
| Sync conflicts | Per-field last write wins; delete wins over any edit |
| Remove | Hard delete with 30-day undo; archive is separate |
| Similar items | Block and ask; `allow_similar: true` adds anyway |
| Quantity | Integer; units in title or notes |
| Priority | None |
| Target price | Kept, structured |
| Links | In notes, linkified in the PWA |
| Check-off | Instant, optional price and store sheet |
| Undo | Separate per actor |
| Recurrence | Shopping items only; manual reopen from Due again |
| Bought items | Non-recurring auto-archive after 30 days, derived on read |
| Quick-add | Split on the first comma into title and notes |
| Deals | Out of scope; the AI job only reads the list |
