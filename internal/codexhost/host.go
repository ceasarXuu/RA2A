package codexhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ceasarXuu/RA2A/internal/appserverprobe"
)

type Config struct {
	CodexPath  string
	SocketPath string
	Stderr     io.Writer
}

type startFunc func(context.Context, Config) (*managedProcess, error)
type connectFunc func(context.Context, string) (io.ReadWriteCloser, error)

type managedProcess struct {
	command *exec.Cmd
	done    chan struct{}
}

type Host struct {
	config     Config
	start      startFunc
	connect    connectFunc
	retryDelay time.Duration

	mu         sync.Mutex
	process    *managedProcess
	connection io.ReadWriteCloser
	client     *appserverprobe.Client
	closed     bool
}

func Start(ctx context.Context, config Config) (*Host, error) {
	if config.CodexPath == "" || config.SocketPath == "" {
		return nil, errors.New("Codex path and App Server socket path are required")
	}
	return startWith(ctx, config, startManaged, connectWebSocket, 100*time.Millisecond)
}

func startManaged(ctx context.Context, config Config) (*managedProcess, error) {
	if err := os.MkdirAll(filepath.Dir(config.SocketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create App Server control directory: %w", err)
	}
	command := managedCommand(context.WithoutCancel(ctx), config)
	command.Stderr = config.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	process := &managedProcess{command: command, done: make(chan struct{})}
	go func() {
		_ = command.Wait()
		close(process.done)
	}()
	return process, nil
}

func managedCommand(ctx context.Context, config Config) *exec.Cmd {
	return exec.CommandContext(ctx, config.CodexPath, "app-server", "--listen", "unix://"+config.SocketPath)
}

func startWith(ctx context.Context, config Config, start startFunc, connect connectFunc, retryDelay time.Duration) (*Host, error) {
	host := &Host{config: config, start: start, connect: connect, retryDelay: retryDelay}
	host.mu.Lock()
	err := host.ensureConnected(ctx)
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
	}
	return err
}

func (host *Host) ensureConnected(ctx context.Context) error {
	if host.closed {
		return errors.New("Codex host is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if host.client != nil {
		return nil
	}
	if connection, err := host.connect(ctx, host.config.SocketPath); err == nil {
		return host.initialize(connection)
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
	host.disconnect()
	if host.process != nil && host.process.command != nil && host.process.command.Process != nil {
		_ = host.process.command.Process.Kill()
		<-host.process.done
	}
	return nil
}
