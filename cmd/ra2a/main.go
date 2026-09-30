package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/codexapp"
	"github.com/ceasarXuu/RA2A/internal/codexhost"
	"github.com/ceasarXuu/RA2A/internal/control"
	"github.com/ceasarXuu/RA2A/internal/desktopipc"
	"github.com/ceasarXuu/RA2A/internal/lannode"
	"github.com/ceasarXuu/RA2A/internal/mailbox"
	"github.com/ceasarXuu/RA2A/internal/mcpserver"
	"github.com/ceasarXuu/RA2A/internal/ochost"
	"github.com/ceasarXuu/RA2A/internal/operator"
)

type sessionSource interface {
	ListSessions(context.Context) ([]lannode.Session, error)
	SendMessage(context.Context, string, string) error
	Close() error
}

type sessionSourceFactory func(context.Context, string, string, io.Writer) (sessionSource, error)

type codexSessionSource struct {
	host        *codexhost.Host
	desktopSend desktopMessageSender
}

// codexAppBridge adapts the injected Codex App session source to the adapter
// contract, keeping the existing source factory as the single injection point.
type codexAppBridge struct{ source sessionSource }

func (bridge codexAppBridge) ListSessions(ctx context.Context) ([]codexapp.Session, error) {
	sessions, err := bridge.source.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	converted := make([]codexapp.Session, 0, len(sessions))
	for _, session := range sessions {
		converted = append(converted, codexapp.Session{ID: session.ID, Title: session.Title, Status: session.Status})
	}
	return converted, nil
}

func (bridge codexAppBridge) SendMessage(ctx context.Context, target, prompt string) error {
	return bridge.source.SendMessage(ctx, target, prompt)
}

func (bridge codexAppBridge) Close() error { return bridge.source.Close() }

