package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JaviCss/eco/port"
)

func TestReadOnlyOpenRefusesToCreateAMissingBase(t *testing.T) {
	dir := t.TempDir()
	seed, err := Open(Config{Profile: ProfileRuntime,
		UserDB:    filepath.Join(dir, "user.db"),
		ProjectDB: filepath.Join(dir, "project.db"),
		Origin:    "runtime",
	})
	if err != nil {
		t.Fatalf("seed Open: %v", err)
	}
	seed.Close()

	empty := t.TempDir()
	before := snapshot(t, empty)
	_, err = Open(Config{Profile: ProfileRuntime,
		UserDB:    filepath.Join(empty, "user.db"),
		ProjectDB: filepath.Join(empty, "project.db"),
		Origin:    "doctor",
		ReadOnly:  true,
	})
	if err == nil {
		t.Fatal("a read-only Open reported success on bases that do not exist")
	}
	if !isUnavailable(err) {
		t.Fatalf("a read-only Open on missing bases: got %v, want ErrUnavailable", err)
	}
	assertUnchanged(t, before, snapshot(t, empty))
}

func TestReadOnlyOpenReadsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	seed, err := Open(Config{Profile: ProfileRuntime,
		UserDB:    filepath.Join(dir, "user.db"),
		ProjectDB: filepath.Join(dir, "project.db"),
		Origin:    "runtime",
	})
	if err != nil {
		t.Fatalf("seed Open: %v", err)
	}
	if _, err := seed.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "a", Body: "the runtime fell over"}); err != nil {
		t.Fatalf("seed Append: %v", err)
	}
	seed.Close()

	before := snapshot(t, dir)
	ro, err := Open(Config{Profile: ProfileRuntime,
		UserDB:    filepath.Join(dir, "user.db"),
		ProjectDB: filepath.Join(dir, "project.db"),
		Origin:    "doctor",
		ReadOnly:  true,
	})
	if err != nil {
		t.Fatalf("read-only Open: %v", err)
	}
	if err := ro.Probe(context.Background()); err != nil {
		t.Fatalf("Probe on a read-only Store: %v", err)
	}
	info, err := ro.Inspect(port.ScopeUser)
	if err != nil {
		t.Fatalf("Inspect on a read-only Store: %v", err)
	}
	if info.JournalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", info.JournalMode)
	}
	if info.UserVersion != SchemaVersion || info.ApplicationID != ApplicationID {
		t.Fatalf("a read-only Store read user_version=%d application_id=%d, want %d and %d",
			info.UserVersion, info.ApplicationID, SchemaVersion, ApplicationID)
	}
	if _, err := ro.Append(context.Background(), port.ScopeUser, port.AxisZ2, port.Entry{ID: "b", Body: "b"}); err == nil {
		t.Fatal("a read-only Store accepted an Append")
	}
	if err := ro.Close(); err != nil {
		t.Fatalf("Close a read-only Store: %v", err)
	}
	assertUnchanged(t, before, snapshot(t, dir))
}
