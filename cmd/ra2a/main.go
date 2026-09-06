package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/codexapp"
	"github.com/ceasarXuu/RA2A/internal/codexhost"
	"github.com/ceasarXuu/RA2A/internal/control"
	"github.com/ceasarXuu/RA2A/internal/lannode"
	"github.com/ceasarXuu/RA2A/internal/mcpserver"
	"github.com/ceasarXuu/RA2A/internal/operator"
)

type sessionSource interface {
	ListSessions(context.Context) ([]lannode.Session, error)
	ListEndpoints(context.Context) ([]lannode.EndpointView, error)
	SendMessage(context.Context, string, string) error
	Close() error
}

type sessionSourceFactory func(context.Context, string, string, io.Writer) (sessionSource, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		err = runMCP(ctx, os.Args[2:], os.Stdin, os.Stdout)
	} else {
		err = run(ctx, os.Args[1:], os.Stdout, startRegistrySessionSource)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMCP(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	controlURL := flags.String("control-url", control.DefaultEndpoint, "local RA2A daemon control URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return mcpserver.Serve(ctx, input, output, control.NewClient(*controlURL))
}

func run(ctx context.Context, args []string, output io.Writer, startSource sessionSourceFactory) error {
	if len(args) == 0 {
		return operator.SetupInteractive(os.Stdin, output)
	}
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(output, operator.Version)
		return nil
	}
	switch args[0] {
	case "name", "pin":
		value, err := commandValue(args, os.Stdin, output)
		if err != nil {
			return err
		}
		config, err := operator.Set(args[0], value)
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "name: %s\nstatus: running\n", config.Name)
		return nil
	case "restart":
		config, err := operator.Restart()
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "name: %s\nstatus: running\n", config.Name)
		return nil
	case "stop":
		config, err := operator.Stop()
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "name: %s\nstatus: paused\n", config.Name)
		return nil
	case "exit":
		config, err := operator.Exit()
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "name: %s\nstatus: exited\n", config.Name)
		return nil
	case "setup":
		flags := flag.NewFlagSet("setup", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		pin := flags.String("pin", "", "shared six-character PIN")
		id := flags.String("node-id", "", "stable node ID")
		name := flags.String("name", "", "display name")
		codex := flags.String("codex", "", "Codex executable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		config := operator.Config{NodeID: *id, Name: *name, PIN: *pin, Codex: *codex}
		if err := operator.Setup(config); err != nil {
			return err
		}
		fmt.Fprintf(output, "name: %s\nPIN: %s\nstatus: running\n", config.Name, config.PIN)
		return nil
	case "update":
		version, changed, deferred, err := operator.Update(ctx)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Fprintf(output, "already up to date: %s\n", version)
			return nil
		}
		if !deferred {
			if _, err := operator.Restart(); err != nil {
				return fmt.Errorf("updated to %s but restart failed: %w", version, err)
			}
		}
		fmt.Fprintf(output, "updated: %s\nstatus: running\n", version)
		return nil
	case "daemon":
		config, err := operator.Load()
		if err != nil {
			return fmt.Errorf("load daemon config: %w", err)
		}
		controlAddress := os.Getenv("RA2A_CONTROL_ADDRESS")
		if controlAddress == "" {
			controlAddress = "127.0.0.1:47321"
		}
		return run(ctx, []string{"serve", "--pin", config.PIN, "--id", config.NodeID, "--name", config.Name, "--codex", config.Codex, "--control-address", controlAddress}, output, startSource)
	}
	if len(args) == 0 || (args[0] != "selftest" && args[0] != "serve" && args[0] != "send") {
		return errors.New("usage: ra2a <setup|restart|stop|exit|name|pin|version|update|selftest|serve|send> [options]")
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pin := flags.String("pin", "", "shared 6-character PIN")
	id := flags.String("id", "", "node ID")
	name := flags.String("name", "", "node name")
	codexPath := flags.String("codex", "codex", "path to the Codex CLI binary")
	appServerSocket := flags.String("app-server-socket", defaultAppServerSocket(), "managed Codex App Server control socket")
	peerID := flags.String("peer", "", "destination RA2A node ID (send only)")
	targetSessionID := flags.String("session", "", "destination Codex session ID (send only)")
	text := flags.String("message", "", "message text (send only)")
	controlAddress := flags.String("control-address", "127.0.0.1:47321", "loopback MCP control address (serve only)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *id == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("read hostname: %w", err)
		}
		*id = hostname
	}
	if *name == "" {
		*name = *id
	}

	source, err := startSource(ctx, *codexPath, *appServerSocket, os.Stderr)
	if err != nil {
		return fmt.Errorf("start managed Codex App Server: %w", err)
	}
	defer source.Close()
	node, err := lannode.Start(ctx, lannode.Config{
		ID: *id, Name: *name, PIN: *pin, Sessions: source.ListSessions,
		Endpoints: func(ctx context.Context) ([]lannode.EndpointView, error) {
			views, err := source.ListEndpoints(ctx)
			if err != nil {
				return nil, err
			}
			prefixed := make([]lannode.EndpointView, 0, len(views))
			for _, view := range views {
				view.Address = "ra2a://" + *id + "/" + view.Address
				prefixed = append(prefixed, view)
			}
			return prefixed, nil
		},
		SendMessage: func(ctx context.Context, message lannode.Message) error {
			return source.SendMessage(ctx, message.TargetSessionID, formatIncomingMessage(message))
		},
	})
	if err != nil {
		return err
	}
	defer node.Close()

	if args[0] == "serve" {
		if err := control.Start(ctx, *controlAddress, control.NewCoordinator(*id, node)); err != nil {
			return err
		}
		fmt.Fprintf(output, "node=ra2a://%s status=running\n", *id)
		<-ctx.Done()
		return nil
	}

	operationContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	wantedPeer := *id
	if args[0] == "send" {
		if *peerID == "" || *targetSessionID == "" || *text == "" {
			return errors.New("send requires --peer, --session, and --message")
		}
		wantedPeer = *peerID
	}
	peer, err := node.WaitForPeer(operationContext, wantedPeer)
	if err != nil {
		return err
	}
	if args[0] == "send" {
		err := node.SendMessage(operationContext, peer, lannode.Message{
			TargetSessionID: *targetSessionID,
			Text:            *text,
			Source:          "ra2a://" + *id,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "delivered=ra2a://%s/%s\n", peer.ID, *targetSessionID)
		return nil
	}
	fmt.Fprintf(output, "discovered=ra2a://%s endpoint=%s\n", peer.ID, peer.Address)
	sessions, err := node.ListSessions(operationContext, peer)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "sessions=%d\nselftest=ok\n", len(sessions))
	return nil
}

