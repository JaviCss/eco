package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionVerbPrintsTheDevelStampWithoutBuildInfo(t *testing.T) {
	if !strings.HasPrefix(versionLine(), "eco ") {
		t.Fatalf("the version line must start with the binary name, got %q", versionLine())
	}
	version, revision, _ := buildStamp()
	if version == "" {
		t.Fatal("the version must never be empty")
	}
	if revision == "" && !strings.Contains(versionLine(), "(") {
		return
	}
	if revision != "" && !strings.Contains(versionLine(), revision) {
		t.Fatalf("the version line must carry the revision %q, got %q", revision, versionLine())
	}
}

func TestRevisionLineNamesTheAbsenceOfARevision(t *testing.T) {
	if got := revisionLine(""); got != "(none)" {
		t.Fatalf("an empty revision must print %q, got %q", "(none)", got)
	}
	if got := revisionLine("aa0141f52b0d"); got != "aa0141f52b0d" {
		t.Fatalf("a real revision must be printed as it is, got %q", got)
	}
}

func TestVersionRejectsArguments(t *testing.T) {
	binary := buildEco(t)
	out, code := run(t, binary, "version", "extra")
	if code != exitUsage {
		t.Fatalf("eco version extra exited %d, want %d:\n%s", code, exitUsage, out)
	}
	if !strings.Contains(out, "unexpected argument") {
		t.Fatalf("the refusal must name the extra argument:\n%s", out)
	}
}

func TestVersionFlagAndVerbSayTheSameThing(t *testing.T) {
	binary := buildEco(t)
	long, longCode := run(t, binary, "--version")
	short, shortCode := run(t, binary, "version")
	if longCode != 0 || shortCode != 0 {
		t.Fatalf("--version exited %d and version exited %d, want 0:\n%s", longCode, shortCode, long)
	}
	if strings.TrimSpace(long) != strings.TrimSpace(short) {
		t.Fatalf("--version says %q and version says %q, want the same line", strings.TrimSpace(long), strings.TrimSpace(short))
	}
	if !strings.HasPrefix(strings.TrimSpace(long), "eco ") {
		t.Fatalf("the version output must name the binary, got %q", long)
	}
}

func TestDoctorCarriesTheVersionAndTheRevision(t *testing.T) {
	binary := buildEco(t)
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	out, code := run(t, binary, "doctor", "--user-db", user, "--project-db", project)
	if code != 0 {
		t.Fatalf("doctor exited %d:\n%s", code, out)
	}
	stamp, stampCode := run(t, binary, "--version")
	if stampCode != 0 {
		t.Fatalf("--version exited %d:\n%s", stampCode, stamp)
	}
	fields := strings.Fields(strings.TrimSpace(stamp))
	if len(fields) < 2 || fields[0] != "eco" {
		t.Fatalf("--version must say 'eco <version>', got %q", stamp)
	}
	if !strings.Contains(out, "version: "+fields[0]+" "+fields[1]) {
		t.Fatalf("doctor must report the version %q, got:\n%s", fields[1], out)
	}
	if !strings.Contains(out, "revision:") {
		t.Fatalf("doctor must report a revision field, got:\n%s", out)
	}
}