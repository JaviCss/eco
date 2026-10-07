package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var include = []string{"cmd", "httpdoor", "mcpdoor", "port", "store"}

var includeFiles = []string{"go.mod", "go.sum", "README.md"}

var skipDirs = map[string]bool{
	"evidencia": true,
	"poc":       true,
	"dist":      true,
	"npm":       true,
	"tools":     true,
	".git":      true,
}

func escapePath(module string) string {
	var out strings.Builder
	for _, r := range module {
		if r >= 'A' && r <= 'Z' {
			out.WriteRune('!')
			out.WriteRune(r + ('a' - 'A'))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func moduleOf(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("goproxy: go.mod: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", errors.New("goproxy: go.mod carries no module line")
}

func hasReplace(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "replace ") || strings.HasPrefix(strings.TrimSpace(line), "=>") {
			return true
		}
	}
	return false
}

func collected(root string) ([]string, error) {
	out := make([]string, 0, 32)
	for _, name := range includeFiles {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			out = append(out, name)
		}
	}
	for _, dir := range include {
		full := filepath.Join(root, dir)
		if _, err := os.Stat(full); err != nil {
			continue
		}
		err := filepath.Walk(full, func(current string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if skipDirs[info.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if skipDirs[info.Name()] || !strings.HasSuffix(info.Name(), ".go") {
				return nil
			}
			rel, err := filepath.Rel(root, current)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("goproxy: walking %s: %w", dir, err)
		}
	}
	sort.Strings(out)
	return out, nil
}

func writeZip(root, destination, module, version string, files []string) error {
	out, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("goproxy: %s: %w", destination, err)
	}
	defer out.Close()
	writer := zip.NewWriter(out)
	prefix := module + "@" + version + "/"
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			writer.Close()
			return fmt.Errorf("goproxy: %s: %w", name, err)
		}
		entry, err := writer.Create(prefix + path.Clean(name))
		if err != nil {
			writer.Close()
			return fmt.Errorf("goproxy: %s: %w", name, err)
		}
		if _, err := io.Copy(entry, bytes.NewReader(raw)); err != nil {
			writer.Close()
			return fmt.Errorf("goproxy: %s: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("goproxy: %s: %w", destination, err)
	}
	return nil
}

func tagTime(root, tag string) (time.Time, error) {
	cmd := exec.Command("git", "log", "-1", "--format=%cI", tag)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return time.Time{}, fmt.Errorf("goproxy: the tag %s is unknown: %w", tag, err)
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(out.String()))
	if err != nil {
		return time.Time{}, fmt.Errorf("goproxy: the time of %s is unreadable: %w", tag, err)
	}
	return parsed.UTC(), nil
}

func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("goproxy: %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("goproxy: %s: %w", path, err)
	}
	return nil
}

func publish(root, destination, tag string) error {
	module, err := moduleOf(root)
	if err != nil {
		return err
	}
	if hasReplace(root) {
		return errors.New("goproxy: go.mod carries a replace directive: a published module with replace is not installable (C2)")
	}
	version := tag
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if strings.TrimSpace(strings.TrimPrefix(version, "v")) == "" {
		return fmt.Errorf("goproxy: %q is not a version tag", tag)
	}
	stamp, err := tagTime(root, tag)
	if err != nil {
		stamp = time.Now().UTC()
	}
	files, err := collected(root)
	if err != nil {
		return err
	}
	dir := filepath.Join(destination, filepath.FromSlash(escapePath(module)), "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("goproxy: %s: %w", dir, err)
	}
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("goproxy: go.mod: %w", err)
	}
	if err := writeFile(filepath.Join(dir, version+".mod"), string(goMod)); err != nil {
		return err
	}
	info, err := json.Marshal(struct {
		Version string
		Time    time.Time
	}{Version: version, Time: stamp})
	if err != nil {
		return fmt.Errorf("goproxy: the .info of %s: %w", tag, err)
	}
	if err := writeFile(filepath.Join(dir, version+".info"), string(info)); err != nil {
		return err
	}
	if err := writeZip(root, filepath.Join(dir, version+".zip"), module, version, files); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "list"), version+"\n"); err != nil {
		return err
	}
	fmt.Printf("goproxy: %s published as %s@%s (%d files)\n", module, module, version, len(files))
	return nil
}

func run() error {
	flags := flag.NewFlagSet("goproxy", flag.ContinueOnError)
	root := flags.String("repo", ".", "directory that holds the module")
	destination := flags.String("out", "", "directory that receives the proxy tree")
	tag := flags.String("tag", "", "tag to publish, v0.1.0 for instance")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("goproxy: unexpected argument %q", flags.Arg(0))
	}
	if strings.TrimSpace(*tag) == "" {
		return errors.New("goproxy: --tag is required")
	}
	if strings.TrimSpace(*destination) == "" {
		return errors.New("goproxy: --out is required")
	}
	if err := os.MkdirAll(*destination, 0o755); err != nil {
		return fmt.Errorf("goproxy: %s: %w", *destination, err)
	}
	return publish(*root, *destination, *tag)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}