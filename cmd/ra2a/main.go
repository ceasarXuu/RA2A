package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/appserverprobe"
	"github.com/ceasarXuu/RA2A/internal/lannode"
)

type sessionSource interface {
	ListSessions(context.Context) ([]lannode.Session, error)
	Close() error
}

type sessionSourceFactory func(context.Context, string, io.Writer) (sessionSource, error)

type codexSessionSource struct {
	command   *exec.Cmd
	input     io.WriteCloser
	client    *appserverprobe.Client
	callMu    sync.Mutex
	closeOnce sync.Once
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
	if len(args) == 0 || (args[0] != "selftest" && args[0] != "serve") {
		return errors.New("usage: ra2a <selftest|serve> --pin <6-character-pin> [--id <node-id>] [--name <node-name>] [--codex <path>]")
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pin := flags.String("pin", "", "shared 6-character PIN")
	id := flags.String("id", "", "node ID")
	name := flags.String("name", "", "node name")
	codexPath := flags.String("codex", "codex", "path to the Codex CLI binary")
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

	source, err := startSource(ctx, *codexPath, os.Stderr)
	if err != nil {
		return fmt.Errorf("start Codex App Server: %w", err)
	}
	defer source.Close()
	node, err := lannode.Start(ctx, lannode.Config{
		ID: *id, Name: *name, PIN: *pin, Sessions: source.ListSessions,
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

	selfTestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	peer, err := node.WaitForPeer(selfTestContext, *id)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "discovered=ra2a://%s endpoint=%s\n", peer.ID, peer.Address)
	sessions, err := node.ListSessions(selfTestContext, peer)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "sessions=%d\nselftest=ok\n", len(sessions))
	return nil
}

func startCodexSessionSource(ctx context.Context, codexPath string, stderr io.Writer) (sessionSource, error) {
	command := exec.CommandContext(ctx, codexPath, "app-server")
	input, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open App Server input: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("open App Server output: %w", err)
	}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("start %s app-server: %w", codexPath, err)
	}
	source := &codexSessionSource{
		command: command,
		input:   input,
		client:  appserverprobe.New(output, input),
	}
	if err := source.client.Initialize(); err != nil {
		_ = source.Close()
		return nil, fmt.Errorf("initialize App Server: %w", err)
	}
	return source, nil
}

func (source *codexSessionSource) ListSessions(ctx context.Context) ([]lannode.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source.callMu.Lock()
	defer source.callMu.Unlock()
	threads, err := source.client.ListThreadSummaries()
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
	source.closeOnce.Do(func() {
		_ = source.input.Close()
		if source.command.Process != nil {
			_ = source.command.Process.Kill()
		}
		_ = source.command.Wait()
	})
	return nil
}
