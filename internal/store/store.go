// Package store owns the SQLite schema, transactions and queries.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/zekihan/go-commons/sqlitestore"
)

// Store wraps the shared SQLite store with basket's migrations and queries.
type Store struct {
	inner *sqlitestore.Store
}

// Tx is a single database transaction.
type Tx struct {
	inner *sqlitestore.Tx
}

// Inner returns the underlying transaction for basket queries.
func (tx *Tx) Inner() *sqlx.Tx {
	return tx.inner.Inner()
}

// Open opens (creating if needed) and migrates the database at path, then
// prunes expired history.
func Open(ctx context.Context, path string) (*Store, error) {
	inner, err := sqlitestore.Open(ctx, path, Migrations)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	st := &Store{inner: inner}

	err = st.Write(ctx, func(ctx context.Context, tx *Tx) error {
		return tx.Prune(ctx, time.Now())
	})
	if err != nil {
		return nil, fmt.Errorf("prune history: %w", errors.Join(err, st.Close()))
	}

	return st, nil
}

// DB exposes the handle for auth queries and database backups.
func (s *Store) DB() *sqlx.DB { return s.inner.DB() }

// Close closes the database.
func (s *Store) Close() error {
	err := s.inner.Close()
	if err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	return nil
}

// Read runs run in a read-only transaction.
func (s *Store) Read(ctx context.Context, run func(context.Context, *Tx) error) error {
	//nolint:wrapcheck // The callback's errors are already wrapped by basket code.
	return s.inner.Read(ctx, func(ctx context.Context, tx *sqlitestore.Tx) error {
		return run(ctx, &Tx{inner: tx})
	})
}

// Write runs run in a write transaction.
func (s *Store) Write(ctx context.Context, run func(context.Context, *Tx) error) error {
	//nolint:wrapcheck // The callback's errors are already wrapped by basket code.
	return s.inner.Write(ctx, func(ctx context.Context, tx *sqlitestore.Tx) error {
		return run(ctx, &Tx{inner: tx})
	})
}
