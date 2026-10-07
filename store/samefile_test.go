package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/JaviCss/eco/port"
)

func listing(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}

func TestOpenRejectsTwoPathsThatAreTheSameBaseByAnotherName(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	seed, err := Open(Config{Profile: ProfileRuntime, Origin: "runtime", UserDB: user, ProjectDB: filepath.Join(dir, "project.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	before := listing(t, dir)
	odd := filepath.Join(dir, "sub", "..", "user.db")
	if !sameFile(user, odd) {
		t.Fatalf("sameFile(%q, %q) = false, want true", user, odd)
	}
	_, err = Open(Config{Profile: ProfileRuntime, Origin: "runtime", UserDB: user, ProjectDB: odd})
	if !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("Open with the same base under another name: got %v, want ErrUnavailable", err)
	}
	after := listing(t, dir)
	if len(before) != len(after) {
		t.Fatalf("the refused Open created files: %v -> %v", before, after)
	}
}

func TestSameFileTellsTwoDifferentBasesApart(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, "a", "base.db")
	right := filepath.Join(dir, "b", "base.db")
	if sameFile(left, right) {
		t.Fatalf("sameFile(%q, %q) = true, want false", left, right)
	}
	if sameFile(filepath.Join(dir, "a", "base.db"), filepath.Join(dir, "a", "base.db")) != true {
		t.Fatal("sameFile of one path with itself must be true")
	}
}