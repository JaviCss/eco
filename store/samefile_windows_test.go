//go:build windows

package store

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"github.com/JaviCss/eco/port"
)

var procGetShortPathName = syscall.NewLazyDLL("kernel32.dll").NewProc("GetShortPathNameW")

func shortPathName(t *testing.T, path string) string {
	t.Helper()
	full, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(%q): %v", path, err)
	}
	buf := make([]uint16, 1024)
	n, _, callErr := procGetShortPathName.Call(uintptr(unsafe.Pointer(full)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		t.Skipf("GetShortPathName(%q) failed: %v", path, callErr)
	}
	return syscall.UTF16ToString(buf[:n])
}

func TestOpenRejectsAHardLinkOfTheSameBase(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	seed, err := Open(Config{Profile: ProfileRuntime, Origin: "runtime", UserDB: user, ProjectDB: filepath.Join(dir, "project.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	link := filepath.Join(dir, "user-hard.db")
	if err := os.Link(user, link); err != nil {
		t.Skipf("os.Link is not available here: %v", err)
	}
	before := listing(t, dir)
	if !sameFile(user, link) {
		t.Fatalf("sameFile did not see %q and %q as the same file", user, link)
	}
	_, err = Open(Config{Profile: ProfileRuntime, Origin: "runtime", UserDB: user, ProjectDB: link})
	if !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("Open with a hard link of the same base: got %v, want ErrUnavailable", err)
	}
	after := listing(t, dir)
	if len(before) != len(after) {
		t.Fatalf("the refused Open created files: %v -> %v", before, after)
	}
}

func TestOpenRejectsTheShortNameOfTheSameBase(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user-base-with-a-long-name.db")
	seed, err := Open(Config{Profile: ProfileRuntime, Origin: "runtime", UserDB: user, ProjectDB: filepath.Join(dir, "project.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	short := shortPathName(t, user)
	if short == user {
		t.Skipf("8.3 names are not generated here: %q has no short form", user)
	}
	before := listing(t, dir)
	if !sameFile(user, short) {
		t.Fatalf("sameFile(%q, %q) = false, want true", user, short)
	}
	_, err = Open(Config{Profile: ProfileRuntime, Origin: "runtime", UserDB: user, ProjectDB: short})
	if !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("Open with the 8.3 name of the same base: got %v, want ErrUnavailable", err)
	}
	t.Logf("long=%q short=%q", user, short)
	after := listing(t, dir)
	if len(before) != len(after) {
		t.Fatalf("the refused Open created files: %v -> %v", before, after)
	}
}