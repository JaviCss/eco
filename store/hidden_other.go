//go:build !windows

package store

import "syscall"

func hiddenProcAttr() *syscall.SysProcAttr {
	return nil
}