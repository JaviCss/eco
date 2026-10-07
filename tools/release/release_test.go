package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func moduleRootForTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve the module root: %v", err)
	}
	return root
}

func TestPlatformManifestCarriesTheTripleAndNoScripts(t *testing.T) {
	root := moduleRootForTest(t)
	for _, p := range platforms {
		files, err := renderPlatformPackage(root, "0.1.0", "MIT", p)
		if err != nil {
			t.Fatalf("render %s: %v", p.Name, err)
		}
		var manifest struct {
			Name     string            `json:"name"`
			Version  string            `json:"version"`
			OS       []string          `json:"os"`
			CPU      []string          `json:"cpu"`
			Bin      map[string]string `json:"bin"`
			Files    []string          `json:"files"`
			License  string            `json:"license"`
			Scripts  map[string]any    `json:"scripts"`
			Optional map[string]any    `json:"optionalDependencies"`
		}
		if err := json.Unmarshal(files["package.json"], &manifest); err != nil {
			t.Fatalf("the manifest of %s is not JSON: %v\n%s", p.Name, err, files["package.json"])
		}
		if manifest.Name != "@"+scope+"/"+p.Name {
			t.Fatalf("the manifest of %s is named %q, want %q", p.Name, manifest.Name, "@"+scope+"/"+p.Name)
		}
		if manifest.Version != "0.1.0" {
			t.Fatalf("the manifest of %s carries version %q", p.Name, manifest.Version)
		}
		if len(manifest.OS) != 1 || manifest.OS[0] != p.NodeOS {
			t.Fatalf("the manifest of %s declares os %v, want [%s]: npm compares os against process.platform (%s)", p.Name, manifest.OS, p.NodeOS, p.NodeOS)
		}
		if len(manifest.CPU) != 1 || manifest.CPU[0] != p.NodeArch {
			t.Fatalf("the manifest of %s declares cpu %v, want [%s]: npm compares cpu against process.arch (%s)", p.Name, manifest.CPU, p.NodeArch, p.NodeArch)
		}
		if len(manifest.Bin) != 1 || manifest.Bin[p.BinName] != "bin/"+p.Binary {
			t.Fatalf("the manifest of %s declares bin %v", p.Name, manifest.Bin)
		}
		if len(manifest.Files) != 2 {
			t.Fatalf("the manifest of %s must close files to two entries, got %v", p.Name, manifest.Files)
		}
		if manifest.Scripts != nil {
			t.Fatalf("the manifest of %s declares scripts %v, want none", p.Name, manifest.Scripts)
		}
		if manifest.Optional != nil {
			t.Fatalf("a platform package must declare no optional dependencies, got %v", manifest.Optional)
		}
		if manifest.License != "MIT" {
			t.Fatalf("the manifest of %s carries license %q, want MIT", p.Name, manifest.License)
		}
		if _, ok := files[filepath.Join("bin", p.Binary)]; !ok {
			t.Fatalf("the manifest of %s does not declare where the binary goes", p.Name)
		}
	}
}

