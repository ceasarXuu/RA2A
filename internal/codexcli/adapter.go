package codexcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

// MinAppServerVersion is the lowest Codex app-server whose delivery contract has
// been verified against this adapter.
const MinAppServerVersion = "0.158.0"

const adapterClientName = "ra2a_codex_cli"

type Config struct {
	CodexPath   string
	CodexHome   string
	Stderr      io.Writer
	CallTimeout time.Duration
	MinVersion  string
}

type Adapter struct {
	config        Config
	logger        *slog.Logger
	nodeID        string
	connectMu     sync.Mutex
	mu            sync.Mutex
	conn          *rpcConn
	server        *appServer
	serverVersion string
	codexHome     string
	registered    map[string]struct{}
	closed        bool
}

func New(nodeID string, config Config) *Adapter {
	if config.CallTimeout == 0 {
		config.CallTimeout = 20 * time.Second
	}
	if config.MinVersion == "" {
		config.MinVersion = MinAppServerVersion
	}
	if config.CodexHome == "" {
		config.CodexHome = defaultCodexHome()
	}
	return &Adapter{
		config: config, nodeID: nodeID,
		logger:     slog.New(slog.NewTextHandler(config.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		codexHome:  config.CodexHome,
		registered: make(map[string]struct{}),
	}
}

func (adapter *Adapter) Kind() agentbridge.AgentKind { return agentbridge.AgentCodexCLI }

// Register records a native thread as reachable through this adapter. Ownership
// is established only through RA2A's own verified access boundary; the adapter
// never infers it from Thread.source or Thread.originator, both of which the
// app-server sets process-wide and cannot distinguish clients.
func (adapter *Adapter) Register(nativeThreadID string) error {
	if !validThreadID(nativeThreadID) {
		return fmt.Errorf("register thread %q: not a valid thread id", nativeThreadID)
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.registered[nativeThreadID] = struct{}{}
	return nil
}

func (adapter *Adapter) Registered() []string {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	ids := make([]string, 0, len(adapter.registered))
	for id := range adapter.registered {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// EnsureThread creates a thread through this adapter and registers it. It is the
// only ownership source that needs no operator input.
func (adapter *Adapter) EnsureThread(ctx context.Context, cwd, model string) (string, error) {
	server, err := adapter.connect(ctx)
	if err != nil {
		return "", err
	}
	thread, err := server.threadStart(ctx, cwd, model)
	if err != nil {
		return "", fmt.Errorf("start thread: %w", err)
	}
	if err := adapter.Register(thread.ID); err != nil {
		return "", err
	}
	adapter.logger.Info("cli_caller_bound", "endpoint_id", thread.ID, "origin", "adapter_created")
	return thread.ID, nil
}

func (adapter *Adapter) connect(ctx context.Context) (*appServer, error) {
	adapter.connectMu.Lock()
	defer adapter.connectMu.Unlock()
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return nil, errors.New("adapter is closed")
	}
	if adapter.server != nil {
		select {
		case <-adapter.conn.closed:
		default:
			server := adapter.server
			adapter.mu.Unlock()
			return server, nil
		}
	}
	stale := adapter.conn
	adapter.conn, adapter.server = nil, nil
	adapter.mu.Unlock()
	if stale != nil {
		_ = stale.Close()
	}

	daemon, err := detectDaemon(ctx, adapter.config.CodexPath, adapter.codexHome)
	if err != nil {
		return nil, err
	}
	if !daemon.Running {
		adapter.logger.Info("cli_daemon_state", "state", "absent", "detail", daemon.Detail)
		return nil, &StartRequiredError{Detail: daemon.Detail}
	}
	if problem := socketPathProblem(daemon.SocketPath); problem != "" {
		return nil, errors.New(problem)
	}
	conn, err := dialRPC(ctx, daemon.SocketPath, adapter.config.CallTimeout)
	if err != nil {
		adapter.logger.Info("cli_daemon_state", "state", "unreachable", "detail", err.Error())
		return nil, &StartRequiredError{Detail: "codex app-server control socket is not reachable"}
	}
	result, err := conn.initialize(ctx, adapterClientName, "0.0.0", true)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("initialize app-server: %w", err)
	}
	version := parseVersion(result.UserAgent)
	if version == "" {
		version = daemon.AppServerVersion
	}
	if !versionAtLeast(version, adapter.config.MinVersion) {
		_ = conn.Close()
		return nil, fmt.Errorf("codex app-server %s is below the supported minimum %s", version, adapter.config.MinVersion)
	}
	server := &appServer{conn: conn}
	// V10 measured that initialize writes clientInfo.name into the app-server
	// process-wide default originator unless the name is allow-listed, so every
	// thread created afterwards on this daemon records this client. The side
	// effect is cross-client and permanent for the daemon's lifetime; it is
	// logged once per connection so it can be traced during support work.
	adapter.logger.Info("cli_originator_side_effect",
		"client_name", adapterClientName, "app_server_version", version)

	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		_ = conn.Close()
		return nil, errors.New("adapter is closed")
	}
	adapter.conn = conn
	adapter.server = server
	adapter.serverVersion = version
	adapter.mu.Unlock()
	adapter.logger.Info("cli_daemon_state", "state", "running",
		"app_server_version", version, "codex_home", result.CodexHome)
	return server, nil
}

// StartRequiredError marks the state the product decision maps to
// start_required: the user has not started Codex, and RA2A must not start it.
type StartRequiredError struct{ Detail string }

func (err *StartRequiredError) Error() string {
	if err.Detail == "" {
		return "codex is not running"
	}
	return err.Detail
}

func (adapter *Adapter) Health(ctx context.Context) agentbridge.Health {
	daemon, err := detectDaemon(ctx, adapter.config.CodexPath, adapter.codexHome)
	if err != nil {
		return agentbridge.Unhealthy(agentbridge.ResultUnknown, err.Error())
	}
	if !daemon.Running {
		return agentbridge.Unhealthy(agentbridge.ResultStartRequired, daemon.Detail)
	}
	if problem := socketPathProblem(daemon.SocketPath); problem != "" {
		return agentbridge.Unhealthy(agentbridge.ResultUnsupported, problem)
	}
	return agentbridge.Ready()
}

func (adapter *Adapter) Close() error {
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return nil
	}
	adapter.closed = true
	conn := adapter.conn
	adapter.conn = nil
	adapter.server = nil
	adapter.mu.Unlock()
	if conn == nil {
		return nil
	}
	adapter.logger.Info("cli_unsubscribed", "scope", "connection")
	return conn.Close()
}
