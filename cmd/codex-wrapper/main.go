// Command codex-wrapper is a transparent launcher that forwards plain `codex`
// TUI invocations to the RA2A-managed app-server via --remote when that server
// is available. When RA2A is unavailable, uninstalled, or the user passes an
// explicit --remote, the wrapper runs the native codex unchanged so the user's
// ordinary workflow is never altered.
package main

import (
	"context"
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

	"github.com/ceasarXuu/RA2A/internal/codexcli"
)

const remoteFlag = "--remote"

// managedHostTimeout bounds the usability gate that must pass before the
// wrapper points a TUI at the RA2A-managed app-server.
const managedHostTimeout = 3 * time.Second

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
	sawPositional := false
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
		sawPositional = true
		break
	}
	// A flags-only invocation is still a TUI launch (`codex --yolo`); only the
	// informational flags pass through untouched.
	if !sawPositional && !hasInfoFlag(args) {
		result.tuiMode = true
	}
	result.injectRemote = result.tuiMode && !result.explicitRemote && len(args) > 0
	// A bare `codex` opens the interactive composer and is also a TUI launch.
	if len(args) == 0 {
		result.tuiMode = true
		result.injectRemote = true
	}
	return result
}

func hasInfoFlag(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--help", "-h", "--version", "-V":
			return true
		}
	}
	return false
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
	// Codex 0.159+ may relocate the real socket outside CODEX_HOME to work
	// around the AF_UNIX path limit and leave a symlink behind, so resolve the
	// recorded path before the socket type check and hand the resolved path to
	// --remote.
	resolved := record.SocketPath
	if target, err := filepath.EvalSymlinks(record.SocketPath); err == nil && target != "" {
		resolved = target
	}
	if info, err := os.Stat(resolved); err != nil || info.Mode()&os.ModeSocket == 0 {
		return ""
	}
	conn, err := net.DialTimeout("unix", resolved, 750*time.Millisecond)
	if err != nil {
		return ""
	}
	_ = conn.Close()
	return resolved
}

func realCodex(exe string) (string, error) {
	if path := os.Getenv("CODEX_WRAPPER_REAL_BIN"); path != "" {
		if !sameExecutable(exe, path) {
			return path, nil
		}
		return "", errors.New("CODEX_WRAPPER_REAL_BIN points to the wrapper itself")
	}
	directory := filepath.Dir(exe)
	for _, name := range []string{"codex.bin", "codex.bin.exe", "codex.bin.cmd", "codex.bin.bat"} {
		candidate := filepath.Join(directory, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && !sameExecutable(exe, candidate) {
			return candidate, nil
		}
	}
	if data, err := os.ReadFile(filepath.Join(directory, ".ra2a-codex-native-path")); err == nil {
		candidate := strings.TrimSpace(string(data))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && !sameExecutable(exe, candidate) {
			return candidate, nil
		}
	}
	// Official standalone managed install layout (chatgpt.com/codex/install.sh).
	standalone := filepath.Join(codexHome(), "packages", "standalone", "current", "bin", "codex")
	if info, err := os.Stat(standalone); err == nil && !info.IsDir() {
		return standalone, nil
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		for _, name := range []string{"codex", "codex.exe", "codex.cmd"} {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && !sameExecutable(exe, candidate) &&
				!(runtime.GOOS == "windows" && strings.EqualFold(candidate, filepath.Join(directory, "codex.cmd"))) {
				return candidate, nil
			}
		}
	}
	return "", errors.New("native Codex not found outside RA2A wrapper; reinstall to restore its native path")
}

func sameExecutable(self, candidate string) bool {
	selfPath, selfErr := filepath.EvalSymlinks(self)
	candidatePath, candidateErr := filepath.EvalSymlinks(candidate)
	return selfErr == nil && candidateErr == nil && selfPath == candidatePath
}

func run(exe string, args []string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(exe), ".cmd") || strings.EqualFold(filepath.Ext(exe), ".bat")) {
		cmd = exec.Command("cmd.exe", append([]string{"/d", "/c", exe}, args...)...)
	} else {
		cmd = exec.Command(exe, args...)
	}
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
	real, err := realCodex(exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-wrapper:", err)
		os.Exit(1)
	}
	if plan.injectRemote {
		socket := readySocket()
		if socket == "" {
			fmt.Fprintln(os.Stderr, "codex-wrapper: RA2A managed app-server unavailable; starting native codex")
		} else {
			originalCount := len(args)
			args = managedArgs(real, socket, args)
			if len(args) > originalCount {
				fmt.Fprintf(os.Stderr, "codex_wrapper_managed_fallback socket=%s reason=official_daemon_unavailable\n", socket)
			}
		}
	}
	if err := run(real, args); err != nil {
		fmt.Fprintln(os.Stderr, "codex-wrapper:", err)
		os.Exit(1)
	}
}

func managedArgs(real, socket string, args []string) []string {
	// A current official CLI shares its own daemon. Only proxy through RA2A
	// when that native ownership path is unavailable.
	probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	state, err := codexcli.DetectDaemon(probeCtx, real)
	if err == nil && state.Running {
		return args
	}
	// The managed host runs with the RA2A service environment, not the caller's
	// shell environment. Capturing the TUI into a host that cannot serve the
	// caller (missing proxy settings, different CODEX_HOME, unreachable
	// backend) silently breaks the user's session, so require a usable host.
	gateCtx, gateCancel := context.WithTimeout(context.Background(), managedHostTimeout)
	defer gateCancel()
	if err := codexcli.CheckManagedHost(gateCtx, socket, codexHome()); err != nil {
		fmt.Fprintf(os.Stderr, "codex_wrapper_managed_skipped socket=%s reason=%q\n", socket, err.Error())
		return args
	}
	return append([]string{remoteFlag, "unix://" + socket}, args...)
}
