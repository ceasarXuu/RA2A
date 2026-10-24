//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

// macOS has no network or mount namespaces available to an unprivileged
// process. The worker therefore relies on a short private root, a sandbox-exec
// profile and private per-kind homes; it never claims kernel-level network
// isolation.
func hardenController() error { return nil }

func validateIsolation() error {
	root := filepath.Clean(os.Getenv("RA2A_SANDBOX_ROOT"))
	if root == "." || !filepath.IsAbs(root) {
		return fmt.Errorf("sandbox requires an absolute private root")
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("sandbox private root must be an owned 0700 directory")
	}
	for _, name := range []string{"HOME", "CODEX_HOME", "RA2A_PI_SESSION_DIR", "RA2A_OC_SESSION_DIR"} {
		value := os.Getenv(name)
		if value == "" {
			continue
		}
		value = filepath.Clean(value)
		if value != root && !strings.HasPrefix(value, root+string(filepath.Separator)) {
			return fmt.Errorf("%s must be under the private sandbox root", name)
		}
	}
	budget := os.Getenv("RA2A_SANDBOX_BUDGET_SOCKET")
	if !filepath.IsAbs(budget) || !strings.HasPrefix(filepath.Clean(budget), root+string(filepath.Separator)) || len(budget) >= 104 {
		return fmt.Errorf("sandbox budget socket must be a short absolute path under the private root")
	}
	if os.Getenv("RA2A_SANDBOX_APP_SOCKET_DIR") != "" {
		return fmt.Errorf("macOS App stdio bridge is not implemented; App remains BLOCKED_ADAPTER")
	}
	return nil
}

func registerAppBridge(*agentbridge.Registry, string, agentbridge.AgentKind, string, string) error {
	return fmt.Errorf("macOS App stdio bridge is not implemented; App remains BLOCKED_ADAPTER")
}

func appProcessStart(int) string { return "" }
