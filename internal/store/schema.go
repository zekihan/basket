// Package store owns the SQLite schema, transactions and queries.
package store

// Migrations are applied in order; PRAGMA user_version records how many ran.
var Migrations = []string{
	`
	CREATE TABLE items (
		id TEXT PRIMARY KEY NOT NULL,
		list TEXT NOT NULL CHECK (list IN ('shopping', 'wishlist')),
		title TEXT NOT NULL,
		quantity INTEGER NOT NULL CHECK (quantity BETWEEN 1 AND 999),
		notes TEXT NOT NULL,
		target_price_minor INTEGER CHECK (target_price_minor >= 0),
		recurring_days INTEGER CHECK (recurring_days IS NULL OR
			(list = 'shopping' AND recurring_days BETWEEN 1 AND 730)),
		status TEXT NOT NULL CHECK (status IN ('open', 'bought', 'archived')),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		version INTEGER NOT NULL CHECK (version > 0)
	);

	-- Preserve the version when a row is deleted, including across ID reuse.
	CREATE TABLE item_versions (
		id TEXT PRIMARY KEY NOT NULL,
		version INTEGER NOT NULL CHECK (version > 0)
	);

	CREATE TABLE purchases (
		id TEXT PRIMARY KEY NOT NULL,
		item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		date TEXT NOT NULL,
		price_minor INTEGER CHECK (price_minor >= 0),
		quantity INTEGER NOT NULL CHECK (quantity BETWEEN 1 AND 999),
		store TEXT,
		created_at TEXT NOT NULL
	);
	CREATE INDEX idx_purchases_item ON purchases(item_id, date, created_at);

	CREATE TABLE change_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		actor TEXT NOT NULL,
		op TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		body TEXT NOT NULL
	);
	CREATE INDEX idx_change_log_actor ON change_log(actor, id);
	CREATE INDEX idx_change_log_created ON change_log(created_at);

	CREATE TABLE applied_ops (
		op_id TEXT PRIMARY KEY NOT NULL,
		result TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX idx_applied_ops_created ON applied_ops(created_at);

	CREATE TABLE settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		time_zone TEXT NOT NULL
	);

	-- Server-side sessions; token_hash is the SHA-256 of the cookie value.
	CREATE TABLE sessions (
		token_hash TEXT PRIMARY KEY,
		issuer TEXT NOT NULL,
		subject TEXT NOT NULL,
		name TEXT,
		email TEXT,
		created_at TEXT NOT NULL,
		renewed_at TEXT NOT NULL,
		expires_at TEXT NOT NULL
	);
	CREATE INDEX idx_sessions_expires ON sessions(expires_at);

	-- One-time OIDC sign-in transactions; id_hash is the SHA-256 of the
	-- transaction cookie value.
	CREATE TABLE oidc_transactions (
		id_hash TEXT PRIMARY KEY,
		state TEXT NOT NULL,
		nonce TEXT NOT NULL,
		pkce_verifier TEXT NOT NULL,
		expires_at TEXT NOT NULL
	);
	`,
}
