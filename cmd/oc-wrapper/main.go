// Command oc-wrapper is a transparent launcher for the opencode command.
//
// `opencode --ra2a` attaches the TUI to a known session on the shared server, so
// messages injected by RA2A are executed there. Every other invocation passes
// straight through to the native opencode,
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
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/ochost"
	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

const ra2aFlag = "--ra2a"

const usage = `opencode [options] --ra2a

  --ra2a   run the TUI on the RA2A-supervised OpenCode server. Messages sent by
           RA2A then appear live, and RA2A can see whether this session is busy
           so it never interrupts a turn in progress.

  --yolo and --auto approve permission requests for the attached session (but
  never override explicit denies). --port,
  --hostname and --mdns are refused because they would move the TUI off the
  shared server.

  Any invocation without --ra2a runs the native opencode untouched.
`

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
	release, err := ocsession.Register(ocsession.Directory(), sessionID)
	if err != nil {
		return fmt.Errorf("register OpenCode attachment: %w", err)
	}
	defer release()
	fmt.Fprintf(stderr, "opencode_attachment_registered session=%s server=%s\n", sessionID, settings.serverURL)
	if settings.autoApprove {
		approveCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		ready := make(chan error, 1)
		go approveRequests(approveCtx, settings.serverURL, sessionID, ready, stderr)
		if err := <-ready; err != nil {
			return fmt.Errorf("start OpenCode auto-approval: %w", err)
		}
	}

	command := exec.Command(settings.executable, append([]string{"attach", settings.serverURL}, attach...)...)
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("opencode attach %s: %w", settings.serverURL, err)
	}
	return nil
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
	command := exec.Command(executable, args...)
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
		if _, err := os.Stat(recorded); err == nil && !isSelf(recorded) {
			return recorded
		}
	}
	if self, err := os.Executable(); err == nil {
		directory := filepath.Dir(self)
		names := []string{"opencode.real", "opencode-bin"}
		if runtime.GOOS == "windows" {
			names = []string{"opencode.real.exe", "opencode-bin.exe"}
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
		name := "opencode"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidate := filepath.Join(directory, name)
		if info, err := os.Stat(candidate); err != nil || info.IsDir() {
			continue
		}
		if isSelf(candidate) {
			continue
		}
		return candidate
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
