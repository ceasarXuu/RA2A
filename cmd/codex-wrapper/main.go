// Command codex-wrapper is a transparent launcher that forwards plain `codex`
// TUI invocations to the RA2A-managed app-server via --remote when that server
// is available. When RA2A is unavailable, uninstalled, or the user passes an
// explicit --remote, the wrapper runs the native codex unchanged so the user's
// ordinary workflow is never altered.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const remoteFlag = "--remote"

var subcommands = map[string]bool{
	"agents": true, "exec": true, "review": true, "login": true, "logout": true,
	"mcp": true, "plugin": true, "mcp-server": true, "app-server": true,
	"remote-control": true, "app": true, "completion": true, "update": true,
	"doctor": true, "sandbox": true, "debug": true, "apply": true,
	"resume": true, "queue": true, "archive": true, "delete": true,
	"migrate-rollouts": true, "unarchive": true, "fork": true, "cloud": true,
	"exec-server": true, "help": true, "version": true,
	"e": true, "a": true,
}

var valueFlags = map[string]bool{
	"-c": true, "-C": true, "--config": true, "--model": true, "-m": true,
	"--model-provider": true, "--name": true, "--personality": true,
	"--effort": true, "--temperature": true, "--service-tier": true,
	"--reasoning-summary": true, "--approval-policy": true,
	"--approvals-reviewer": true, "--sandbox": true, "--add-dir": true,
	"--code-mode-host": true, "--plugins": true, "--variables": true,
}

type plan struct {
	injectRemote   bool
	explicitRemote bool
	tuiMode        bool
}

func classify(args []string) plan {
	var result plan
	// The user may place --remote anywhere; honor it no matter where it appears.
	for _, arg := range args {
		if arg == remoteFlag || strings.HasPrefix(arg, remoteFlag+"=") ||
			arg == "--remote-auth-token-env" || strings.HasPrefix(arg, "--remote-auth-token-env=") {
			result.explicitRemote = true
			return result
		}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--remote-auth-token-env" || strings.HasPrefix(arg, "--remote-auth-token-env=") {
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--") && !strings.Contains(arg, "=") {
			if valueFlags[arg] {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") && len(arg) > 1 && !strings.Contains(arg, "=") {
			if valueFlags[arg] {
				i++
			}
			continue
		}
		result.tuiMode = !subcommands[arg]
		break
	}
	result.injectRemote = result.tuiMode && !result.explicitRemote && len(args) > 0
	// A bare `codex` opens the interactive composer and is also a TUI launch.
	if len(args) == 0 {
		result.tuiMode = true
		result.injectRemote = true
	}
	return result
}

func codexHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".codex")
	}
	return filepath.Join(userHome, ".codex")
}

type ownerRecord struct {
	PID        int    `json:"pid"`
	SocketPath string `json:"socketPath"`
	CodexPath  string `json:"codexPath"`
}

func leasePath() string {
	return filepath.Join(codexHome(), "app-server-control", "app-server-control.sock.ra2a-owner.json")
}

// readySocket returns the RA2A-managed app-server socket only when the owner
// lease resolves and the socket actually accepts connections; otherwise it
// returns an empty string so the wrapper degrades to the native experience.
func readySocket() string {
	raw, err := os.ReadFile(leasePath())
	if err != nil {
		return ""
	}
	var record ownerRecord
	if err := json.Unmarshal(raw, &record); err != nil || record.SocketPath == "" {
		return ""
	}
	if info, err := os.Lstat(record.SocketPath); err != nil || info.Mode()&os.ModeSocket == 0 {
		return ""
	}
	conn, err := net.DialTimeout("unix", record.SocketPath, 750*time.Millisecond)
	if err != nil {
		return ""
	}
	_ = conn.Close()
	return record.SocketPath
}

func realCodex(exe string) (string, error) {
	if path := os.Getenv("CODEX_WRAPPER_REAL_BIN"); path != "" {
		return path, nil
	}
	sibling := filepath.Join(filepath.Dir(exe), "codex.bin")
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		return sibling, nil
	}
	// Official standalone managed install layout (chatgpt.com/codex/install.sh).
	standalone := filepath.Join(codexHome(), "packages", "standalone", "current", "bin", "codex")
	if info, err := os.Stat(standalone); err == nil && !info.IsDir() {
		return standalone, nil
	}
	resolved, err := exec.LookPath("codex")
	if err != nil {
		return "", err
	}
	self, selfErr := filepath.EvalSymlinks(exe)
	probe, probeErr := filepath.EvalSymlinks(resolved)
	if selfErr == nil && probeErr == nil && self == probe {
		return "", errors.New("wrapper resolved to itself; install the real codex as a sibling named codex.bin or set CODEX_WRAPPER_REAL_BIN")
	}
	return resolved, nil
}

func run(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-wrapper: resolve executable:", err)
		os.Exit(1)
	}
	args := os.Args[1:]
	plan := classify(args)
	if plan.injectRemote {
		socket := readySocket()
		if socket == "" {
			fmt.Fprintln(os.Stderr, "codex-wrapper: RA2A managed app-server unavailable; starting native codex")
		} else {
			args = append([]string{remoteFlag, "unix://" + socket}, args...)
		}
	}
	real, err := realCodex(exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-wrapper:", err)
		os.Exit(1)
	}
	if runtime.GOOS == "windows" {
		real = appendExeSuffix(real)
	}
	if err := run(real, args); err != nil {
		fmt.Fprintln(os.Stderr, "codex-wrapper:", err)
		os.Exit(1)
	}
}

// appendExeSuffix is a no-op stub kept for future Windows shim parity; the
// wrapper binary itself is cross-platform and exec works with exact paths.
func appendExeSuffix(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".exe") {
		return path
	}
	return path + ".exe"
}
