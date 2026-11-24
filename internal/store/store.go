package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"

	// SQLite driver for database/sql.
	_ "modernc.org/sqlite"
)

// Store wraps the database. Its single connection serializes transactions,
// including writes to the auth tables.
type Store struct {
	db *sqlx.DB
}

type Tx struct {
	tx *sqlx.Tx
}

var errSchemaVersion = errors.New("unsupported schema version")

// Open opens (creating if needed) and migrates the database at path, enabling
// WAL and foreign keys.
func Open(ctx context.Context, path string) (*Store, error) {
	options := url.Values{
		"_pragma": {"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)"},
		"_txlock": {"immediate"},
	}
	dsn := new(url.URL)
	dsn.Scheme = "file"
	dsn.RawQuery = options.Encode()

	if path == ":memory:" {
		dsn.Opaque = path
	} else {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("database path: %w", err)
		}

		dsn.Path = absolute
	}

	db, err := sqlx.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	st := &Store{db: db}

	err = st.Write(ctx, func(ctx context.Context, tx *Tx) error {
		migrationErr := migrate(ctx, tx)
		if migrationErr != nil {
			return migrationErr
		}

		return tx.Prune(ctx, time.Now())
	})
	if err != nil {
		return nil, errors.Join(err, st.Close())
	}

	return st, nil
}

func migrate(ctx context.Context, tx *Tx) error {
	var version int

	err := tx.tx.GetContext(ctx, &version, "PRAGMA user_version")
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	if version < 0 || version > len(Migrations) {
		return fmt.Errorf("%w %d", errSchemaVersion, version)
	}

	for index := version; index < len(Migrations); index++ {
		_, err = tx.tx.ExecContext(ctx, Migrations[index])
		if err != nil {
			return fmt.Errorf("migration %d: %w", index+1, err)
		}

		_, err = tx.tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(index+1))
		if err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}

	return nil
}

// DB exposes the handle for auth queries and database backups.
func (s *Store) DB() *sqlx.DB { return s.db }

func (s *Store) Close() error {
	err := s.db.Close()
	if err != nil {
		return fmt.Errorf("close sqlite: %w", err)
	}

	return nil
}

func (s *Store) Read(ctx context.Context, run func(context.Context, *Tx) error) error {
	return s.transaction(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelDefault}, run)
}

func (s *Store) Write(ctx context.Context, run func(context.Context, *Tx) error) error {
	return s.transaction(ctx, nil, run)
}

func (s *Store) transaction(
	ctx context.Context,
	options *sql.TxOptions,
	run func(context.Context, *Tx) error,
) error {
	tx, err := s.db.BeginTxx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin sqlite transaction: %w", err)
	}

	// Rollback is also needed on panic. It is a no-op after commit.
	defer tx.Rollback() //nolint:errcheck // The transaction is already done after a successful commit.

	err = run(ctx, &Tx{tx: tx})
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit sqlite transaction: %w", err)
	}

	return nil
}
