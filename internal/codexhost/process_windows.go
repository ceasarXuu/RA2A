//go:build windows

package codexhost

import (
	"os/exec"
	"strconv"
	"syscall"
)

func configureManagedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func terminateManagedProcess(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	return killManagedPID(command.Process.Pid)
}

func killManagedPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	return exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}
