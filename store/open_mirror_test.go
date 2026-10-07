package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stampBase(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", inspectDSN(path, 5000))
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s on %s: %v", stmt, path, err)
		}
	}
}

func foreignStamps() []string {
	return []string{`PRAGMA application_id = 424242`, `CREATE TABLE payload(x TEXT)`}
}

func newerStamps() []string {
	return []string{
		fmt.Sprintf(`PRAGMA application_id = %d`, ApplicationID),
		fmt.Sprintf(`PRAGMA user_version = %d`, SchemaVersion+7),
	}
}

func TestOpenRefusesTheBadBaseBeforeItTouchesTheGoodOne(t *testing.T) {
	t.Run("foreign_application_id_in_the_project_base", func(t *testing.T) {
		dir := t.TempDir()
		project := filepath.Join(dir, "project.db")
		stampBase(t, project, foreignStamps()...)
		assertRejectedUntouched(t, dir, filepath.Join(dir, "user.db"), project, "application_id")
	})

	t.Run("newer_user_version_in_the_project_base", func(t *testing.T) {
		dir := t.TempDir()
		project := filepath.Join(dir, "project.db")
		stampBase(t, project, newerStamps()...)
		assertRejectedUntouched(t, dir, filepath.Join(dir, "user.db"), project, "user_version")
	})

	t.Run("reparse_point_on_the_project_base", func(t *testing.T) {
		root := t.TempDir()
		real := filepath.Join(root, "real")
		if err := os.MkdirAll(real, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		link := filepath.Join(root, "link")
		if err := makeJunction(link, real); err != nil {
			t.Skipf("this host cannot create a junction: %v", err)
		}
		stampBase(t, filepath.Join(real, "project.db"), newerStamps()...)
		before := snapshot(t, root)
		_, err := Open(Config{Profile: ProfileRuntime,
			UserDB:    filepath.Join(real, "user.db"),
			ProjectDB: filepath.Join(link, "project.db"),
			Origin:    "runtime",
		})
		if err == nil {
			t.Fatal("Open with a reparse point on the project base reported success")
		}
		if !isUnavailable(err) {
			t.Fatalf("Open with a reparse point on the project base: got %v, want ErrUnavailable", err)
		}
		if !strings.Contains(err.Error(), "reparse point") {
			t.Fatalf("the rejection must name the reason, got %v", err)
		}
		assertUnchanged(t, before, snapshot(t, root))
	})

	t.Run("same_path_for_both_bases", func(t *testing.T) {
		dir := t.TempDir()
		same := filepath.Join(dir, "same.db")
		assertRejectedUntouched(t, dir, same, same, "same file")
	})
}

func assertRejectedUntouched(t *testing.T, dir, user, project, reason string) {
	t.Helper()
	before := snapshot(t, dir)
	_, err := Open(Config{Profile: ProfileRuntime, UserDB: user, ProjectDB: project, Origin: "runtime"})
	if err == nil {
		t.Fatal("Open reported success on a configuration it must refuse")
	}
	if !isUnavailable(err) {
		t.Fatalf("Open: got %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), reason) {
		t.Fatalf("the rejection must name %q, got %v", reason, err)
	}
	assertUnchanged(t, before, snapshot(t, dir))
}

func TestOpenCreatesBothBasesWhenBothAreMissing(t *testing.T) {
	dir := t.TempDir()
	s := newStoreAt(t, dir)
	if err := s.Probe(context.Background()); err != nil {
		t.Fatalf("Probe on a Store built by the two-phase Open: got %v, want nil", err)
	}
	for _, name := range []string{"user.db", "project.db"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s was not created: %v", name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s was created empty", name)
		}
	}
}

func TestOpenChecksTheUserBaseBeforeItCreatesTheProjectBase(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project.db")
	stampBase(t, project, newerStamps()...)
	assertRejectedUntouched(t, dir, project, filepath.Join(dir, "user.db"), "user_version")
}