func commandValue(args []string, input io.Reader, output io.Writer) (string, error) {
	if len(args) > 2 {
		return "", fmt.Errorf("%s accepts at most one value", args[0])
	}
	if len(args) == 2 {
		return args[1], nil
	}
	fmt.Fprintf(output, "%s: ", args[0])
	value, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func formatIncomingMessage(message lannode.Message) string {
	var prompt strings.Builder
	prompt.WriteString("[RA2A message]\n")
	if message.Source != "" {
		fmt.Fprintf(&prompt, "from: %s\n", message.Source)
	}
	if message.MessageID != "" {
		fmt.Fprintf(&prompt, "message-id: %s\n", message.MessageID)
	}
	prompt.WriteString("\n")
	prompt.WriteString(message.Text)
	return prompt.String()
}

func defaultAppServerSocket() string {
	return codexhost.DefaultSocketPath()
}

func startRegistrySessionSource(ctx context.Context, codexPath, appServerSocket string, stderr io.Writer) (sessionSource, error) {
	adapter, err := codexapp.New(ctx, codexhost.Config{
		CodexPath: codexPath, SocketPath: appServerSocket, Stderr: stderr,
	}, true /* desktopEnabled */)
	if err != nil {
		return nil, err
	}
	registry := agentbridge.NewRegistry()
	if err := registry.Register(adapter); err != nil {
		_ = adapter.Close()
		return nil, err
	}
	return &registrySessionSource{registry: registry}, nil
}

// sourceRegistry is the slice of agentbridge.Registry the session source
// needs; it is an interface so main tests can inject a fake.
type sourceRegistry interface {
	ListEndpoints(context.Context) ([]agentbridge.EndpointRef, error)
	Deliver(context.Context, string, string) (agentbridge.DeliveryResult, error)
	Close() error
}

// registrySessionSource adapts the agentbridge Registry world to the legacy
// LAN sessionSource seam. Endpoint addresses are adapter-local
// ("<agent>/<id>"); they gain a node prefix only when published over the LAN
// and lose it again when a message arrives for this node.
type registrySessionSource struct {
	registry sourceRegistry
}

func (source *registrySessionSource) ListSessions(ctx context.Context) ([]lannode.Session, error) {
	refs, err := source.registry.ListEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	return sessionsFromRefs(refs), nil
}

// ListEndpoints projects registry endpoints onto LAN endpoint views with
// adapter-local addresses ("<agent>/<id>"). The daemon adds its node prefix
// when publishing the views over the LAN.
func (source *registrySessionSource) ListEndpoints(ctx context.Context) ([]lannode.EndpointView, error) {
	refs, err := source.registry.ListEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	return endpointViewsFromRefs(refs), nil
}

func (source *registrySessionSource) SendMessage(ctx context.Context, target, prompt string) error {
	address := localAddress(target)
	result, err := source.registry.Deliver(ctx, address, prompt)
	if err != nil {
		return err
	}
	return deliveryResultToError(result, target)
}

func (source *registrySessionSource) Close() error {
	return source.registry.Close()
}

// endpointViewsFromRefs projects registry endpoints onto LAN endpoint views.
// Each address stays adapter-local ("<agent>/<id>"); the daemon prefixes its
// node ID before publishing so peers can round-trip the full target.
func endpointViewsFromRefs(refs []agentbridge.EndpointRef) []lannode.EndpointView {
	views := make([]lannode.EndpointView, 0, len(refs))
	for _, ref := range refs {
		capabilities := make([]string, 0, len(ref.Endpoint.Capabilities))
		for capability := range ref.Endpoint.Capabilities {
			capabilities = append(capabilities, string(capability))
		}
		views = append(views, lannode.EndpointView{
			ID:           ref.Endpoint.EndpointID,
			AgentType:    string(ref.Endpoint.AgentType),
			Capabilities: capabilities,
			Address:      ref.Endpoint.Address,
		})
	}
	return views
}

// localAddress normalizes an incoming target into the adapter-local address
// space. Full LAN addresses (ra2a://<node>/<agent>/<id>) shed their node
// prefix; legacy bare session IDs (from older peers or the CLI --session
// flag) fall back to the default agent family.
func localAddress(target string) string {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "ra2a" || parsed.Host == "" {
		if target == "" {
			return target
		}
		return string(codexapp.AgentTypeCodexApp) + "/" + target
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0] + "/" + parts[1]
	}
	return target
}

