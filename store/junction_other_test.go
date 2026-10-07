//go:build !windows

package store

import "os"

func makeJunction(link, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	return os.Symlink(target, link)
}