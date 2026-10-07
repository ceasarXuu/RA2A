package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/codexapp"
	"github.com/ceasarXuu/RA2A/internal/codexcli"
	"github.com/ceasarXuu/RA2A/internal/control"
	"github.com/ceasarXuu/RA2A/internal/lannode"
	"github.com/ceasarXuu/RA2A/internal/mailbox"
	"github.com/ceasarXuu/RA2A/internal/ochost"
	"github.com/ceasarXuu/RA2A/internal/ocsession"
	"github.com/ceasarXuu/RA2A/internal/opencode"
	"github.com/ceasarXuu/RA2A/internal/operator"
	"github.com/ceasarXuu/RA2A/internal/pi"
)

var errOpenCodeDisabled = errors.New("opencode integration is disabled")

// opencodeSettings resolves the shared OpenCode server URL. A configured
// OpenCode harness may start after the daemon; the adapter stays registered.
func opencodeSettings() (url string, sessions []string) { //nolint:unparam // retained for existing callers
	if disabled, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("RA2A_DISABLE_OPENCODE"))); disabled {
		return "", nil
	}
	url = "http://127.0.0.1:4099"
	if override := strings.TrimSpace(os.Getenv("RA2A_OPENCODE_URL")); override != "" {
		url = override
	}
	if config, err := operator.Load(); err == nil {
		if config.OpenCode == "" {
			return "", nil
		}
		if config.OpenCodeURL != "" {
			url = config.OpenCodeURL
		}
	} else if _, err := exec.LookPath("opencode"); err != nil {
		return "", nil
	}
	return url, sessions
}

func startOpencodeAdapter(ctx context.Context, nodeID string, stderr io.Writer) (agentbridge.Adapter, error) {
	url, _ := opencodeSettings()
	if url == "" {
		return nil, errOpenCodeDisabled
	}
	adapter := opencode.New(nodeID, opencode.Config{BaseURL: url, Stderr: stderr, ClientName: "ra2a"}, stderr)
	adapter.Watch(ctx)
	return adapter, nil
}

func runOpencodeAttach(ctx context.Context, args []string, output io.Writer) error {
	url, _ := opencodeSettings()
	host, err := ochost.Start(ctx, ochost.Config{
		Executable: opencodeExecutable(), URL: url, Stderr: os.Stderr,
		OwnerPath: opencodeOwnerPath(), ReadinessTimeout: 25 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("prepare the shared OpenCode server: %w", err)
	}
	defer func() { _ = host.Close() }()
	fmt.Fprintf(output, "opencode-server=%s owned=%v\n", host.URL(), host.Owned())
	_, attach, err := ocsession.Select(ctx, host.URL(), args)
	if err != nil {
		return fmt.Errorf("select OpenCode session: %w", err)
	}
	focusConfig, release, err := ocsession.PrepareFocus(ocsession.Directory())
	if err != nil {
		return fmt.Errorf("prepare OpenCode focus tracking: %w", err)
	}
	defer release()
	command := exec.CommandContext(ctx, opencodeExecutable(), append([]string{"attach", host.URL()}, attach...)...)
	command.Env = append(os.Environ(), "OPENCODE_TUI_CONFIG="+focusConfig)
	command.Stdin = os.Stdin
	command.Stdout = output
	command.Stderr = os.Stderr
	return command.Run()
}

func opencodeExecutable() string {
	if override := strings.TrimSpace(os.Getenv("RA2A_OPENCODE_BINARY")); override != "" {
		return override
	}
	return "opencode"
}

func opencodeOwnerPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ra2a", "opencode-owner.json")
}

