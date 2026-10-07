package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

type mainPackage struct {
	Scope     string
	Version   string
	Module    string
	Platforms []platform
	Optionals []optional
}

type optional struct {
	Name    string
	Version string
}

type platformPackage struct {
	Scope   string
	Name    string
	Version string
	OS      string
	Arch    string
	Binary  string
	BinName string
}

const binaryName = "eco"

func binaryFor(osName string) string {
	if osName == "windows" {
		return binaryName + ".exe"
	}
	return binaryName
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("release: the working directory is unreadable: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("release: no go.mod above the working directory: run it from the module root")
		}
		dir = parent
	}
}

func exactTag(root string) (string, error) {
	cmd := exec.Command("git", "describe", "--tags", "--exact-match")
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("release: the module carries no exact tag: %v: %s", err, strings.TrimSpace(out.String()))
	}
	tag := strings.TrimSpace(out.String())
	if tag == "" {
		return "", errors.New("release: git describe --tags --exact-match returned nothing")
	}
	return tag, nil
}

func npmVersion(tag string) (string, error) {
	version := strings.TrimPrefix(tag, "v")
	if version == "" {
		return "", fmt.Errorf("release: %q is not a version tag", tag)
	}
	return version, nil
}

type buildTarget struct {
	OS   string
	Arch string
}

func buildTargets() []buildTarget {
	out := make([]buildTarget, 0, len(platforms))
	for _, p := range platforms {
		out = append(out, buildTarget{OS: p.GoOS, Arch: p.GoArch})
	}
	return out
}

func sortedValues(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func buildBinary(root, out, version, goos, goarch string) (string, error) {
	dir := filepath.Join(out, goos+"-"+goarch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("release: %s: %w", dir, err)
	}
	binary := filepath.Join(dir, binaryFor(goos))
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binary, "./cmd/eco")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("release: go build for %s/%s: %v: %s", goos, goarch, err, log.String())
	}
	return binary, nil
}

func checksum(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func writeChecksums(out string, binaries []string) error {
	lines := make([]string, 0, len(binaries))
	for _, binary := range binaries {
		sum, err := checksum(binary)
		if err != nil {
			return fmt.Errorf("release: %s: %w", binary, err)
		}
		rel, err := filepath.Rel(out, binary)
		if err != nil {
			return fmt.Errorf("release: %s: %w", binary, err)
		}
		lines = append(lines, sum+"  "+filepath.ToSlash(rel))
	}
	sort.Strings(lines)
	path := filepath.Join(out, "SHA256SUMS")
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("release: %s: %w", path, err)
	}
	return nil
}

func renderTemplate(root, name string, data any) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(root, "tools", "release", "templates", name))
	if err != nil {
		return nil, fmt.Errorf("release: template %s: %w", name, err)
	}
	tpl, err := template.New(name).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("release: template %s: %w", name, err)
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("release: template %s: %w", name, err)
	}
	return out.Bytes(), nil
}

func writeFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("release: %s: %w", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("release: %s: %w", path, err)
	}
	return nil
}

func licenseField(root string) string {
	for _, name := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		head := strings.ToLower(string(raw))
		switch {
		case strings.Contains(head, "permission is hereby granted, free of charge"):
			return "MIT"
		case strings.Contains(head, "apache license"):
			return "Apache-2.0"
		case strings.Contains(head, "gnu general public license"):
			return "GPL-3.0"
		case strings.Contains(head, "redistribution and use in source and binary forms"):
			return "BSD-3-Clause"
		}
		return "SEE LICENSE IN LICENSE"
	}
	return ""
}

func renderPlatformPackage(root, version, license string, p platform) (map[string][]byte, error) {
	data := platformPackage{
		Scope:   scope,
		Name:    p.Name,
		Version: version,
		OS:      p.NodeOS,
		Arch:    p.NodeArch,
		Binary:  p.Binary,
		BinName: p.BinName,
	}
	manifest, err := renderTemplate(root, "platform.json", data)
	if err != nil {
		return nil, err
	}
	if license != "" {
		manifest = append(manifest[:len(manifest)-1], []byte(",\n  \"license\": \""+license+"\"\n}\n")...)
	}
	readme, err := renderTemplate(root, "platform-README.md", data)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		filepath.Join("package.json"):  manifest,
		filepath.Join("README.md"):     readme,
		filepath.Join("bin", p.Binary): nil,
	}, nil
}

