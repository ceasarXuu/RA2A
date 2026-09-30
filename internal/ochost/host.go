// Package ochost supervises a local OpenCode server so RA2A and the user's TUI
// can share one instance.
//
// Sharing one server is not a convenience: OpenCode servers do not notify each
// other about writes to the shared session store, so a message injected through
// a different server is invisible in the user's TUI until they manually reload
// the session. It is also the only way RA2A can observe session busy state and
// refuse delivery while the user is mid-turn.
package ochost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	// Executable is the opencode binary. Defaults to "opencode" on PATH.
	Executable string
	// URL is the base URL RA2A and the user's TUI both attach to.
	URL string
	// Stderr receives the supervised process output.
	Stderr io.Writer
	// RestartDelay is how long to wait before restarting an exited server.
	RestartDelay time.Duration
	// ReadinessTimeout bounds the wait for the server to answer.
	ReadinessTimeout time.Duration
	// OwnerPath records which process owns the supervised server.
	OwnerPath string
}

type ownerRecord struct {
	PID        int    `json:"pid"`
	URL        string `json:"url"`
	Executable string `json:"executable"`
	StartedAt  int64  `json:"startedAt"`
}

var (
	// ErrBusy means another process already owns the URL, so RA2A refuses to
	// start a second server rather than fighting over the port.
	ErrBusy        = errors.New("an OpenCode server already owns this URL")
	ErrNotReady    = errors.New("OpenCode server did not become ready")
	ErrUnreachable = errors.New("OpenCode server is unreachable")
)

type Host struct {
	config Config
	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	closed bool
}

func portOf(rawURL string) (int, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://")
	trimmed = strings.TrimSuffix(trimmed, "/")
	host, portText, err := net.SplitHostPort(trimmed)
	if err != nil {
		return 0, fmt.Errorf("opencode URL must be host:port, got %q", rawURL)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return 0, fmt.Errorf("opencode URL must stay on loopback, got %q", host)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("opencode URL has no usable port: %q", rawURL)
	}
	return port, nil
}

// Reachable reports whether something already answers on the URL. RA2A uses it
// to adopt an externally started server instead of starting a duplicate.
func Reachable(ctx context.Context, rawURL string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(rawURL, "/")+"/session", nil)
	if err != nil {
		return false
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode == http.StatusOK
}

// Start supervises an OpenCode server. If a healthy server already answers on
// the URL it is adopted as-is and no process is spawned, so RA2A never fights a
// server the user started themselves.
func Start(ctx context.Context, config Config) (*Host, error) {
	if config.Executable == "" {
		config.Executable = "opencode"
	}
	if config.URL == "" {
		config.URL = "http://127.0.0.1:4099"
	}
	if config.Stderr == nil {
		config.Stderr = io.Discard
	}
	if config.RestartDelay == 0 {
		config.RestartDelay = 2 * time.Second
	}
	if config.ReadinessTimeout == 0 {
		config.ReadinessTimeout = 20 * time.Second
	}
	if _, err := portOf(config.URL); err != nil {
		return nil, err
	}
	host := &Host{config: config}
	if Reachable(ctx, config.URL) {
		return host, nil
	}
	if owner, err := readOwner(config.OwnerPath); err == nil && owner.PID > 0 && processAlive(owner.PID) {
		// A previous RA2A instance recorded ownership but the server is not
		// answering; wait for the recorded process rather than starting a rival.
		if err := host.waitReady(ctx, config.ReadinessTimeout); err == nil {
			return host, nil
		}
	}
	if err := host.spawn(ctx); err != nil {
		return nil, err
	}
	if err := host.waitReady(ctx, config.ReadinessTimeout); err != nil {
		_ = host.Close()
		return nil, err
	}
	return host, nil
}

