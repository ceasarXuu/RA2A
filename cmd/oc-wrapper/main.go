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
	"net/url"
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

const usage = `opencode [options] --ra2a

  --ra2a   run the TUI on the RA2A-supervised OpenCode server. Messages sent by
           RA2A then appear live, and RA2A can see whether this session is busy
           so it never interrupts a turn in progress.

  Every other flag is passed through unchanged, so --yolo, --model, --agent and
  the rest keep working: opencode --yolo --ra2a is valid. --port, --hostname and
  --mdns are refused because they would move the TUI off the shared server.

  Any invocation without --ra2a runs the native opencode untouched.
`

type config struct {
	serverURL  string
	executable string
	timeout    time.Duration
	ownerPath  string
	launch     []string
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
	// The RA2A help is asked for by combining the two, in either order, so the
	// check cannot depend on the flag being first.
	if containsRA2A(args) && (containsHelp(args)) {
		fmt.Fprint(stdout, usage)
		return nil
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
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
	launch, err := launchArgs(settings.serverURL, withoutRA2A(args))
	if err != nil {
		return err
	}
	settings.launch = launch

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

	command := exec.Command(settings.executable, settings.launch...)
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("opencode: %w", err)
	}
	return nil
}

// launchArgs builds the native command line that puts the TUI on the supervised
// server.
//
// The top-level command is used rather than `attach`, because `attach` has a far
// smaller flag set: it rejects `--yolo`/`--auto` and `--model`, `--agent`,
// `--prompt` and every other flag that only the top-level command understands.
// Pointing the top-level command at the supervised server's host and port makes
// it reuse that server instead of starting another one -- the listener count is
// unchanged -- so the user keeps every flag they passed and still shares the one
// server RA2A delivers into.
func launchArgs(serverURL string, args []string) ([]string, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("parse server URL %q: %w", serverURL, err)
	}
	host, port := parsed.Hostname(), parsed.Port()
	if host == "" || port == "" {
		return nil, fmt.Errorf("server URL %q must carry a host and a port", serverURL)
	}
	for _, arg := range args {
		switch strings.SplitN(arg, "=", 2)[0] {
		case "--port", "--hostname", "--mdns", "--mdns-domain":
			// Letting these through would silently point the TUI at a different
			// server, and messages RA2A delivers would stop appearing in it.
			return nil, fmt.Errorf("%s cannot be combined with %s: the shared server address is fixed", strings.SplitN(arg, "=", 2)[0], ra2aFlag)
		}
	}
	return append([]string{"--port", port, "--hostname", host}, args...), nil
}

func containsRA2A(args []string) bool {
	for _, arg := range args {
		if arg == ra2aFlag {
			return true
		}
	}
	return false
}

func containsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
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

// nativeExecutable finds the real opencode, skipping this wrapper so it can
// never invoke itself.
//
// The installer places the wrapper in a PATH directory that precedes the real
// binary, so a plain exec.LookPath("opencode") resolves back to the wrapper. The
// whole PATH is therefore scanned and the first candidate that is not this
// binary wins; returning a bare name would resolve to the wrapper again.
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
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if directory == "" {
			directory = "."
		}
		candidate := filepath.Join(directory, "opencode")
		if info, err := os.Stat(candidate); err != nil || info.IsDir() {
			continue
		}
		if isSelf(candidate) {
			continue
		}
		return candidate
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