type messageSender func(context.Context, string, string) error
type desktopMessageSender func(context.Context, string, string, desktopipc.StartModelResolver) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		err = runMCP(ctx, os.Args[2:], os.Stdin, os.Stdout)
	} else {
		err = run(ctx, os.Args[1:], os.Stdout, startCodexSessionSource)
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
	case "opencode-mcp-unregister":
		return operator.UnregisterOpenCodeMCP()
	case "opencode-server-cleanup":
		return ochost.CleanupShared(operator.OpenCodeOwnerPath())
	case "mailbox":
		return runMailbox(args[1:], output)
	case "adopt-cli", "release-cli":
		if len(args) != 2 || args[1] == "" {
			return fmt.Errorf("%s requires an id", args[0])
		}
		config, err := applySessionCommand(args[0], args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "cli-session=%s published=%d\n", args[1], len(config.CLISessions))
		return nil
	case "opencode":
		return runOpencodeAttach(ctx, args[1:], output)
	case "setup":
		flags := flag.NewFlagSet("setup", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		pin := flags.String("pin", "", "shared six-character PIN")
		id := flags.String("node-id", "", "stable node ID")
		name := flags.String("name", "", "display name")
		codex := flags.String("codex", "", "Codex executable")
		opencodePath := flags.String("opencode", "", "OpenCode executable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		config := operator.Config{NodeID: *id, Name: *name, PIN: *pin, Codex: *codex, OpenCode: *opencodePath}
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
		return errors.New("usage: ra2a <setup|restart|stop|exit|name|pin|version|update|adopt-cli|release-cli|mailbox|opencode|selftest|serve|send> [options]")
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

	adopted, err := adoptedCLISessions()
	if err != nil {
		return err
	}
	store, err := openMailboxStore()
	if err != nil {
		return err
	}
	registry, err := buildRegistry(ctx, *id, *codexPath, *appServerSocket, os.Stderr, startSource, adopted)
	if err != nil {
		return err
	}
	defer registry.Close()
	node, err := lannode.Start(ctx, lannode.Config{
		ID: *id, Name: *name, PIN: *pin, Sessions: registryAdapter{registry: registry}.ListSessions,
		SendMessage: func(ctx context.Context, message lannode.Message) error {
			return deliverOverLAN(ctx, *id, registry, store, message)
		},
	})
	if err != nil {
		return err
	}
	defer node.Close()

	if args[0] == "serve" {
		coordinator := control.NewAdapterCoordinator(*id, node, registryAdapter{registry: registry})
		store, err := openMailboxStore()
		if err != nil {
			return err
		}
		coordinator = coordinator.WithMailbox(store)
		if err := control.Start(ctx, *controlAddress, coordinator, store); err != nil {
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

// buildRegistry wires one adapter per supported agent. Adding an agent means
// registering one more adapter here; the router, LAN layer and MCP layer stay
// unchanged.
// runMailbox is the client side of the mailbox. It deliberately uses only the
// loopback control plane, so any harness can read its mail without an adapter,
// a live session or a model call.
func runMailbox(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("mailbox requires read, send or list")
	}
	flags := flag.NewFlagSet("mailbox", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	controlURL := flags.String("control-url", control.DefaultEndpoint, "local RA2A daemon control URL")
	recipient := flags.String("to", "", "mailbox recipient")
	from := flags.String("from", "", "sender address recorded on the message")
	text := flags.String("message", "", "message text (send only)")
	peek := flags.Bool("peek", false, "read without marking messages as read")
	limit := flags.Int("limit", 0, "maximum messages to return")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	client := control.NewClient(*controlURL)
	switch args[0] {
	case "list":
		names, err := client.ListMailboxes(context.Background())
		if err != nil {
			return err
		}
		for _, name := range names {
			fmt.Fprintln(output, name)
		}
		return nil
	case "read":
		if *recipient == "" {
			return errors.New("mailbox read requires --to")
		}
		result, err := client.ReadMailbox(context.Background(), control.MailboxReadRequest{
			To: *recipient, Limit: *limit, Peek: *peek,
		})
		if err != nil {
			return err
		}
		for _, message := range result.Messages {
			fmt.Fprintf(output, "--- %s from %s at %s\n%s\n",
				message.ID, message.From, message.SentAt.Format(time.RFC3339), message.Text)
		}
		fmt.Fprintf(output, "pending=%d returned=%d\n", result.Pending, len(result.Messages))
		return nil
	case "send":
		if *recipient == "" || *text == "" {
			return errors.New("mailbox send requires --to and --message")
		}
		return client.SendMailbox(context.Background(), control.MailboxSendRequest{
			To: *recipient, Text: *text, From: *from,
		})
	default:
		return fmt.Errorf("unknown mailbox command %q", args[0])
	}
}

func applySessionCommand(command, id string) (operator.Config, error) {
	switch command {
	case "adopt-cli":
		return operator.AdoptCLISession(id)
	case "release-cli":
		return operator.ReleaseCLISession(id)
	}
	return operator.Config{}, fmt.Errorf("unknown session command %q", command)
}

// openMailboxStore places the mailbox under the RA2A config directory so it
// survives restarts and inherits the config directory's private permissions.
func openMailboxStore() (*mailbox.Store, error) {
	path, err := operator.MailboxPath()
	if err != nil {
		return nil, err
	}
	store, err := mailbox.OpenStore(path)
	if err != nil {
		return nil, fmt.Errorf("open mailbox store: %w", err)
	}
	return store, nil
}

// adoptedCLISessions reads the operator-recorded CLI thread IDs. A missing or
// unreadable config simply publishes nothing rather than failing daemon start.
func adoptedCLISessions() ([]string, error) {
	config, err := operator.Load()
	if err != nil {
		return nil, nil
	}
	return config.CLISessions, nil
}

func defaultAppServerSocket() string {
	return codexhost.DefaultSocketPath()
}

func startCodexSessionSource(ctx context.Context, codexPath, appServerSocket string, stderr io.Writer) (sessionSource, error) {
	host, err := codexhost.Start(ctx, codexhost.Config{
		CodexPath: codexPath, SocketPath: appServerSocket, Stderr: stderr,
	})
	if err != nil {
		return nil, err
	}
	return &codexSessionSource{host: host, desktopSend: sendDesktopMessage}, nil
}

func (source *codexSessionSource) SendMessage(ctx context.Context, target, prompt string) error {
	var desktop messageSender
	if source.desktopSend != nil {
		desktop = func(ctx context.Context, target, prompt string) error {
			return source.desktopSend(ctx, target, prompt, source.host.ResolveThreadModel)
		}
	}
	return sendWithDesktopPreference(ctx, target, prompt, source.host.SendMessage, desktop)
}

func sendWithDesktopPreference(ctx context.Context, target, prompt string, managed, desktop messageSender) error {
	if desktop == nil {
		return fmt.Errorf("%w: start Codex Desktop and retry", control.ErrDesktopOwnerUnavailable)
	}
	desktopErr := desktop(ctx, target, prompt)
	if desktopErr == nil {
		return nil
	}
	if desktopipc.IsDeliveryUnknown(desktopErr) {
		return fmt.Errorf("%w: %v", control.ErrDeliveryUnknown, desktopErr)
	}
	if !desktopipc.IsNotDelivered(desktopErr) {
		return desktopErr
	}
	return fmt.Errorf("%w: start Codex Desktop and retry: %v", control.ErrDesktopOwnerUnavailable, desktopErr)
}

func sendDesktopMessage(ctx context.Context, target, prompt string, resolveModel desktopipc.StartModelResolver) error {
	connection, _, err := desktopipc.DialContext(ctx, "")
	if err != nil {
		return &desktopipc.NotDeliveredError{Cause: err}
	}
	defer connection.Close()
	client := desktopipc.New(connection)
	if err := client.Initialize(ctx); err != nil {
		return &desktopipc.NotDeliveredError{Cause: err}
	}
	_, err = client.SendMessage(ctx, target, prompt, desktopipc.NewMessageID(), resolveModel)
	return err
}

func (source *codexSessionSource) ListSessions(ctx context.Context) ([]lannode.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	threads, err := source.host.ListThreadSummaries(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]lannode.Session, 0, len(threads))
	for _, thread := range threads {
		sessions = append(sessions, lannode.Session{
			ID: thread.ID, Title: thread.Title, Status: thread.Status,
		})
	}
	return sessions, nil
}

func (source *codexSessionSource) Close() error {
	return source.host.Close()
}
