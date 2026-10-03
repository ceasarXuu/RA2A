// Command oc-wrapper is a transparent launcher for the opencode command.
//
// Interactive `opencode` (or `opencode --ra2a`) tracks the TUI's current session
// on the shared server, so messages injected by RA2A are executed there.
// Every other invocation passes
// straight through to the native opencode.
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
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/ochost"
	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

const ra2aFlag = "--ra2a"

const usage = `opencode [options] [--ra2a]

  Interactive TUI launches use the RA2A-supervised OpenCode server by default.
  --ra2a   explicitly request the same shared TUI mode. Messages sent by
           RA2A then appear live, and RA2A can see whether this session is busy
           so it never interrupts a turn in progress.

  --yolo and --auto approve permission requests for the attached session (but
  never override explicit denies). --port,
  --hostname and --mdns are refused because they would move the TUI off the
  shared server.

  Non-TUI subcommands, --help and --version run the native opencode untouched.
`

var nativeSubcommands = map[string]bool{
	"completion": true, "acp": true, "mcp": true, "run": true, "debug": true,
	"providers": true, "auth": true, "agent": true, "upgrade": true,
	"uninstall": true, "serve": true, "web": true, "models": true,
	"stats": true, "export": true, "import": true, "github": true,
	"pr": true, "session": true, "plugin": true, "plug": true,
	"db": true, "attach": true,
}

type config struct {
	serverURL   string
	executable  string
	timeout     time.Duration
	ownerPath   string
	attachArgs  []string
	autoApprove bool
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
	if !containsRA2A(args) && !interactiveTUI(args) {
		if usesNativeOnlyOptions(args) {
			fmt.Fprintln(stderr, "opencode: these options require a private server; RA2A auto-attachment is not active for this invocation")
		}
		return passthrough(args, stdout, stderr)
	}

	settings := config{
		serverURL:  envOr("RA2A_OPENCODE_URL", "http://127.0.0.1:4099"),
		executable: nativeExecutable(),
		timeout:    25 * time.Second,
		ownerPath:  ownerPath(),
	}
	if settings.executable == "" {
		return errors.New("native OpenCode binary not found outside the RA2A wrapper")
	}
	attach, autoApprove, err := translateAttachArgs(withoutRA2A(args))
	if err != nil {
		return err
	}
	settings.attachArgs, settings.autoApprove = attach, autoApprove

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
	sessionID, attach, err := ocsession.Select(ctx, settings.serverURL, settings.attachArgs)
	if err != nil {
		return fmt.Errorf("select OpenCode session: %w", err)
	}
	focusConfig, release, err := ocsession.PrepareFocus(ocsession.Directory())
	if err != nil {
		return fmt.Errorf("prepare OpenCode focus tracking: %w", err)
	}
	defer release()
	fmt.Fprintf(stderr, "opencode_attachment_tracking startup_session=%s server=%s\n", sessionID, settings.serverURL)
	if settings.autoApprove {
		approveCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		ready := make(chan error, 1)
		go approveCurrentRequests(approveCtx, settings.serverURL, func() string { return ocsession.Current(ocsession.Directory()) }, ready, stderr)
		if err := <-ready; err != nil {
			return fmt.Errorf("start OpenCode auto-approval: %w", err)
		}
	}

	command := nativeCommand(settings.executable, append([]string{"attach", settings.serverURL}, attach...)...)
	command.Env = append(os.Environ(), "OPENCODE_TUI_CONFIG="+focusConfig)
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("opencode attach %s: %w", settings.serverURL, err)
	}
	return nil
}

func interactiveTUI(args []string) bool {
	if usesNativeOnlyOptions(args) {
		return false
	}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "--version" || arg == "-v" {
			return false
		}
		if nativeSubcommands[arg] {
			return false
		}
	}
	return true
}

func usesNativeOnlyOptions(args []string) bool {
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "--port", "--hostname", "--mdns", "--mdns-domain", "--cors", "--fork",
			"--model", "-m", "--agent", "--prompt":
			return true
		}
	}
	return false
}

// translateAttachArgs splits the user's arguments into what `attach` accepts and
// the top-level-only flags the wrapper has to handle.
//
// `attach` exposes a far smaller flag set than the top-level command: it has no
// permission flag at all, so `opencode --yolo --ra2a` used to die on an argument
// dump from the yargs parser. The flag is removed from the arguments; the
// caller answers this session's permission.asked events instead of changing
// the server-wide policy or setting an attach-only environment variable.
func translateAttachArgs(args []string) (kept []string, autoApprove bool, err error) {
	kept = make([]string, 0, len(args))
	for _, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		negative := hasValue && (value == "false" || value == "0")
		switch name {
		case "--yolo", "--auto":
			if !negative {
				autoApprove = true
			}
		case "--port", "--hostname", "--mdns", "--mdns-domain":
			// Letting these through would point the TUI at a different server,
			// and messages RA2A delivers would stop appearing in it with no
			// visible cause.
			return nil, false, fmt.Errorf("%s cannot be combined with %s: the shared server address is fixed", name, ra2aFlag)
		default:
			kept = append(kept, arg)
		}
	}
	return kept, autoApprove, nil
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
	executable := nativeExecutable()
	if executable == "" {
		return errors.New("native OpenCode binary not found outside the RA2A wrapper")
	}
	command := nativeCommand(executable, args...)
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func nativeCommand(executable string, args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(executable), ".cmd") || strings.EqualFold(filepath.Ext(executable), ".bat")) {
		return exec.Command("cmd.exe", append([]string{"/d", "/c", executable}, args...)...)
	}
	return exec.Command(executable, args...)
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
		if _, err := os.Stat(recorded); err == nil && !isSelf(recorded) {
			return recorded
		}
	}
	if self, err := os.Executable(); err == nil {
		directory := filepath.Dir(self)
		if raw, err := os.ReadFile(filepath.Join(directory, ".ra2a-opencode-native-path")); err == nil {
			candidate := strings.TrimSpace(string(raw))
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && !isSelf(candidate) {
				return candidate
			}
		}
		names := []string{"opencode.real", "opencode-bin"}
		if runtime.GOOS == "windows" {
			names = []string{"opencode.real.exe", "opencode.real.cmd", "opencode-bin.exe", "opencode-bin.cmd"}
		}
		for _, name := range names {
			candidate := filepath.Join(directory, name)
			if _, err := os.Stat(candidate); err == nil && !isSelf(candidate) {
				return candidate
			}
		}
	}
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if directory == "" {
			directory = "."
		}
		names := []string{"opencode"}
		if runtime.GOOS == "windows" {
			names = []string{"opencode.exe", "opencode.cmd"}
		}
		for _, name := range names {
			candidate := filepath.Join(directory, name)
			if info, err := os.Stat(candidate); err != nil || info.IsDir() || isSelf(candidate) {
				continue
			}
			return candidate
		}
	}
	return ""
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
