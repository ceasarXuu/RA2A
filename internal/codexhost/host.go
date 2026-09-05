package codexhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ceasarXuu/RA2A/internal/appserverprobe"
)

type Config struct {
	CodexPath    string
	SocketPath   string
	Stderr       io.Writer
	OwnerPath    string
	RestartDelay time.Duration
}

var ErrSessionBusy = errors.New("SESSION_BUSY")

type startFunc func(context.Context, Config) (*managedProcess, error)
type connectFunc func(context.Context, string) (io.ReadWriteCloser, error)

type managedProcess struct {
	command    *exec.Cmd
	done       chan struct{}
	ownerPath  string
	socketPath string
	// terminatedByOwner is set by Close after it terminated the process
	// group itself; the exit goroutine then skips residual reaping (and its
	// reaped log) because no survivor can remain.
	terminatedByOwner atomic.Bool
}

type Host struct {
	config       Config
	start        startFunc
	connect      connectFunc
	retryDelay   time.Duration
	restartDelay time.Duration

	mu         sync.Mutex
	process    *managedProcess
	connection io.ReadWriteCloser
	client     *appserverprobe.Client
	closed     bool
	supervise  bool
}

func Start(ctx context.Context, config Config) (*Host, error) {
	if config.CodexPath == "" || config.SocketPath == "" {
		return nil, errors.New("Codex path and App Server socket path are required")
	}
	prepared, err := prepareConfig(config)
	if err != nil {
		return nil, err
	}
	return startWith(ctx, prepared, startManaged, DialUnixWebSocket, 100*time.Millisecond)
}