func TestMainManifestPinsTheOptionalsByExactVersion(t *testing.T) {
	root := moduleRootForTest(t)
	files, err := renderMainPackage(root, "0.1.0", "", "github.com/JaviCss/eco")
	if err != nil {
		t.Fatalf("render the main package: %v", err)
	}
	var manifest struct {
		Name     string            `json:"name"`
		Version  string            `json:"version"`
		Bin      map[string]string `json:"bin"`
		Files    []string          `json:"files"`
		Engines  map[string]string `json:"engines"`
		Scripts  map[string]any    `json:"scripts"`
		License  string            `json:"license"`
		Optional map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal(files["package.json"], &manifest); err != nil {
		t.Fatalf("the main manifest is not JSON: %v\n%s", err, files["package.json"])
	}
	if manifest.Name != "@"+scope+"/eco" {
		t.Fatalf("the main manifest is named %q, want %q", manifest.Name, "@"+scope+"/eco")
	}
	if manifest.Bin["eco"] != "bin/eco.js" {
		t.Fatalf("the main manifest declares bin %v, want eco -> bin/eco.js", manifest.Bin)
	}
	if manifest.Engines["node"] != ">= 20" {
		t.Fatalf("the main manifest declares engines %v, want node >= 20", manifest.Engines)
	}
	if manifest.Scripts != nil {
		t.Fatalf("the main manifest declares scripts %v, want none", manifest.Scripts)
	}
	if manifest.License != "" {
		t.Fatalf("with no LICENSE in the repo the license field must be absent, got %q", manifest.License)
	}
	if len(manifest.Optional) != len(platforms) {
		t.Fatalf("the main manifest declares %d optional dependencies, want %d: %v", len(manifest.Optional), len(platforms), manifest.Optional)
	}
	for _, p := range platforms {
		got, ok := manifest.Optional["@"+scope+"/"+p.Name]
		if !ok {
			t.Fatalf("the optional %s is missing: %v", p.Name, manifest.Optional)
		}
		if got != "0.1.0" {
			t.Fatalf("the optional %s is pinned to %q, want the exact version 0.1.0 (a file: spec breaks on unpack)", p.Name, got)
		}
		if strings.HasPrefix(got, "file:") {
			t.Fatalf("the optional %s uses a file: spec: %q", p.Name, got)
		}
	}
	launcher := string(files[filepath.Join("bin", "eco.js")])
	if !strings.HasPrefix(launcher, "#!/usr/bin/env node") {
		t.Fatal("the launcher must carry a node shebang so npm can link it")
	}
	for _, forbidden := range []string{"require('express", "require(\"express", "from 'node:child_process'"} {
		if forbidden == "from 'node:child_process'" && strings.Contains(launcher, forbidden) {
			t.Fatal("the launcher must not use an ESM import")
		}
	}
	if !strings.Contains(launcher, "require('node:child_process')") {
		t.Fatal("the launcher must spawn the platform binary with node:child_process")
	}
	if strings.Contains(launcher, "require.resolve(\"${PACKAGE}/${BINARY}\"") {
		t.Fatal("the launcher mixes quote styles in its resolver call")
	}
}

func TestChecksumsCoverEveryBinaryAndVerify(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"windows-amd64": "windows binary bytes",
		"linux-amd64":   "linux binary bytes",
		"darwin-arm64":  "darwin binary bytes",
	}
	var paths []string
	for name, body := range files {
		path := filepath.Join(dir, name, "eco")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		paths = append(paths, path)
	}
	if err := writeChecksums(dir, sortedValues(map[string]string{
		"a": paths[0], "b": paths[1], "c": paths[2],
	})); err != nil {
		t.Fatalf("writeChecksums: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		t.Fatalf("read SHA256SUMS: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != len(paths) {
		t.Fatalf("SHA256SUMS has %d lines, want %d:\n%s", len(lines), len(paths), raw)
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("a line of SHA256SUMS is malformed: %q", line)
		}
		want, err := checksum(filepath.Join(dir, filepath.FromSlash(fields[1])))
		if err != nil {
			t.Fatalf("the entry %q names a file that is not there: %v", fields[1], err)
		}
		if want != fields[0] {
			t.Fatalf("the hash of %s is %s, SHA256SUMS says %s", fields[1], want, fields[0])
		}
	}
}

func TestPlatformNamesAndManifestsUseTheNodeValuesNotTheGoOnes(t *testing.T) {
	root := moduleRootForTest(t)
	files, err := renderMainPackage(root, "0.1.0", "", "github.com/JaviCss/eco")
	if err != nil {
		t.Fatalf("render the main package: %v", err)
	}
	launcher := string(files[filepath.Join("bin", "eco.js")])
	if !strings.Contains(launcher, "process.platform") || !strings.Contains(launcher, "process.arch") {
		t.Fatalf("the launcher must resolve the platform package by process.platform/process.arch:\n%s", launcher)
	}
	for _, p := range platforms {
		want := "@" + scope + "/" + mainPackageName + "-" + p.NodeOS + "-" + p.NodeArch
		if p.Name != strings.TrimPrefix(want, "@"+scope+"/") {
			t.Fatalf("the platform package is named %q, want %q: npm compares os/cpu against process.platform (%s) and process.arch (%s), not against the Go names", p.Name, p.Name, p.NodeOS, p.NodeArch)
		}
		if p.Name == mainPackageName+"-"+p.GoOS+"-"+p.GoArch && p.GoOS != p.NodeOS {
			t.Fatalf("the platform package %q is named with the Go OS %q, but npm will look for %q", p.Name, p.GoOS, p.NodeOS)
		}
	}
	raw, err := renderTemplate(root, "platform.json", platformPackage{
		Scope: scope, Name: platforms[0].Name, Version: "0.1.0",
		OS: platforms[0].NodeOS, Arch: platforms[0].NodeArch,
		Binary: platforms[0].Binary, BinName: platforms[0].BinName,
	})
	if err != nil {
		t.Fatalf("render the platform manifest: %v", err)
	}
	if strings.Contains(string(raw), `"os": [
    "windows"`) || strings.Contains(string(raw), `"amd64"`) {
		t.Fatalf("the platform manifest carries Go names where npm compares Node names:\n%s", raw)
	}
}

func TestNpmVersionStripsTheVAndRejectsAnEmptyTag(t *testing.T) {
	if got, err := npmVersion("v0.1.0"); err != nil || got != "0.1.0" {
		t.Fatalf("npmVersion(v0.1.0) = %q, %v; want 0.1.0", got, err)
	}
	if _, err := npmVersion("v"); err == nil {
		t.Fatal("npmVersion(v) reported success")
	}
}

func TestExactTagNamesTheReasonWhenHeadCarriesNoTag(t *testing.T) {
	root := moduleRootForTest(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the module root of the test is wrong: %v", err)
	}
	tag, err := exactTag(root)
	if err != nil {
		if !strings.Contains(err.Error(), "no exact tag") {
			t.Fatalf("the refusal must name the reason, got %v", err)
		}
		return
	}
	if tag != "v0.1.0" {
		t.Fatalf("HEAD carries the tag %q, want v0.1.0", tag)
	}
}
