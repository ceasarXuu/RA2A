//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/sandbox"
	"golang.org/x/sys/unix"
)

func hardenController() error {
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}

func validateIsolation() error {
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil || os.Getenv("RA2A_SANDBOX_HOST_NETNS") == "" || current == os.Getenv("RA2A_SANDBOX_HOST_NETNS") {
		return fmt.Errorf("sandbox requires a private network namespace")
	}
	for _, name := range []string{"HOME", "CODEX_HOME", "RA2A_PI_SESSION_DIR", "RA2A_OC_SESSION_DIR"} {
		value := filepath.Clean(os.Getenv(name))
		if value != "/home/sandbox" && !strings.HasPrefix(value, "/home/sandbox/") {
			return fmt.Errorf("%s must be under private /home/sandbox", name)
		}
	}
	if os.Getenv("RA2A_SANDBOX_BUDGET_SOCKET") != "/run/budget.sock" {
		return fmt.Errorf("sandbox budget socket must be exclusively mounted at /run/budget.sock")
	}
	return nil
}

func registerAppBridge(registry *agentbridge.Registry, id string, kind agentbridge.AgentKind, directory, budget string) error {
	if kind != agentbridge.AgentCodexCLI || directory != "/run/app-bridges" {
		return fmt.Errorf("private shared Codex App bridge directory required")
	}
	app, err := sandbox.Wrap(&sandbox.App{NodeID: id, Directory: directory}, budget)
	if err != nil {
		return err
	}
	return registry.Register(app)
}

func appProcessStart(pid int) string { return sandbox.AppProcessStart(pid) }
