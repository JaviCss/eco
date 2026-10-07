package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaviCss/eco/port"
)

type outageStore struct {
	inner *Store
	down  bool
}

func (o *outageStore) SetUnavailable(down bool) {
	o.down = down
	if down {
		if err := o.inner.shutdown(); err != nil {
			panic(err)
		}
		return
	}
	if err := o.inner.restart(); err != nil {
		panic(err)
	}
}

func (o *outageStore) guard(ctx context.Context, op string, scope port.Scope, axis port.Axis) error {
	if err := checkTarget(scope, axis); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.down {
		return fmt.Errorf("eco: %s: %w", op, port.ErrUnavailable)
	}
	return nil
}

func (o *outageStore) Read(ctx context.Context, scope port.Scope, axis port.Axis, limit int) ([]port.Entry, error) {
	if err := o.guard(ctx, "Read", scope, axis); err != nil {
		return nil, err
	}
	if err := checkLimit(limit); err != nil {
		return nil, err
	}
	return o.inner.Read(ctx, scope, axis, limit)
}

func (o *outageStore) Append(ctx context.Context, scope port.Scope, axis port.Axis, entry port.Entry) (port.Entry, error) {
	if err := o.guard(ctx, "Append", scope, axis); err != nil {
		return port.Entry{}, err
	}
	return o.inner.Append(ctx, scope, axis, entry)
}

func (o *outageStore) Search(ctx context.Context, scope port.Scope, axis port.Axis, query string, limit int) ([]port.Entry, error) {
	if err := o.guard(ctx, "Search", scope, axis); err != nil {
		return nil, err
	}
	if err := checkLimit(limit); err != nil {
		return nil, err
	}
	return o.inner.Search(ctx, scope, axis, query, limit)
}

func (o *outageStore) Probe(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.down {
		return fmt.Errorf("eco: Probe: %w", port.ErrUnavailable)
	}
	return o.inner.Probe(ctx)
}

func newStoreAt(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(Config{
		UserDB:    filepath.Join(dir, "user.db"),
		ProjectDB: filepath.Join(dir, "project.db"),
		Origin:    "runtime",
	})
	if err != nil {
		t.Fatalf("Open: got error %v, want nil", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreConformance(t *testing.T) {
	port.Conformance(t,
		func() port.Port { return newStoreAt(t, t.TempDir()) },
		func() port.Outage { return &outageStore{inner: newStoreAt(t, t.TempDir())} },
	)
}

func TestStoreOpenLeavesNoArtifactsForFreshBases(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir before: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("the temp dir was not empty before Open: %v", before)
	}
	s, err := Open(Config{UserDB: user, ProjectDB: project, Origin: "runtime"})
	if err != nil {
		t.Fatalf("Open: got error %v, want nil", err)
	}
	defer s.Close()
	if err := s.Probe(context.Background()); err != nil {
		t.Fatalf("Probe on a fresh Store: got %v, want nil", err)
	}
	var appID, userVersion int
	db, err := sql.Open("sqlite", inspectDSN(user, 5000))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	if err := db.QueryRow(`PRAGMA application_id`).Scan(&appID); err != nil {
		t.Fatalf("application_id: %v", err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if appID != ApplicationID {
		t.Fatalf("application_id = %d, want %d", appID, ApplicationID)
	}
	if userVersion != SchemaVersion {
		t.Fatalf("user_version = %d, want %d", userVersion, SchemaVersion)
	}
}

func TestStoreRejectsForeignApplicationID(t *testing.T) {
	dir := t.TempDir()
	foreign := filepath.Join(dir, "foreign.db")
	db, err := sql.Open("sqlite", inspectDSN(foreign, 5000))
	if err != nil {
		t.Fatalf("create foreign: %v", err)
	}
	if _, err := db.Exec(`PRAGMA application_id = 424242`); err != nil {
		t.Fatalf("stamp foreign application_id: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE payload(x TEXT)`); err != nil {
		t.Fatalf("create foreign table: %v", err)
	}
	db.Close()

	before := snapshot(t, dir)
	_, err = Open(Config{UserDB: foreign, ProjectDB: filepath.Join(dir, "project.db"), Origin: "runtime"})
	if err == nil {
		t.Fatal("Open on a foreign application_id reported success")
	}
	if !isUnavailable(err) {
		t.Fatalf("Open on a foreign application_id: got %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "application_id") {
		t.Fatalf("the rejection must name the reason, got %v", err)
	}
	assertUnchanged(t, before, snapshot(t, dir))
}

func TestStoreRejectsNewerSchema(t *testing.T) {
	dir := t.TempDir()
	newer := filepath.Join(dir, "newer.db")
	db, err := sql.Open("sqlite", inspectDSN(newer, 5000))
	if err != nil {
		t.Fatalf("create newer: %v", err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA application_id = %d`, ApplicationID)); err != nil {
		t.Fatalf("stamp application_id: %v", err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, SchemaVersion+7)); err != nil {
		t.Fatalf("stamp user_version: %v", err)
	}
	db.Close()

	before := snapshot(t, dir)
	_, err = Open(Config{UserDB: newer, ProjectDB: filepath.Join(dir, "project.db"), Origin: "runtime"})
	if err == nil {
		t.Fatal("Open on a newer schema reported success")
	}
	if !isUnavailable(err) {
		t.Fatalf("Open on a newer schema: got %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "user_version") {
		t.Fatalf("the rejection must name the reason, got %v", err)
	}
	assertUnchanged(t, before, snapshot(t, dir))
}

func TestStoreRejectsSamePathForBothBases(t *testing.T) {
	dir := t.TempDir()
	same := filepath.Join(dir, "same.db")
	before := snapshot(t, dir)
	_, err := Open(Config{UserDB: same, ProjectDB: same, Origin: "runtime"})
	if err == nil {
		t.Fatal("Open with the same path for both bases reported success")
	}
	if !strings.Contains(err.Error(), "same file") {
		t.Fatalf("the rejection must name the reason, got %v", err)
	}
	assertUnchanged(t, before, snapshot(t, dir))
}

func TestStoreRejectsReparseParent(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := makeJunction(link, real); err != nil {
		t.Skipf("this host cannot create a junction: %v", err)
	}
	before := snapshot(t, root)
	_, err := Open(Config{
		UserDB:    filepath.Join(link, "user.db"),
		ProjectDB: filepath.Join(real, "project.db"),
		Origin:    "runtime",
	})
	if err == nil {
		t.Fatal("Open under a junction reported success")
	}
	if !isUnavailable(err) {
		t.Fatalf("Open under a junction: got %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("the rejection must name the reason, got %v", err)
	}
	assertUnchanged(t, before, snapshot(t, root))
}

func snapshot(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	out := map[string]int64{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info %s: %v", entry.Name(), err)
		}
		out[entry.Name()] = info.Size()
	}
	return out
}

func assertUnchanged(t *testing.T, before, after map[string]int64) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("the directory changed: before %v, after %v", before, after)
	}
	for name, size := range before {
		got, ok := after[name]
		if !ok {
			t.Fatalf("%s disappeared: before %v, after %v", name, before, after)
		}
		if got != size {
			t.Fatalf("%s changed size: before %d, after %d", name, size, got)
		}
	}
}

func isUnavailable(err error) bool {
	return err != nil && errorsIs(err, port.ErrUnavailable)
}
