//go:build !windows

package main

import (
	"context"
	"os"
	"time"
)

func waitForParent(pid int, cancel context.CancelFunc) {
	process, err := os.FindProcess(pid)
	if err != nil {
		cancel()
		return
	}
	for {
		if err := process.Signal(os.Signal(nil)); err != nil {
			cancel()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}