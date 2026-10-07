//go:build windows

package main

import (
	"context"
	"syscall"
)

var (
	kernel32DLL       = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess   = kernel32DLL.NewProc("OpenProcess")
	procWaitForObject = kernel32DLL.NewProc("WaitForSingleObject")
	procCloseHandle   = kernel32DLL.NewProc("CloseHandle")
)

const (
	synchronizeAccess = 0x00100000
	waitInfinite      = 0xFFFFFFFF
)

func waitForParent(pid int, cancel context.CancelFunc) {
	handle, _, _ := procOpenProcess.Call(synchronizeAccess, 0, uintptr(pid))
	if handle == 0 {
		cancel()
		return
	}
	defer procCloseHandle.Call(handle)
	status, _, _ := procWaitForObject.Call(handle, waitInfinite)
	if status != 0 {
		cancel()
		return
	}
	cancel()
}