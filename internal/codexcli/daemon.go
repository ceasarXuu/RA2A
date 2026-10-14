// Package codexcli adapts locally running Codex CLI TUI sessions into RA2A
// endpoints. It attaches to the official `codex app-server daemon` as a second
// same-user client; it never injects flags into the user's TUI process and
// never starts the daemon on its own.
package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const controlSocketRelative = "app-server-control/app-server-control.sock"

// maxUnixSocketPath is the sun_path limit the daemon works around by
// relocating long socket paths. A resolved path above this limit silently
// disables daemon attachment, so the adapter must detect it up front instead of
// reporting an unreachable daemon later.
const maxUnixSocketPath = 107

type DaemonState struct {
	Running          bool
	SocketPath       string
	CLIVersion       string
	AppServerVersion string
	Detail           string
}

type daemonVersionOutput struct {
	Status            string `json:"status"`
	CLIVersion        string `json:"cliVersion"`
	AppServerVersion  string `json:"appServerVersion"`
	SocketPath        string `json:"socketPath"`
	ManagedCodexPath  string `json:"managedCodexPath"`
	ManagedCodexVersn string `json:"managedCodexVersion"`
}

func defaultCodexHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func controlSocketPath(codexHome string) string {
	if codexHome == "" {
		return ""
	}
	return filepath.Join(codexHome, filepath.FromSlash(controlSocketRelative))
}

// socketPathProblem reports why a resolved socket path cannot be used, or "".
func socketPathProblem(socketPath string) string {
	if socketPath == "" {
		return "codex home is unknown, so the app-server control socket path cannot be resolved"
	}
	if resolved, err := filepath.EvalSymlinks(socketPath); err == nil && resolved != "" {
		socketPath = resolved
	}
	if runtime.GOOS != "windows" {
		return ""
	}
	if len(socketPath) > maxUnixSocketPath {
		return fmt.Sprintf(
			"codex control socket path is %d bytes, above the %d byte AF_UNIX limit; use a shorter CODEX_HOME",
			len(socketPath), maxUnixSocketPath)
	}
	return ""
}

// DetectDaemon probes the official daemon lifecycle command. The command is a
// probe rather than a status query: when the daemon is not running it exits
// non-zero with a connection error instead of printing JSON, which is exactly
// the start_required signal the product decision requires.
func DetectDaemon(ctx context.Context, codexPath string) (DaemonState, error) {
	return detectDaemon(ctx, codexPath, "")
}

// A configured home scopes both the lifecycle probe and its socket fallback.
// The public probe keeps the caller's ambient environment for wrapper use.
func detectDaemon(ctx context.Context, codexPath, codexHome string) (DaemonState, error) {
	home := codexHome
	if home == "" {
		home = defaultCodexHome()
	}
	state := DaemonState{SocketPath: controlSocketPath(home)}
	if codexPath == "" {
		return state, errors.New("codex executable path is required")
	}
	command := exec.CommandContext(ctx, codexPath, "app-server", "daemon", "version")
	command.Env = append(os.Environ(), "CODEX_NO_UPDATE=1")
	if codexHome != "" {
		command.Env = append(command.Env, "CODEX_HOME="+codexHome)
	}
	output, runErr := command.Output()
	trimmed := strings.TrimSpace(string(output))
	if runErr != nil || trimmed == "" {
		state.Detail = daemonProbeDetail(runErr, output)
		return state, nil
	}
	var parsed daemonVersionOutput
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		state.Detail = fmt.Sprintf("daemon version output is not a single JSON object: %v", err)
		return state, nil
	}
	state.CLIVersion = parsed.CLIVersion
	state.AppServerVersion = parsed.AppServerVersion
	if parsed.SocketPath != "" {
		state.SocketPath = parsed.SocketPath
	}
	switch parsed.Status {
	case "running":
		state.Running = true
	case "notRunning", "stopped":
		state.Running = false
		state.Detail = "codex app-server daemon is not running"
	default:
		state.Detail = fmt.Sprintf("unexpected daemon status %q", parsed.Status)
	}
	return state, nil
}

func daemonProbeDetail(runErr error, output []byte) string {
	if runErr == nil {
		return "daemon version returned no JSON"
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && len(exitErr.Stderr) > 0 {
		detail := strings.TrimSpace(string(exitErr.Stderr))
		if index := strings.Index(detail, "\nCaused by:"); index > 0 {
			detail = strings.TrimSpace(detail[:index])
		}
		return detail
	}
	if message := strings.TrimSpace(string(output)); message != "" {
		return message
	}
	return runErr.Error()
}

// parseVersion extracts the app-server version from initialize.userAgent, which
// is the only version-bearing field the handshake offers.
func parseVersion(userAgent string) string {
	for _, field := range strings.Fields(userAgent) {
		if index := strings.LastIndex(field, "/"); index >= 0 {
			candidate := field[index+1:]
			if semver.IsValid("v" + candidate) {
				return candidate
			}
		}
	}
	return ""
}

func versionAtLeast(have, want string) bool {
	return semver.IsValid("v"+have) && semver.IsValid("v"+want) &&
		semver.Compare("v"+have, "v"+want) >= 0
}

const probeTimeout = 10 * time.Second
