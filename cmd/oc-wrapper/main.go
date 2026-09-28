// Command oc-wrapper is a transparent launcher for the opencode command.
//
// `opencode --ra2a` attaches the TUI to the RA2A-supervised OpenCode server, so
// messages injected by RA2A appear live and RA2A can observe session busy
// state. Every other invocation passes straight through to the native opencode,
// so the wrapper is invisible unless the user asks for it.
//
// Sharing one server is required, not cosmetic: OpenCode servers do not notify
// each other about writes to the shared session store, so a message injected
// through a different server stays invisible in the user's TUI until they
// reload the session, and RA2A cannot see whether the user is mid-turn.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/ochost"
)

const ra2aFlag = "--ra2a"

const usage = `opencode --ra2a

  --ra2a   attach the TUI to the RA2A-supervised OpenCode server. Messages sent
           by RA2A then appear live, and RA2A can see whether this session is
           busy so it never interrupts a turn in progress.

  Any other invocation runs the native opencode unchanged.
`

type config struct {
	serverURL  string
	executable string
	timeout    time.Duration
	ownerPath  string
	attachArgs []string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr *os.File) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		if containsRA2A(args) {
			fmt.Fprint(stdout, usage)
			return nil
		}
		return passthrough(args, stdout, stderr)
	}
	if !containsRA2A(args) {
		return passthrough(args, stdout, stderr)
	}

	settings := config{
		serverURL:  envOr("RA2A_OPENCODE_URL", "http://127.0.0.1:4099"),
		executable: nativeExecutable(),
		timeout:    25 * time.Second,
		ownerPath:  ownerPath(),
	}
	settings.attachArgs = withoutRA2A(args)

	host, err := ochost.Start(ctx, ochost.Config{
		Executable:       settings.executable,
		URL:              settings.serverURL,
		Stderr:           stderr,
		OwnerPath:        settings.ownerPath,
		ReadinessTimeout: settings.timeout,
	})
	if err != nil {
		if errors.Is(err, ochost.ErrBusy) {
			return fmt.Errorf("cannot start the OpenCode server for RA2A: %w", err)
		}
		return fmt.Errorf("prepare the RA2A OpenCode server: %w", err)
	}
	defer func() { _ = host.Close() }()

	command := exec.Command(settings.executable, append([]string{"attach", settings.serverURL}, settings.attachArgs...)...)
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("opencode attach %s: %w", settings.serverURL, err)
	}
	return nil
}

func containsRA2A(args []string) bool {
	for _, arg := range args {
		if arg == ra2aFlag {
			return true
		}
	}
	return false
}

func withoutRA2A(args []string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == ra2aFlag {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

// passthrough runs the native opencode with the original arguments. The
// wrapper must be invisible to a user who never asked for RA2A.
func passthrough(args []string, stdout, stderr *os.File) error {
	command := exec.Command(nativeExecutable(), args...)
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

// nativeExecutable finds the real opencode, skipping this wrapper so it can never
// invoke itself. The installer places the real binary under a recorded path; a
// same-directory sibling is the fallback.
func nativeExecutable() string {
	if recorded := os.Getenv("RA2A_OPENCODE_BINARY"); recorded != "" {
		if _, err := os.Stat(recorded); err == nil {
			return recorded
		}
	}
	if self, err := os.Executable(); err == nil {
		directory := filepath.Dir(self)
		for _, name := range []string{"opencode.real", "opencode-bin"} {
			candidate := filepath.Join(directory, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	if path, err := exec.LookPath("opencode"); err == nil && !isSelf(path) {
		return path
	}
	return "opencode"
}

func isSelf(path string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	resolvedSelf, _ := filepath.EvalSymlinks(self)
	resolvedPath, _ := filepath.EvalSymlinks(path)
	return resolvedSelf == resolvedPath
}

func ownerPath() string {
	if override := os.Getenv("RA2A_OC_OWNER_FILE"); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ra2a", "opencode-owner.json")
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
