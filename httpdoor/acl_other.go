//go:build !windows

package httpdoor

import (
	"fmt"
	"os"
)

func createPrivateTemp(dir, prefix, suffix string) (*os.File, string, error) {
	file, err := os.CreateTemp(dir, prefix+"*"+suffix)
	if err != nil {
		return nil, "", fmt.Errorf("httpdoor: %w", err)
	}
	name := file.Name()
	if err := restrictFileToOwner(name); err != nil {
		file.Close()
		os.Remove(name)
		return nil, "", err
	}
	return file, name, nil
}

func restrictFileToOwner(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("httpdoor: the port file could not be made owner only: %w", err)
	}
	return nil
}

func PortFileACL(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("httpdoor: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("httpdoor: %s is not a regular file", path)
	}
	return []string{fmt.Sprintf("%04o", info.Mode().Perm())}, nil
}