//go:build !windows

package httpdoor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func restrictFileToOwner(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("httpdoor: the port file could not be made owner only: %w", err)
	}
	return nil
}

func PortFileACL(path string) ([]string, error) {
	out, err := exec.Command("stat", "-c", "%a", path).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("httpdoor: %w", err)
	}
	mode := strings.TrimSpace(string(out))
	if mode == "" {
		return nil, fmt.Errorf("httpdoor: stat returned no mode for the port file")
	}
	return []string{mode}, nil
}
