package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEscapePathLowercasesTheCapitalsWithABang(t *testing.T) {
	got := escapePath("github.com/JaviCss/eco")
	if got != "github.com/!javi!css/eco" {
		t.Fatalf("escapePath(github.com/JaviCss/eco) = %q, want github.com/!javi!css/eco", got)
	}
}

func TestCollectedLeavesOutEverythingThatIsNotTheModule(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve the module root: %v", err)
	}
	files, err := collected(root)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("collect returned no file at all")
	}
	var withGoMod bool
	for _, name := range files {
		switch {
		case name == "go.mod":
			withGoMod = true
		case strings.HasPrefix(name, "tools/"):
			t.Fatalf("the module zip must not carry %s", name)
		case strings.HasPrefix(name, "poc/"), strings.HasPrefix(name, "evidencia/"),
			strings.HasPrefix(name, "dist/"), strings.HasPrefix(name, "npm/"):
			t.Fatalf("the module zip must not carry %s", name)
		case !strings.HasSuffix(name, ".go") && name != "go.sum" && name != "README.md":
			t.Fatalf("the module zip carries an unexpected file: %s", name)
		}
	}
	if !withGoMod {
		t.Fatal("the module zip must carry go.mod")
	}
}

func TestPublishWritesTheFourFilesAndTheZipPrefix(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve the module root: %v", err)
	}
	dir := t.TempDir()
	if err := publish(root, dir, "v0.1.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	module, err := moduleOf(root)
	if err != nil {
		t.Fatalf("moduleOf: %v", err)
	}
	at := filepath.Join(dir, filepath.FromSlash(escapePath(module)), "@v")
	for _, name := range []string{"list", "v0.1.0.info", "v0.1.0.mod", "v0.1.0.zip"} {
		if _, err := os.Stat(filepath.Join(at, name)); err != nil {
			t.Fatalf("the proxy has no %s: %v", name, err)
		}
	}
	list, err := os.ReadFile(filepath.Join(at, "list"))
	if err != nil {
		t.Fatalf("read list: %v", err)
	}
	if strings.TrimSpace(string(list)) != "v0.1.0" {
		t.Fatalf("list holds %q, want v0.1.0", list)
	}
	info, err := os.ReadFile(filepath.Join(at, "v0.1.0.info"))
	if err != nil {
		t.Fatalf("read the info: %v", err)
	}
	var stamp struct {
		Version string
		Time    string
	}
	if err := json.Unmarshal(info, &stamp); err != nil {
		t.Fatalf("the .info is not JSON: %v\n%s", err, info)
	}
	if stamp.Version != "v0.1.0" {
		t.Fatalf("the .info carries version %q", stamp.Version)
	}
	if stamp.Time == "" {
		t.Fatal("the .info carries no time")
	}
	gomod, err := os.ReadFile(filepath.Join(at, "v0.1.0.mod"))
	if err != nil {
		t.Fatalf("read the .mod: %v", err)
	}
	if !strings.HasPrefix(string(gomod), "module "+module) {
		t.Fatalf("the .mod does not open with the module line:\n%s", gomod)
	}
	if strings.Contains(string(gomod), "replace ") {
		t.Fatalf("the .mod carries a replace directive, which makes the module uninstallable:\n%s", gomod)
	}
	reader, err := zip.OpenReader(filepath.Join(at, "v0.1.0.zip"))
	if err != nil {
		t.Fatalf("open the zip: %v", err)
	}
	defer reader.Close()
	prefix := module + "@v0.1.0/"
	var sawGoMod bool
	for _, entry := range reader.File {
		if !strings.HasPrefix(entry.Name, prefix) {
			t.Fatalf("the zip entry %q is not under %q", entry.Name, prefix)
		}
		rest := strings.TrimPrefix(entry.Name, prefix)
		switch {
		case rest == "go.mod":
			sawGoMod = true
		case strings.HasPrefix(rest, "evidencia/"), strings.HasPrefix(rest, "poc/"),
			strings.HasPrefix(rest, "dist/"), strings.HasPrefix(rest, "npm/"), strings.HasPrefix(rest, "tools/"):
			t.Fatalf("the zip carries %s", rest)
		}
	}
	if !sawGoMod {
		t.Fatal("the zip carries no go.mod at the root of the module")
	}
}

func TestPublishRefusesATaglessRepoAndAReplacedModule(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve the module root: %v", err)
	}
	if err := publish(root, t.TempDir(), "v"); err == nil {
		t.Fatal("publish accepted a tag with no version in it")
	}
	if !hasReplace(root) && true {
		dir := t.TempDir()
		fake := filepath.Join(dir, "eco")
		if err := os.MkdirAll(fake, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := "module github.com/JaviCss/eco\n\ngo 1.26.0\n\nreplace github.com/x/y => ../../y\n"
		if err := os.WriteFile(filepath.Join(fake, "go.mod"), []byte(body), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		err := publish(fake, filepath.Join(dir, "out"), "v0.1.0")
		if err == nil {
			t.Fatal("publish accepted a module with a replace directive")
		}
		if !strings.Contains(err.Error(), "replace") {
			t.Fatalf("the refusal must name the replace directive, got %v", err)
		}
	}
}