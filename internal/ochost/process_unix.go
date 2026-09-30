//go:build !windows

package ochost

import (
	"os/exec"
	"syscall"
)

func configureServerCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
