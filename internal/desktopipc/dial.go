package desktopipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
)

func SocketCandidates(goos, codexHome, tempDir string, uid int) []string {
	if goos == "windows" {
		return []string{`\\.\pipe\codex-ipc`}
	}
	return []string{
		path.Join(codexHome, "ipc", "ipc.sock"),
		path.Join(tempDir, "codex-ipc", fmt.Sprintf("ipc-%d.sock", uid)),
	}
}

func DefaultSocketCandidates() ([]string, error) {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		home = filepath.Join(userHome, ".codex")
	}
	return SocketCandidates(runtime.GOOS, home, os.TempDir(), currentUID()), nil
}

func DialContext(ctx context.Context, explicitSocket string) (net.Conn, string, error) {
	paths := []string{explicitSocket}
	if explicitSocket == "" {
		var err error
		paths, err = DefaultSocketCandidates()
		if err != nil {
			return nil, "", err
		}
	}
	var failures []error
	for _, socketPath := range paths {
		conn, err := dialPlatform(ctx, socketPath)
		if err == nil {
			return conn, socketPath, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", socketPath, err))
	}
	return nil, "", fmt.Errorf("connect Desktop IPC: %w", errors.Join(failures...))
}

func currentUID() int {
	current, err := user.Current()
	if err != nil {
		return 0
	}
	uid, err := strconv.Atoi(current.Uid)
	if err != nil {
		return 0
	}
	return uid
}
