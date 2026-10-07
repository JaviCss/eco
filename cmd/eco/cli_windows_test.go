//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestServeDiesWithItsParent(t *testing.T) {
	if os.Getenv("ECO_SERVE_PARENT") == "1" {
		serveParentHelper(t)
		return
	}
	binary := buildEco(t)
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain: %v", err)
	}
	dir := t.TempDir()
	self := filepath.Join(dir, "eco.test.exe")
	build := exec.Command(tool, "test", "-c", "-o", self, ".")
	build.Env = append(build.Environ(), "GOFLAGS=-mod=mod")
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build the helper binary: %v\n%s", buildErr, out)
	}
	portFile := filepath.Join(dir, "eco.port")
	pidFile := portFile + ".pid"
	helper := exec.Command(self, "-test.run=TestServeDiesWithItsParent", "-test.v")
	helper.Env = append(helper.Environ(),
		"ECO_SERVE_PARENT=1",
		"ECO_BINARY="+binary,
		"ECO_PORT_FILE="+portFile,
		"ECO_PID_FILE="+pidFile,
	)
	out, err := helper.CombinedOutput()
	t.Logf("helper output:\n%s", out)
	if err != nil {
		t.Fatalf("the parent helper exited %v", err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the helper did not record the pid of eco serve: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("the pid file holds %q: %v", raw, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var gone, fileGone bool
	for time.Now().Before(deadline) {
		gone = processGone(pid)
		if _, err := os.Stat(portFile); os.IsNotExist(err) {
			fileGone = true
		}
		if gone && fileGone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !fileGone {
		t.Fatalf("the port file is still there %s after the parent died", 2*time.Second)
	}
	if !gone {
		t.Fatalf("eco serve (pid %d) is still alive %s after its parent died", pid, 2*time.Second)
	}
}

func serveParentHelper(t *testing.T) {
	binary := os.Getenv("ECO_BINARY")
	portFile := os.Getenv("ECO_PORT_FILE")
	pidFile := os.Getenv("ECO_PID_FILE")
	dir := t.TempDir()
	user := filepath.Join(dir, "user.db")
	project := filepath.Join(dir, "project.db")
	seedBases(t, user, project)
	server := exec.Command(binary, "serve",
		"--user-db", user, "--project-db", project,
		"--parent-pid", strconv.Itoa(os.Getpid()), "--port-file", portFile)
	if err := server.Start(); err != nil {
		t.Fatalf("the helper cannot start eco serve: %v", err)
	}
	waitForFile(t, portFile, 15*time.Second)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(server.Process.Pid)), 0o600); err != nil {
		t.Fatalf("the helper cannot record the pid: %v", err)
	}
	t.Logf("helper launched eco serve as pid %d and now dies", server.Process.Pid)
	os.Exit(0)
}

func processGone(pid int) bool {
	handle, _, _ := procOpenProcess.Call(synchronizeAccess, 0, uintptr(pid))
	if handle == 0 {
		return true
	}
	defer procCloseHandle.Call(handle)
	status, _, _ := procWaitForObject.Call(handle, 0)
	return status == 0
}