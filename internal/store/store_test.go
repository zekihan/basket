package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenReopen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "basket #1.db")

	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	for pragma, want := range map[string]string{
		"PRAGMA user_version": "1",
		"PRAGMA journal_mode": "wal",
		"PRAGMA foreign_keys": "1",
		"PRAGMA busy_timeout": "5000",
	} {
		var got string

		err = st.DB().GetContext(t.Context(), &got, pragma)
		if err != nil || got != want {
			t.Fatalf("%s = %q, want %q, error %v", pragma, got, want, err)
		}
	}

	err = st.Write(t.Context(), func(ctx context.Context, tx *Tx) error {
		return tx.SetTimeZone(ctx, "Europe/Istanbul")
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.Close()
	if err != nil {
		t.Fatal(err)
	}

	st, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	err = st.Read(t.Context(), func(ctx context.Context, tx *Tx) error {
		got, queryErr := tx.TimeZone(ctx)
		if got != "Europe/Istanbul" {
			t.Errorf("reopened time_zone = %q", got)
		}

		return queryErr
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenPrunesExpiredHistory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "basket.db")

	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	err = st.Write(t.Context(), func(ctx context.Context, tx *Tx) error {
		for key, created := range map[string]time.Time{
			"older": now.Add(-Retention - time.Hour),
			"newer": now,
		} {
			writeErr := tx.AddChange(ctx, key, "op", "{}", created)
			if writeErr != nil {
				return writeErr
			}

			writeErr = tx.SaveAppliedOp(ctx, key, "{}", created)
			if writeErr != nil {
				return writeErr
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.Close()
	if err != nil {
		t.Fatal(err)
	}

	st, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	for _, query := range []string{"SELECT count(*) FROM change_log", "SELECT count(*) FROM applied_ops"} {
		var count int

		err = st.DB().GetContext(t.Context(), &count, query)
		if err != nil || count != 1 {
			t.Fatalf("%s = %d, %v, want 1", query, count, err)
		}
	}
}