func buildRegistry(ctx context.Context, nodeID, codexPath, appServerSocket string, stderr io.Writer, startSource sessionSourceFactory, cliSessions []string) (*agentbridge.Registry, error) {
	registry := agentbridge.NewRegistry(nodeID)
	if err := registry.Register(pi.New(nodeID, pi.Directory())); err != nil {
		return nil, err
	}
	if codexPath != "" {
		source, err := startSource(ctx, codexPath, appServerSocket, stderr)
		if err != nil {
			return nil, fmt.Errorf("start managed Codex App Server: %w", err)
		}
		cliAdapter := codexcli.New(nodeID, codexcli.Config{CodexPath: codexPath, Stderr: stderr})
		for _, threadID := range cliSessions {
			if err := cliAdapter.Register(threadID); err != nil {
				fmt.Fprintf(stderr, "skip cli session %q: %v\n", threadID, err)
				continue
			}
		}
		appAdapter := codexapp.New(nodeID, codexAppBridge{source: source}, stderr, cliAdapter.Registered()...)
		if err := registry.Register(appAdapter); err != nil {
			_ = source.Close()
			return nil, err
		}
		if err := registry.Register(cliAdapter); err != nil {
			_ = source.Close()
			return nil, err
		}
	}
	opencodeAdapter, err := startOpencodeAdapter(ctx, nodeID, stderr)
	if err != nil {
		if !errors.Is(err, errOpenCodeDisabled) {
			fmt.Fprintf(stderr, "opencode integration unavailable: %v\n", err)
		}
	} else if err := registry.Register(opencodeAdapter); err != nil {
		_ = registry.Close()
		return nil, err
	}
	return registry, nil
}

type registryAdapter struct{ registry *agentbridge.Registry }

func (wrapper registryAdapter) ListSessions(ctx context.Context) ([]lannode.Session, error) {
	endpoints, err := wrapper.Endpoints(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]lannode.Session, 0, len(endpoints))
	for _, endpoint := range endpoints {
		capabilities := make([]string, 0, len(endpoint.Capabilities))
		for _, capability := range endpoint.Capabilities {
			capabilities = append(capabilities, string(capability))
		}
		sessions = append(sessions, lannode.Session{
			ID: endpoint.Address.EndpointID, Address: endpoint.Address.String(), Title: endpoint.Title,
			Status: string(endpoint.Status), Agent: string(endpoint.Agent), Capabilities: capabilities,
		})
	}
	return sessions, nil
}

func (wrapper registryAdapter) Endpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	endpoints, problems := wrapper.registry.Endpoints(ctx)
	for _, problem := range problems {
		fmt.Fprintf(os.Stderr, "endpoint problem: %v\n", problem)
	}
	return endpoints, nil
}

func (wrapper registryAdapter) Deliver(ctx context.Context, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	return wrapper.registry.Deliver(ctx, envelope)
}

func (wrapper registryAdapter) Health(ctx context.Context) map[agentbridge.AgentKind]agentbridge.Health {
	return wrapper.registry.Health(ctx)
}

func (wrapper registryAdapter) Lookup(ctx context.Context, address agentbridge.Address) (agentbridge.Endpoint, agentbridge.Adapter, error) {
	return wrapper.registry.Lookup(ctx, address)
}

func (wrapper registryAdapter) ResolveCaller(ctx context.Context, caller agentbridge.CallerContext) (agentbridge.Address, error) {
	return wrapper.registry.ResolveCaller(ctx, caller)
}

func deliverOverLAN(ctx context.Context, nodeID string, registry *agentbridge.Registry, store *mailbox.Store, message lannode.Message) error {
	envelope := agentbridge.MessageEnvelope{
		ID: message.MessageID, ProtocolVersion: agentbridge.ProtocolVersion,
		SourceAddress: message.Source, TargetAddress: "ra2a://" + nodeID + "/" + message.TargetSessionID,
		Text: message.Text, CreatedAt: time.Now().UTC(),
	}
	if result, handled := control.DeliverMailbox(store, nodeID, envelope); handled {
		if result.Delivered() {
			return nil
		}
		return fmt.Errorf("%w: %s", control.ErrDeliveryUnknown, result.Detail)
	}
	if envelope.ID == "" {
		envelope.ID = message.TargetSessionID + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	result := registry.Deliver(ctx, envelope)
	if result.Delivered() {
		return nil
	}
	if result.Code == agentbridge.ResultStartRequired {
		return fmt.Errorf("%w: %s", control.ErrStartRequired, result.Detail)
	}
	if result.Code == agentbridge.ResultNotFound {
		return fmt.Errorf("%w: %s", control.ErrTargetNotFound, result.Detail)
	}
	return fmt.Errorf("%w: %s", control.ErrDeliveryUnknown, result.Detail)
}
