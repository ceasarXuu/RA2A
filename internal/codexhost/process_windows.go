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

// reapManagedGroup mirrors the Unix contract: reaped=true means surviving tree
// members were terminated. taskkill cannot distinguish "no such process" from a
// real failure, so a failed taskkill is reported as nothing-to-reap to avoid
// misleading reap_failed logs on the common already-gone case.
func reapManagedGroup(command *exec.Cmd) (bool, error) {
	if command == nil || command.Process == nil {
		return false, nil
	}
	if err := killManagedPID(command.Process.Pid); err != nil {
		return false, nil
	}
	return true, nil
}
