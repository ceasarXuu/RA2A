//go:build !windows

package ocsession

import (
	"os"
	"syscall"
)

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	return err == nil && process.Signal(syscall.Signal(0)) == nil
}
