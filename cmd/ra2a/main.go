package main

import (
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
	"github.com/ceasarXuu/RA2A/internal/lannode"
)

type sessionSource interface {
	ListSessions(context.Context) ([]lannode.Session, error)
	SendMessage(context.Context, string, string) error
	Close() error
}

type sessionSourceFactory func(context.Context, string, string, io.Writer) (sessionSource, error)

type codexSessionSource struct {
	host *codexhost.Host
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, startCodexSessionSource); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer, startSource sessionSourceFactory) error {
	if len(args) == 0 || (args[0] != "selftest" && args[0] != "serve" && args[0] != "send") {
		return errors.New("usage: ra2a <selftest|serve|send> --pin <6-character-pin> [--id <node-id>] [--name <node-name>] [--codex <path>]")
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
	return &codexSessionSource{host: host}, nil
}

func (source *codexSessionSource) SendMessage(ctx context.Context, target, prompt string) error {
	return source.host.SendMessage(ctx, target, prompt)
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
