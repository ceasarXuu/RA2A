//go:build !windows

package codexhost

import (
	"os/exec"
	"syscall"
)

func configureManagedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateManagedProcess(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return command.Process.Kill()
}

func killManagedPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
