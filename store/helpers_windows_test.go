package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func makeJunction(link, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	cmd.SysProcAttr = hiddenProcAttr()
	return cmd.Run()
}

func errorsIs(err, target error) bool {
	return errors.Is(err, target)
}

func dirEntriesOf(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out, nil
}

func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return abs, nil
}