//go:build windows

package ochost

import (
	"os/exec"
	"syscall"
)

func configureServerCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
