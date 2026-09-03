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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/codexhost"
	"github.com/ceasarXuu/RA2A/internal/control"
	"github.com/ceasarXuu/RA2A/internal/desktopipc"
	"github.com/ceasarXuu/RA2A/internal/lannode"
	"github.com/ceasarXuu/RA2A/internal/mcpserver"
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
	desktopSend messageSender
}

type messageSender func(context.Context, string, string) error

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
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".codex", "app-server-control", "app-server-control.sock")
		}
		codexHome = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(codexHome, "app-server-control", "app-server-control.sock")
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
	return sendWithDesktopPreference(ctx, target, prompt, source.host.SendMessage, source.desktopSend)
}

func sendWithDesktopPreference(ctx context.Context, target, prompt string, managed, desktop messageSender) error {
	if desktop == nil {
		return managed(ctx, target, prompt)
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
	managedErr := managed(ctx, target, prompt)
	if managedErr != nil {
		return fmt.Errorf("%w; Desktop owner route unavailable: %v", managedErr, desktopErr)
	}
	return nil
}

func sendDesktopMessage(ctx context.Context, target, prompt string) error {
	connection, _, err := desktopipc.DialContext(ctx, "")
	if err != nil {
		return &desktopipc.NotDeliveredError{Cause: err}
	}
	defer connection.Close()
	client := desktopipc.New(connection)
	if err := client.Initialize(ctx); err != nil {
		return &desktopipc.NotDeliveredError{Cause: err}
	}
	_, err = client.SendMessage(ctx, target, prompt, desktopipc.NewMessageID())
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
