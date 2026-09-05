//go:build !windows

package codexhost

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

// reapManagedGroup terminates residual members left in the process group of an
// already-exited managed leader (for example the codex child of a killed node
// wrapper on Linux). It returns reaped=true only when living members were
// actually terminated. Zombie members (dead but not yet reaped by their new
// parent) do not count, so a group already terminated by Close reports
// reaped=false without a misleading log. A kill failure returns the error so
// the caller can decide whether the group may still be alive.
func reapManagedGroup(command *exec.Cmd) (bool, error) {
	if command == nil || command.Process == nil {
		return false, nil
	}
	pid := command.Process.Pid
	if !processGroupHasLivingMember(pid) {
		return false, nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return false, err
	}
	return true, nil
}

// processGroupHasLivingMember reports whether the process group pgid contains
// any member whose state is not zombie.
func processGroupHasLivingMember(pgid int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		index := strings.Index(string(stat), ")")
		if index < 0 {
			continue
		}
		fields := strings.Fields(string(stat)[index+1:])
		if len(fields) < 3 {
			continue
		}
		pgrp, err := strconv.Atoi(fields[2])
		if err != nil || pgrp != pgid {
			continue
		}
		if fields[0] != "Z" {
			return true
		}
	}
	return false
}
