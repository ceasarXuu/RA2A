//go:build windows

package ochost

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func configureServerCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func serverCommand(executable string, args ...string) *exec.Cmd {
	if strings.EqualFold(filepath.Ext(executable), ".cmd") || strings.EqualFold(filepath.Ext(executable), ".bat") {
		return exec.Command("cmd.exe", append([]string{"/d", "/c", executable}, args...)...)
	}
	return exec.Command(executable, args...)
}

func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	return syscall.GetExitCodeProcess(handle, &code) == nil && code == 259 // STILL_ACTIVE
}

func stopSharedProcess(pid int) error {
	// A .cmd shim starts the native OpenCode server as a child of cmd.exe.
	// Terminating only the wrapper PID leaves the listening server orphaned.
	if err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run(); err == nil || !processAlive(pid) {
		return nil
	} else {
		return fmt.Errorf("terminate OpenCode process tree %d: %w", pid, err)
	}
}
