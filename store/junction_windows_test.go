//go:build windows

package store

import (
	"os"
	"os/exec"
)

func makeJunction(link, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	cmd.SysProcAttr = hiddenProcAttr()
	return cmd.Run()
}