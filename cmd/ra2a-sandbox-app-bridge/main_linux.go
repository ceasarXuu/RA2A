package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/ceasarXuu/RA2A/internal/codexstdio"
	"github.com/ceasarXuu/RA2A/internal/sandbox"
	"golang.org/x/sys/unix"
)

func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "sandbox App bridge stopped")
		os.Exit(1)
	}
}

func run() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	home := os.Getenv("CODEX_HOME")
	network, err := os.Readlink("/proc/self/ns/net")
	if err != nil || os.Getenv("HOME") != "/home/sandbox" || home != "/home/sandbox/.codex" || os.Getenv("RA2A_SANDBOX_HOST_NETNS") == "" || network == os.Getenv("RA2A_SANDBOX_HOST_NETNS") {
		return fmt.Errorf("private App isolation required")
	}
	peer, err := strconv.Atoi(os.Getenv("RA2A_SANDBOX_APP_WORKER_PID"))
	if err != nil || peer <= 0 || os.Getenv("RA2A_SANDBOX_APP_WORKER_START") == "" || sandbox.AppProcessStart(peer) != os.Getenv("RA2A_SANDBOX_APP_WORKER_START") {
		return fmt.Errorf("protected App worker required")
	}
	path := os.Getenv("RA2A_SANDBOX_APP_NATIVE")
	if !filepath.IsAbs(path) {
		return fmt.Errorf("explicit native path required")
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		cmd := exec.CommandContext(ctx, path, "--version")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	server := false
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-c" && i+1 < len(os.Args) {
			i++
			continue
		}
		if os.Args[i] == "app-server" && !server {
			server = true
			continue
		}
		if os.Args[i] != "--analytics-default-enabled" {
			return fmt.Errorf("only new App backend mode is permitted")
		}
	}
	if !server {
		return fmt.Errorf("App backend mode required")
	}
	listener, err := sandbox.ListenApp(os.Getenv("RA2A_SANDBOX_APP_SOCKET_DIR"))
	if err != nil {
		return err
	}
	defer listener.Close()
	cmd := exec.CommandContext(ctx, path, os.Args[1:]...)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	bridge := codexstdio.New(os.Stdin, os.Stdout, out, in)
	host := sandbox.AppHost{Bridge: bridge, PID: cmd.Process.Pid, Start: sandbox.AppProcessStart(cmd.Process.Pid), PeerPID: peer, PeerStart: os.Getenv("RA2A_SANDBOX_APP_WORKER_START"), Home: home, Executable: path, NodeID: os.Getenv("RA2A_SANDBOX_NODE_ID")}
	go func() { _ = host.Serve(ctx, listener) }()
	err = bridge.Run(ctx)
	cancel()
	_ = cmd.Wait()
	return err
}