func optionalsFor(version string) []optional {
	out := make([]optional, 0, len(platforms))
	for _, p := range platforms {
		out = append(out, optional{Name: "@" + scope + "/" + p.Name, Version: version})
	}
	return out
}

func renderMainPackage(root, version, license, module string) (map[string][]byte, error) {
	data := mainPackage{
		Scope:     scope,
		Version:   version,
		Module:    module,
		Platforms: platforms,
		Optionals: optionalsFor(version),
	}
	manifest, err := renderTemplate(root, "package.json", data)
	if err != nil {
		return nil, err
	}
	if license != "" {
		manifest = append(manifest[:len(manifest)-1], []byte(",\n  \"license\": \""+license+"\"\n}\n")...)
	}
	readme, err := renderTemplate(root, "README.md", data)
	if err != nil {
		return nil, err
	}
	launcher, err := renderTemplate(root, "eco.js", data)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		filepath.Join("package.json"):  manifest,
		filepath.Join("README.md"):     readme,
		filepath.Join("bin", "eco.js"): launcher,
	}, nil
}

func modulePath(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("release: go.mod: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", errors.New("release: go.mod carries no module line")
}

func pack(dir, destination string) (string, error) {
	cmd := exec.Command("npm", "pack", "--pack-destination", destination)
	cmd.Dir = dir
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("release: npm pack in %s: %v: %s", dir, err, log.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".tgz") {
			return filepath.Join(destination, filepath.FromSlash(line)), nil
		}
	}
	return "", fmt.Errorf("release: npm pack in %s printed no tarball: %s", dir, log.String())
}

func run() error {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	versionFlag := flags.String("version", "", "version to build; without it, the exact tag of HEAD")
	outFlag := flags.String("out", "dist", "directory that receives the binaries, the checksums and the packages")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("release: unexpected argument %q", flags.Arg(0))
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	tag := *versionFlag
	if tag == "" {
		tag, err = exactTag(root)
		if err != nil {
			return err
		}
	}
	version, err := npmVersion(tag)
	if err != nil {
		return err
	}
	module, err := modulePath(root)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(*outFlag)
	if err != nil {
		return fmt.Errorf("release: --out %s: %w", *outFlag, err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("release: %s: %w", out, err)
	}
	license := licenseField(root)

	binaries := make(map[string]string, len(platforms))
	for _, target := range buildTargets() {
		binary, err := buildBinary(root, out, tag, target.OS, target.Arch)
		if err != nil {
			return err
		}
		binaries[target.OS+"-"+target.Arch] = binary
	}
	if err := writeChecksums(out, sortedValues(binaries)); err != nil {
		return err
	}

	npmRoot := filepath.Join(out, "npm")
	if err := os.MkdirAll(npmRoot, 0o755); err != nil {
		return fmt.Errorf("release: %s: %w", npmRoot, err)
	}
	for _, p := range platforms {
		files, err := renderPlatformPackage(root, version, license, p)
		if err != nil {
			return err
		}
		dir := filepath.Join(npmRoot, p.Name)
		for name, raw := range files {
			if raw == nil {
				continue
			}
			if err := writeFile(filepath.Join(dir, name), raw); err != nil {
				return err
			}
		}
		raw, err := os.ReadFile(binaries[p.GoOS+"-"+p.GoArch])
		if err != nil {
			return fmt.Errorf("release: %s: %w", binaries[p.GoOS+"-"+p.GoArch], err)
		}
		if err := writeFile(filepath.Join(dir, "bin", p.Binary), raw); err != nil {
			return err
		}
		if _, err := pack(dir, npmRoot); err != nil {
			return err
		}
	}

	mainFiles, err := renderMainPackage(root, version, license, module)
	if err != nil {
		return err
	}
	mainDir := filepath.Join(npmRoot, mainPackageName)
	for name, raw := range mainFiles {
		if err := writeFile(filepath.Join(mainDir, name), raw); err != nil {
			return err
		}
	}
	if _, err := pack(mainDir, npmRoot); err != nil {
		return err
	}
	fmt.Printf("release: %s built into %s and packed into %s\n", tag, out, npmRoot)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