// sessionsFromRefs projects registry endpoints onto the legacy LAN session
// model. Identity stays native (Session.ID == thread ID).
func sessionsFromRefs(refs []agentbridge.EndpointRef) []lannode.Session {
	sessions := make([]lannode.Session, 0, len(refs))
	for _, ref := range refs {
		status := ref.Endpoint.NativeStatus
		if status == "" {
			status = string(ref.Endpoint.Status)
		}
		sessions = append(sessions, lannode.Session{
			ID: ref.Endpoint.EndpointID, Title: ref.Endpoint.Title, Status: status,
			AgentType: string(ref.Endpoint.AgentType),
		})
	}
	return sessions
}

// deliveryResultToError converts an agentbridge outcome back into the
// LAN/control error vocabulary that lannode and the MCP control HTTP handler
// key on (sentinel wrapping plus SESSION_BUSY / TARGET_NOT_FOUND substrings).
func deliveryResultToError(result agentbridge.DeliveryResult, target string) error {
	switch result.Code {
	case agentbridge.Delivered:
		return nil
	case agentbridge.StartRequired:
		return fmt.Errorf("%w: %s", control.ErrDesktopOwnerUnavailable, result.Detail)
	case agentbridge.DeliveryUnknown:
		return fmt.Errorf("%w: %s", control.ErrDeliveryUnknown, result.Detail)
	case agentbridge.Busy:
		return fmt.Errorf("%w: %s", codexhost.ErrSessionBusy, result.Detail)
	case agentbridge.NotFound:
		return fmt.Errorf("TARGET_NOT_FOUND: no session %q: %s", target, result.Detail)
	default: // unreachable, unsupported, adapter_failed
		return fmt.Errorf("%s: %s", result.Code, result.Detail)
	}
}

var _ sessionSource = (*registrySessionSource)(nil)