func startManaged(ctx context.Context, config Config) (*managedProcess, error) {
	if err := os.MkdirAll(filepath.Dir(config.SocketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create App Server control directory: %w", err)
	}
	command := managedCommand(ctx, config)
	command.Stderr = config.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	process := &managedProcess{
		command: command, done: make(chan struct{}), ownerPath: config.OwnerPath, socketPath: config.SocketPath,
	}
	if config.OwnerPath != "" {
		if err := writeOwnerRecord(config.OwnerPath, ownerRecord{
			PID: command.Process.Pid, SocketPath: config.SocketPath, CodexPath: config.CodexPath,
		}); err != nil {
			_ = terminateManagedProcess(command)
			_ = command.Wait()
			return nil, fmt.Errorf("record managed Codex host: %w", err)
		}
	}
	go func() {
		waitErr := command.Wait()
		finalizeManagedExit(command, config.Stderr, process.ownerPath, process.socketPath, waitErr, ctx.Err(), process.terminatedByOwner.Load())
		close(process.done)
	}()
	return process, nil
}

// finalizeManagedExit runs after the managed leader exits: it records the exit,
// reaps any residual process group members (Linux node-wrapper codex child),
// and only then clears the owner record. The owner record is kept when the reap
// fails so a later daemon startup can still reclaim the residual group. When
// Close itself terminated the process group, reaping is skipped: no survivor
// can remain and the reaped log would be misleading.
func finalizeManagedExit(command *exec.Cmd, stderr io.Writer, ownerPath, socketPath string, waitErr, parentErr error, terminatedByOwner bool) {
	reportManagedExit(stderr, command.Process.Pid, waitErr, parentErr)
	if !terminatedByOwner {
		reaped, reapErr := reapManagedGroup(command)
		switch {
		case reaped:
			reportManagedReaped(stderr, command.Process.Pid)
		case reapErr != nil:
			reportManagedReapFailed(stderr, command.Process.Pid, reapErr)
		}
		if reapErr == nil {
			_ = clearOwnerRecord(ownerPath, command.Process.Pid, socketPath)
		}
		return
	}
	_ = clearOwnerRecord(ownerPath, command.Process.Pid, socketPath)
}

func reportManagedExit(writer io.Writer, pid int, waitErr, parentErr error) {
	if writer == nil || parentErr != nil {
		return
	}
	if waitErr == nil {
		fmt.Fprintf(writer, "event=managed_codex_host_exited pid=%d status=clean\n", pid)
		return
	}
	fmt.Fprintf(writer, "event=managed_codex_host_exited pid=%d error=%q\n", pid, waitErr)
}

func reportManagedReaped(writer io.Writer, pid int) {
	if writer == nil {
		return
	}
	fmt.Fprintf(writer, "event=managed_codex_host_reaped pid=%d\n", pid)
}

func reportManagedReapFailed(writer io.Writer, pid int, reapErr error) {
	if writer == nil {
		return
	}
	fmt.Fprintf(writer, "event=managed_codex_host_reap_failed pid=%d error=%q\n", pid, reapErr)
}

func managedCommand(ctx context.Context, config Config) *exec.Cmd {
	command := exec.CommandContext(ctx, config.CodexPath, "app-server", "--listen", unixListenURL(config.SocketPath))
	configureManagedCommand(command)
	return command
}

func unixListenURL(socketPath string) string {
	return "unix://" + strings.ReplaceAll(socketPath, `\`, "/")
}

func startWith(ctx context.Context, config Config, start startFunc, connect connectFunc, retryDelay time.Duration) (*Host, error) {
	restartDelay := config.RestartDelay
	if restartDelay <= 0 {
		restartDelay = time.Second
	}
	host := &Host{
		config: config, start: start, connect: connect,
		retryDelay: retryDelay, restartDelay: restartDelay,
	}
	host.mu.Lock()
	err := host.ensureConnected(ctx)
	if err == nil {
		host.supervise = true
	}
	host.mu.Unlock()
	if err != nil {
		host.Close()
		return nil, err
	}
	return host, nil
}

func (host *Host) ListThreadSummaries(ctx context.Context) ([]appserverprobe.ThreadSummary, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.ensureConnected(ctx); err != nil {
		return nil, err
	}
	threads, err := host.client.ListThreadSummaries()
	if err != nil {
		host.disconnect()
	}
	return threads, err
}

func (host *Host) SendMessage(ctx context.Context, threadID, prompt string) error {
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.ensureConnected(ctx); err != nil {
		return err
	}
	_, err := host.client.InjectMessage(threadID, prompt)
	if err != nil {
		host.disconnect()
		var rpcErr *appserverprobe.RPCError
		if errors.As(err, &rpcErr) {
			message := strings.ToLower(rpcErr.Message)
			if strings.Contains(message, "active writer") || (strings.Contains(message, "turn") && strings.Contains(message, "active")) {
				return fmt.Errorf("%w: %s", ErrSessionBusy, rpcErr.Message)
			}
		}
	}
	return err
}

func (host *Host) ResolveThreadModel(ctx context.Context, threadID string) (string, error) {
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.ensureConnected(ctx); err != nil {
		return "", err
	}
	model, err := host.client.ResolveThreadModel(threadID)
	if err != nil {
		host.disconnect()
	}
	return model, err
}

func (host *Host) ensureConnected(ctx context.Context) error {
	if host.closed {
		return errors.New("Codex host is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if host.process != nil && processDone(host.process) {
		host.disconnect()
		host.process = nil
	}
	if host.client != nil {
		return nil
	}
	if host.process == nil || processDone(host.process) {
		if host.start == nil {
			return errors.New("Codex host is unavailable")
		}
		process, err := host.start(ctx, host.config)
		if err != nil {
			return fmt.Errorf("start managed Codex host: %w", err)
		}
		host.process = process
		go host.watchProcess(ctx, process)
	}

	for {
		connection, err := host.connect(ctx, host.config.SocketPath)
		if err == nil {
			return host.initialize(connection)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("connect to managed Codex host: %w", ctx.Err())
		case <-host.process.done:
			return errors.New("managed Codex host exited before accepting connections")
		case <-time.After(host.retryDelay):
		}
	}
}

func (host *Host) watchProcess(ctx context.Context, process *managedProcess) {
	select {
	case <-ctx.Done():
		return
	case <-process.done:
	}
	timer := time.NewTimer(host.restartDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	host.mu.Lock()
	defer host.mu.Unlock()
	if host.closed || !host.supervise || host.process != process {
		return
	}
	host.disconnect()
	host.process = nil
	_ = host.ensureConnected(ctx)
}

func (host *Host) initialize(connection io.ReadWriteCloser) error {
	client := appserverprobe.New(connection, connection)
	if err := client.Initialize(); err != nil {
		_ = connection.Close()
		return fmt.Errorf("initialize managed Codex host: %w", err)
	}
	host.connection = connection
	host.client = client
	return nil
}

func (host *Host) disconnect() {
	if host.connection != nil {
		_ = host.connection.Close()
	}
	host.connection = nil
	host.client = nil
}

func processDone(process *managedProcess) bool {
	select {
	case <-process.done:
		return true
	default:
		return false
	}
}

func (host *Host) Close() error {
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.closed {
		return nil
	}
	host.closed = true
	host.supervise = false
	host.disconnect()
	if host.process != nil && host.process.command != nil && host.process.command.Process != nil {
		host.process.terminatedByOwner.Store(true)
		_ = terminateManagedProcess(host.process.command)
		<-host.process.done
	}
	if host.process != nil {
		_ = clearOwnerRecord(host.process.ownerPath, host.process.commandPid(), host.process.socketPath)
	}
	return nil
}

func (process *managedProcess) commandPid() int {
	if process == nil || process.command == nil || process.command.Process == nil {
		return 0
	}
	return process.command.Process.Pid
}
