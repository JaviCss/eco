package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaviCss/eco/port"
)

func TestStoreRejectsAnIDOccupiedOnAnotherAxis(t *testing.T) {
	s := newStoreAt(t, t.TempDir())
	ctx := context.Background()
	if _, err := s.Append(ctx, port.ScopeUser, port.AxisZ2, port.Entry{ID: "shared", Body: "in Z2"}); err != nil {
		t.Fatalf("seed Z2: %v", err)
	}
	_, err := s.Append(ctx, port.ScopeUser, port.AxisR, port.Entry{ID: "shared", Body: "in R"})
	if !errors.Is(err, port.ErrInvalidEntry) {
		t.Fatalf("the same id on another axis: got %v, want ErrInvalidEntry", err)
	}
	if !strings.Contains(err.Error(), string(port.AxisZ2)) {
		t.Fatalf("the rejection must name the axis that holds the id, got %v", err)
	}
	read, err := s.Read(ctx, port.ScopeUser, port.AxisR, 10)
	if err != nil {
		t.Fatalf("read R: %v", err)
	}
	if len(read) != 0 {
		t.Fatalf("R holds %d entries after the refusal, want 0: %v", len(read), read)
	}
	kept, err := s.Read(ctx, port.ScopeUser, port.AxisZ2, 10)
	if err != nil {
		t.Fatalf("read Z2: %v", err)
	}
	if len(kept) != 1 || kept[0].Body != "in Z2" {
		t.Fatalf("the refusal changed Z2: %v", kept)
	}
}

func TestStoreSearchReportsAClosedHandleAsUnavailable(t *testing.T) {
	s := newStoreAt(t, t.TempDir())
	ctx := context.Background()
	if _, err := s.Append(ctx, port.ScopeUser, port.AxisZ2, port.Entry{ID: "a", Body: "the runtime fell over"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	db, err := s.db(port.ScopeUser)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the handle under the Store: %v", err)
	}
	_, err = s.Search(ctx, port.ScopeUser, port.AxisZ2, "runtime", 10)
	if !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("Search on a closed handle: got %v, want ErrUnavailable", err)
	}
	if errors.Is(err, port.ErrNotFound) {
		t.Fatalf("Search on a closed handle reported a miss, which hides the outage: %v", err)
	}
}

func TestStoreWriteTransactionTakesTheWriteLockUpFront(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deferred bool
		wantHeld bool
	}{
		{name: "immediate", deferred: false, wantHeld: true},
		{name: "deferred", deferred: true, wantHeld: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			user := filepath.Join(dir, "user.db")
			s, err := Open(Config{
				UserDB:      user,
				ProjectDB:   filepath.Join(dir, "project.db"),
				Origin:      "runtime",
				DeferredTx:  tc.deferred,
				BusyTimeout: DefaultBusyTimeout,
			})
			if err != nil {
				t.Fatalf("Open in the %s case: %v", tc.name, err)
			}
			t.Cleanup(func() { s.Close() })
			if strings.Contains(s.dsn(user), "_txlock=immediate") == tc.deferred {
				t.Fatalf("the write dsn of the %s case does not match the mode: %s", tc.name, s.dsn(user))
			}
			db, err := s.db(port.ScopeUser)
			if err != nil {
				t.Fatalf("handle: %v", err)
			}
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatalf("BeginTx: %v", err)
			}
			var rows int
			if err := tx.QueryRow(`SELECT count(*) FROM entries`).Scan(&rows); err != nil {
				tx.Rollback()
				t.Fatalf("read inside the Store's write transaction: %v", err)
			}
			other, err := sql.Open("sqlite", inspectDSN(user, 250))
			if err != nil {
				tx.Rollback()
				t.Fatalf("open the other connection: %v", err)
			}
			defer other.Close()
			held, detail := writeLockRefused(t, other, tx)
			if held != tc.wantHeld {
				t.Fatalf("with a %s write transaction the Store's lock was held=%t, want %t (%s)", tc.name, held, tc.wantHeld, detail)
			}
			tx.Rollback()
		})
	}
}

func writeLockRefused(t *testing.T, other *sql.DB, tx *sql.Tx) (bool, string) {
	t.Helper()
	ctx := context.Background()
	if _, err := other.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return true, "BEGIN IMMEDIATE: " + err.Error()
	}
	_, err := other.ExecContext(ctx, sqlInsert, "intruder", "Z2", "user", 1, "b", "{}")
	detail := "the other connection wrote"
	if err != nil {
		detail = err.Error()
	}
	if _, rollbackErr := other.Exec(`ROLLBACK`); rollbackErr != nil {
		t.Fatalf("ROLLBACK on the other connection: %v", rollbackErr)
	}
	return err != nil, detail
}