//go:build !windows

package codexhost

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorReapsResidualProcessGroupAfterManagedLeaderExit(t *testing.T) {
	firstClient, firstServer := net.Pipe()
	secondClient, secondServer := net.Pipe()
	defer secondServer.Close()

	firstDone := make(chan struct{})
	restarted := make(chan struct{})
	startCalls := 0
	var leaderPid int
	start := func(context.Context, Config) (*managedProcess, error) {
		startCalls++
		if startCalls == 1 {
			command := exec.Command("sh", "-c", "sleep 60 & sleep 60")
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := command.Start(); err != nil {
				return nil, err
			}
			leaderPid = command.Process.Pid
			process := &managedProcess{command: command, done: firstDone}
			go func() {
				waitErr := command.Wait()
				finalizeManagedExit(command, nil, "", "", waitErr, nil, false)
				close(firstDone)
			}()
			return process, nil
		}
		close(restarted)
		return &managedProcess{done: make(chan struct{})}, nil
	}
	connectCalls := 0
	connect := func(context.Context, string) (io.ReadWriteCloser, error) {
		connectCalls++
		if connectCalls == 1 {
			return firstClient, nil
		}
		return secondClient, nil
	}
	serveHostProtocol(t, firstServer, []rpcExchange{{method: "initialize", result: map[string]any{}}})
	serveHostProtocol(t, secondServer, []rpcExchange{{method: "initialize", result: map[string]any{}}})

	host, err := startWith(context.Background(), Config{SocketPath: "/managed.sock", RestartDelay: 50 * time.Millisecond}, start, connect, time.Millisecond)
	if err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer host.Close()
	t.Cleanup(func() {
		if leaderPid > 0 {
			_ = syscall.Kill(-leaderPid, syscall.SIGKILL)
		}
	})

	if err := waitForGroupMembers(leaderPid, 2, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(leaderPid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill managed leader: %v", err)
	}
	<-firstDone

	if err := syscall.Kill(-leaderPid, 0); err != nil {
		t.Fatalf("residual child process group vanished before finalize reaped it: %v", err)
	}
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("managed process was not restarted proactively")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := syscall.Kill(-leaderPid, 0); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("residual child of exited managed leader was not reaped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFinalizeManagedExitReapsResidualGroupAndClearsOwnerRecord(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "owner.json")
	socketPath := filepath.Join(root, "ra2a.sock")

	command := exec.Command("sh", "-c", "sleep 60 & sleep 60")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	leaderPid := command.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-leaderPid, syscall.SIGKILL) })
	if err := writeOwnerRecord(ownerPath, ownerRecord{PID: leaderPid, SocketPath: socketPath}); err != nil {
		t.Fatal(err)
	}
	if err := waitForGroupMembers(leaderPid, 2, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(leaderPid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill managed leader: %v", err)
	}

	var output bytes.Buffer
	waitErr := command.Wait()
	finalizeManagedExit(command, &output, ownerPath, socketPath, waitErr, nil, false)

	logs := output.String()
	if !strings.Contains(logs, "event=managed_codex_host_exited pid="+strconv.Itoa(leaderPid)) ||
		!strings.Contains(logs, "event=managed_codex_host_reaped pid="+strconv.Itoa(leaderPid)) {
		t.Fatalf("finalize logs = %q", logs)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("owner record still exists: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := syscall.Kill(-leaderPid, 0); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("residual child process group was not reaped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFinalizeManagedExitSkipsReapedLogWhenGroupAlreadyGone(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "owner.json")
	socketPath := filepath.Join(root, "ra2a.sock")

	command := exec.Command("sh", "-c", "exit 0")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	leaderPid := command.Process.Pid
	if err := writeOwnerRecord(ownerPath, ownerRecord{PID: leaderPid, SocketPath: socketPath}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	waitErr := command.Wait()
	finalizeManagedExit(command, &output, ownerPath, socketPath, waitErr, nil, false)

	logs := output.String()
	if !strings.Contains(logs, "event=managed_codex_host_exited pid="+strconv.Itoa(leaderPid)+" status=clean") {
		t.Fatalf("finalize logs = %q", logs)
	}
	if strings.Contains(logs, "managed_codex_host_reaped") {
		t.Fatalf("reaped logged for an already-gone group: %q", logs)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("owner record still exists: %v", err)
	}
}

func TestFinalizeManagedExitSkipsReapedLogWhenCloseTerminatedTheGroup(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "owner.json")
	socketPath := filepath.Join(root, "ra2a.sock")

	command := exec.Command("sh", "-c", "sleep 60 & sleep 60")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	leaderPid := command.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-leaderPid, syscall.SIGKILL) })
	if err := waitForGroupMembers(leaderPid, 2, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	if err := terminateManagedProcess(command); err != nil {
		t.Fatalf("terminate managed process: %v", err)
	}
	waitErr := command.Wait()
	var output bytes.Buffer
	finalizeManagedExit(command, &output, ownerPath, socketPath, waitErr, nil, true)

	logs := output.String()
	if strings.Contains(logs, "managed_codex_host_reaped") {
		t.Fatalf("reaped logged although Close already terminated the group: %q", logs)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("owner record still exists: %v", err)
	}
}

func TestFinalizeManagedExitWithSurvivorsDoesNotDependOnWatchdogGuard(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "owner.json")
	socketPath := filepath.Join(root, "ra2a.sock")

	command := exec.Command("sh", "-c", "sleep 60 & sleep 60")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	leaderPid := command.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-leaderPid, syscall.SIGKILL) })
	if err := waitForGroupMembers(leaderPid, 2, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := writeOwnerRecord(ownerPath, ownerRecord{PID: leaderPid, SocketPath: socketPath}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(leaderPid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill managed leader: %v", err)
	}
	waitErr := command.Wait()

	var output bytes.Buffer
	finalizeManagedExit(command, &output, ownerPath, socketPath, waitErr, nil, false)

	logs := output.String()
	if !strings.Contains(logs, "event=managed_codex_host_reaped pid="+strconv.Itoa(leaderPid)) {
		t.Fatalf("survivor group was not reaped: %q", logs)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("owner record still exists: %v", err)
	}
}

func TestCleanupOwnerRecordReapsResidualGroupAfterDaemonCrash(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "owner.json")
	socketPath := filepath.Join(root, "ra2a.sock")

	command := exec.Command("sh", "-c", "sleep 60 & sleep 60")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	leaderPid := command.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-leaderPid, syscall.SIGKILL) })
	if err := waitForGroupMembers(leaderPid, 2, 2*time.Second); err != nil {
		t.Fatal(err)
	}

	if err := writeOwnerRecord(ownerPath, ownerRecord{PID: leaderPid, SocketPath: socketPath}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(leaderPid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill managed leader: %v", err)
	}
	waitErr := command.Wait()
	_ = waitErr

	if err := cleanupOwnerRecord(ownerPath); err != nil {
		t.Fatalf("cleanup owner record: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := syscall.Kill(-leaderPid, 0); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("residual group survived daemon-restart cleanup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("owner record still exists: %v", err)
	}
}

func waitForGroupMembers(pgid, want int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for processGroupMemberCount(pgid) < want {
		if time.Now().After(deadline) {
			return fmt.Errorf("process group %d never reached %d members", pgid, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func processGroupMemberCount(pgid int) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	members := 0
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
		if err == nil && pgrp == pgid {
			members++
		}
	}
	return members
}
