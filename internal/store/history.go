package store

import (
	"context"
	"time"
)

const Retention = 30 * 24 * time.Hour

type Change struct {
	Op   string `db:"op"`
	Body string `db:"body"`
}

func (tx *Tx) LatestChange(ctx context.Context, actor string, now time.Time) (*Change, error) {
	return find[Change](ctx, tx, `
		SELECT op, body FROM change_log WHERE actor = ? AND created_at >= ? ORDER BY id DESC LIMIT 1
	`, actor, now.Add(-Retention).Unix())
}

func (tx *Tx) AddChange(ctx context.Context, actor, op, body string, now time.Time) error {
	return tx.exec(ctx,
		"INSERT INTO change_log (actor, op, body, created_at) VALUES (?, ?, ?, ?)",
		actor, op, body, now.Unix())
}

func (tx *Tx) AppliedOp(ctx context.Context, id string, now time.Time) (*string, error) {
	return find[string](ctx, tx,
		"SELECT result FROM applied_ops WHERE op_id = ? AND created_at >= ?",
		id, now.Add(-Retention).Unix())
}

func (tx *Tx) SaveAppliedOp(ctx context.Context, id, result string, now time.Time) error {
	return tx.exec(ctx, `
		INSERT INTO applied_ops (op_id, result, created_at) VALUES (?, ?, ?)
		ON CONFLICT(op_id) DO UPDATE SET result = excluded.result, created_at = excluded.created_at
	`, id, result, now.Unix())
}

func (tx *Tx) TimeZone(ctx context.Context) (string, error) {
	zone, err := find[string](ctx, tx, "SELECT time_zone FROM settings WHERE id = 1")
	if zone == nil {
		return "", err
	}

	return *zone, nil
}

func (tx *Tx) SetTimeZone(ctx context.Context, zone string) error {
	return tx.exec(ctx, `
		INSERT INTO settings (id, time_zone) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET time_zone = excluded.time_zone
	`, zone)
}

func (tx *Tx) Prune(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-Retention).Unix()

	err := tx.exec(ctx, "DELETE FROM change_log WHERE created_at < ?", cutoff)
	if err != nil {
		return err
	}

	return tx.exec(ctx, "DELETE FROM applied_ops WHERE created_at < ?", cutoff)
}
