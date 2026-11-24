//nolint:tagliatelle // Stored snapshots use snake_case field names.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const (
	itemColumns = "id, list, title, quantity, notes, target_price_minor, " +
		"recurring_days, status, created_at, updated_at, version"
	purchaseColumns = "id, item_id, date, price_minor, quantity, store, created_at"
)

type Item struct {
	ID               string `json:"id"                 db:"id"`
	List             string `json:"list"               db:"list"`
	Title            string `json:"title"              db:"title"`
	Notes            string `json:"notes"              db:"notes"`
	TargetPriceMinor *int64 `json:"target_price_minor" db:"target_price_minor"`
	RecurringDays    *int   `json:"recurring_days"     db:"recurring_days"`
	Status           string `json:"status"             db:"status"`
	CreatedAt        string `json:"created_at"         db:"created_at"`
	UpdatedAt        string `json:"updated_at"         db:"updated_at"`
	Quantity         int    `json:"quantity"           db:"quantity"`
	Version          int64  `json:"version"            db:"version"`
}

type Purchase struct {
	ID         string  `json:"id"          db:"id"`
	ItemID     string  `json:"item_id"     db:"item_id"`
	Date       string  `json:"date"        db:"date"`
	PriceMinor *int64  `json:"price_minor" db:"price_minor"`
	Store      *string `json:"store"       db:"store"`
	CreatedAt  string  `json:"created_at"  db:"created_at"`
	Quantity   int     `json:"quantity"    db:"quantity"`
}

func (tx *Tx) Items(ctx context.Context) ([]Item, error) {
	var items []Item

	err := tx.tx.SelectContext(
		ctx,
		&items,
		"SELECT "+itemColumns+" FROM items ORDER BY created_at DESC, id DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}

	return items, nil
}

func (tx *Tx) Item(ctx context.Context, id string) (*Item, error) {
	return find[Item](ctx, tx, "SELECT "+itemColumns+" FROM items WHERE id = ?", id)
}

func (tx *Tx) SaveItem(ctx context.Context, item *Item) error {
	_, err := tx.tx.NamedExecContext(ctx, `
		INSERT INTO items (
			id, list, title, quantity, notes, target_price_minor, recurring_days,
			status, created_at, updated_at, version
		) VALUES (
			:id, :list, :title, :quantity, :notes, :target_price_minor, :recurring_days,
			:status, :created_at, :updated_at, :version
		) ON CONFLICT(id) DO UPDATE SET
			list = excluded.list, title = excluded.title, quantity = excluded.quantity,
			notes = excluded.notes, target_price_minor = excluded.target_price_minor,
			recurring_days = excluded.recurring_days, status = excluded.status,
			created_at = excluded.created_at, updated_at = excluded.updated_at, version = excluded.version
	`, item)
	if err != nil {
		return fmt.Errorf("save item: %w", err)
	}

	return nil
}

func (tx *Tx) DeleteItem(ctx context.Context, id string) error {
	return tx.exec(ctx, "DELETE FROM items WHERE id = ?", id)
}

func (tx *Tx) ItemVersion(ctx context.Context, id string) (int64, error) {
	version, err := find[int64](ctx, tx, "SELECT version FROM item_versions WHERE id = ?", id)
	if version == nil {
		return 0, err
	}

	return *version, nil
}

func (tx *Tx) SaveItemVersion(ctx context.Context, id string, version int64) error {
	return tx.exec(ctx, `
		INSERT INTO item_versions (id, version) VALUES (?, ?)
		ON CONFLICT(id) DO UPDATE SET version = excluded.version`, id, version)
}

func (tx *Tx) Purchases(ctx context.Context) ([]Purchase, error) {
	var rows []Purchase

	err := tx.tx.SelectContext(ctx, &rows,
		"SELECT "+purchaseColumns+" FROM purchases ORDER BY date DESC, created_at DESC, id DESC")
	if err != nil {
		return nil, fmt.Errorf("list purchases: %w", err)
	}

	return rows, nil
}

func (tx *Tx) ItemPurchases(ctx context.Context, id string) ([]Purchase, error) {
	var rows []Purchase

	err := tx.tx.SelectContext(
		ctx,
		&rows,
		"SELECT "+purchaseColumns+" FROM purchases WHERE item_id = ? ORDER BY date DESC, created_at DESC, id DESC",
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("list purchases: %w", err)
	}

	return rows, nil
}

func (tx *Tx) Purchase(ctx context.Context, id string) (*Purchase, error) {
	return find[Purchase](ctx, tx, "SELECT "+purchaseColumns+" FROM purchases WHERE id = ?", id)
}

func (tx *Tx) SavePurchase(ctx context.Context, purchase *Purchase) error {
	_, err := tx.tx.NamedExecContext(ctx, `
		INSERT INTO purchases (id, item_id, date, price_minor, quantity, store, created_at)
		VALUES (:id, :item_id, :date, :price_minor, :quantity, :store, :created_at)
		ON CONFLICT(id) DO UPDATE SET item_id = excluded.item_id, date = excluded.date,
			price_minor = excluded.price_minor, quantity = excluded.quantity,
			store = excluded.store, created_at = excluded.created_at
	`, purchase)
	if err != nil {
		return fmt.Errorf("save purchase: %w", err)
	}

	return nil
}

func (tx *Tx) DeletePurchase(ctx context.Context, id string) error {
	return tx.exec(ctx, "DELETE FROM purchases WHERE id = ?", id)
}

func find[T any](ctx context.Context, tx *Tx, query string, args ...any) (*T, error) {
	var row T

	err := tx.tx.GetContext(ctx, &row, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // A missing row is represented by a nil pointer.
	}

	if err != nil {
		return nil, fmt.Errorf("query sqlite row: %w", err)
	}

	return &row, nil
}

func (tx *Tx) exec(ctx context.Context, query string, args ...any) error {
	_, err := tx.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("write sqlite row: %w", err)
	}

	return nil
}