func (host *Host) spawn(ctx context.Context) error {
	port, err := portOf(host.config.URL)
	if err != nil {
		return err
	}
	// Probe the port, then release it immediately: a probe listener that stays
	// open would block the very server we are about to start.
	probe, listenErr := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if listenErr != nil {
		return fmt.Errorf("%w: port %d cannot be bound", ErrBusy, port)
	}
	_ = probe.Close()
	// The server is deliberately detached from the caller's lifetime. It is a
	// shared resource: RA2A and every attached TUI depend on it, so tying it to
	// whichever TUI happened to start it would let one user closing a TUI break
	// every other client. Its lifetime is bounded by explicit Close, which the
	// RA2A daemon calls on stop/exit.
	superviseCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	command := exec.Command(host.config.Executable, "serve",
		"--port", strconv.Itoa(port), "--hostname", "127.0.0.1")
	command.Stderr = host.config.Stderr
	command.Stdout = host.config.Stderr
	// A new process group keeps a terminal signal aimed at the caller's TUI from
	// reaching the shared server.
	configureServerCommand(command)
	if err := command.Start(); err != nil {
		cancel()
		return fmt.Errorf("start opencode server: %w", err)
	}
	host.mu.Lock()
	host.cmd = command
	host.cancel = cancel
	host.mu.Unlock()
	_ = host.writeOwner()

	go func() {
		_ = command.Wait()
		host.mu.Lock()
		closed := host.closed
		host.mu.Unlock()
		if closed || superviseCtx.Err() != nil {
			return
		}
		// The user's TUI depends on this server, so an unexpected exit is
		// restarted rather than left silently broken.
		time.Sleep(host.config.RestartDelay)
		if Reachable(superviseCtx, host.config.URL) {
			return
		}
		_ = host.spawn(superviseCtx)
	}()
	return nil
}

func (host *Host) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if Reachable(ctx, host.config.URL) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return ErrNotReady
}

func (host *Host) URL() string { return host.config.URL }

func (host *Host) Owned() bool {
	host.mu.Lock()
	defer host.mu.Unlock()
	return host.cmd != nil
}

func (host *Host) Close() error {
	host.mu.Lock()
	if host.closed {
		host.mu.Unlock()
		return nil
	}
	host.closed = true
	cancel := host.cancel
	host.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	_ = host.clearOwner()
	return nil
}

func (host *Host) writeOwner() error {
	if host.config.OwnerPath == "" {
		return nil
	}
	host.mu.Lock()
	pid := 0
	if host.cmd != nil && host.cmd.Process != nil {
		pid = host.cmd.Process.Pid
	}
	host.mu.Unlock()
	if pid == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(host.config.OwnerPath), 0o700); err != nil {
		return err
	}
	record := ownerRecord{PID: pid, URL: host.config.URL,
		Executable: host.config.Executable, StartedAt: time.Now().Unix()}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(host.config.OwnerPath, data, 0o600)
}

func (host *Host) clearOwner() error {
	if host.config.OwnerPath == "" {
		return nil
	}
	return os.Remove(host.config.OwnerPath)
}

func readOwner(path string) (ownerRecord, error) {
	var record ownerRecord
	if path == "" {
		return record, errors.New("no owner path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	return record, json.Unmarshal(data, &record)
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// CleanupShared stops a server that RA2A started, using the recorded owner
// file. It is the counterpart to RegisterOwnerRecord: the RA2A daemon owns the
// shared OpenCode server, so `ra2a stop` and `ra2a exit` must reclaim it the
// same way they reclaim the managed Codex App Server. Stopping is skipped when
// the recorded process is not alive, so an externally started server the user
// wants to keep is never touched.
func CleanupShared(ownerPath string) error {
	if ownerPath == "" {
		return nil
	}
	record, err := readOwner(ownerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if record.PID <= 0 || !processAlive(record.PID) {
		return os.Remove(ownerPath)
	}
	process, err := os.FindProcess(record.PID)
	if err != nil {
		return os.Remove(ownerPath)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop shared OpenCode server: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(record.PID) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(record.PID) {
		_ = process.Signal(syscall.SIGKILL)
	}
	return os.Remove(ownerPath)
}
