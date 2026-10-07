package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaviCss/eco/store"
)

func buildEco(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "eco.exe")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = filepath.Join(root, "cmd", "eco")
	build.Env = append(build.Environ(), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build eco: %v\n%s", err, out)
	}
	return binary
}

func run(t *testing.T, binary string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return string(out), code
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

func seedBases(t *testing.T, user, project string) {
	t.Helper()
	s, err := store.Open(store.Config{UserDB: user, ProjectDB: project, Origin: "seed"})
	if err != nil {
		t.Fatalf("seed the bases: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close the seeded bases: %v", err)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

func TestDoctorReportsBothBases(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	out, code := run(t, binary, "doctor", "--user-db", user, "--project-db", project)
	if code != 0 {
		t.Fatalf("doctor exited %d:\n%s", code, out)
	}
	for _, want := range []string{"sqlite_version:", "user_version: 1", "journal_mode: wal", "size_bytes:", "application_id:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output is missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "user db:") != 1 || strings.Count(out, "project db:") != 1 {
		t.Fatalf("doctor must report exactly one user db and one project db:\n%s", out)
	}
}

func TestDoctorLeavesTheDirectoryAlone(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	before := map[string]int64{}
	for _, name := range dirNames(t, dir) {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		before[name] = info.Size()
	}
	out, code := run(t, binary, "doctor", "--user-db", user, "--project-db", project)
	if code != 0 {
		t.Fatalf("doctor exited %d:\n%s", code, out)
	}
	after := map[string]int64{}
	for _, name := range dirNames(t, dir) {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		after[name] = info.Size()
	}
	if len(after) != len(before) {
		t.Fatalf("doctor changed the directory: before %v, after %v", before, after)
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

func TestDoctorRefusesToCreateAMissingBase(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	out, code := run(t, binary, "doctor",
		"--user-db", filepath.Join(dir, "user.db"),
		"--project-db", filepath.Join(dir, "project.db"))
	if code == 0 {
		t.Fatalf("doctor reported success on bases that do not exist:\n%s", out)
	}
	if !strings.Contains(out, "unavailable") {
		t.Fatalf("doctor must refuse with the ErrUnavailable reason:\n%s", out)
	}
	if got := dirNames(t, dir); len(got) != 0 {
		t.Fatalf("doctor wrote into an empty directory: %v", got)
	}
}

func TestDoctorWarnsOnASyncedFolder(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	synced := filepath.Join(dir, "OneDrive", "Eco")
	if err := os.MkdirAll(synced, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	user := filepath.Join(synced, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	out, code := run(t, binary, "doctor", "--user-db", user, "--project-db", project)
	if code != 0 {
		t.Fatalf("doctor exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "OneDrive") {
		t.Fatalf("doctor must warn about a synced folder:\n%s", out)
	}
}

func TestHelpListsOnlyDoctor(t *testing.T) {
	binary := buildEco(t)
	out, code := run(t, binary, "--help")
	if code != 0 {
		t.Fatalf("--help exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "doctor") {
		t.Fatalf("--help must list doctor:\n%s", out)
	}
	for _, forbidden := range []string{"mcp", "serve", "read", "append", "search", "promote"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Fatalf("--help mentions %q, and this card ships no verbs:\n%s", forbidden, out)
		}
	}
}

func TestUnknownVerbIsRejected(t *testing.T) {
	binary := buildEco(t)
	_, code := run(t, binary, "mcp")
	if code == 0 {
		t.Fatal("an unknown verb exited 0")
	}
}