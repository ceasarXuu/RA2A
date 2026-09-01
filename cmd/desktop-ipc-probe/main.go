package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ceasarXuu/RA2A/internal/desktopipc"
)

type options struct {
	threadID   string
	message    string
	messageID  string
	allowWrite bool
}

type turnStarter interface {
	StartTurn(context.Context, string, string, string) (desktopipc.TurnResult, error)
}

func run(ctx context.Context, starter turnStarter, opts options, output io.Writer) error {
	if !opts.allowWrite {
		return errors.New("refusing to inject through Desktop IPC without --allow-write")
	}
	if opts.threadID == "" || opts.message == "" {
		return errors.New("--thread-id and --message are required")
	}
	if opts.messageID == "" {
		opts.messageID = fmt.Sprintf("ra2a-probe-%d", time.Now().UnixNano())
	}
	result, err := starter.StartTurn(ctx, opts.threadID, opts.message, opts.messageID)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]string{
		"status":    "accepted",
		"threadId":  opts.threadID,
		"turnId":    result.TurnID,
		"messageId": opts.messageID,
	})
}

func main() {
	var opts options
	var socketPath string
	var timeout time.Duration
	flag.StringVar(&opts.threadID, "thread-id", "", "Desktop-owned target thread ID")
	flag.StringVar(&opts.message, "message", "", "text to inject")
	flag.StringVar(&opts.messageID, "message-id", "", "stable message ID")
	flag.StringVar(&socketPath, "socket", "", "override Codex Desktop IPC socket path")
	flag.DurationVar(&timeout, "timeout", 15*time.Second, "connection and request timeout")
	flag.BoolVar(&opts.allowWrite, "allow-write", false, "allow the probe to start a Desktop turn")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, connectedPath, err := desktopipc.DialContext(ctx, socketPath)
	if err != nil {
		exitError(err)
	}
	defer conn.Close()
	client := desktopipc.New(conn)
	if err := client.Initialize(ctx); err != nil {
		exitError(err)
	}
	fmt.Fprintf(os.Stderr, "Connected to Codex Desktop IPC: %s\n", connectedPath)
	if err := run(ctx, client, opts, os.Stdout); err != nil {
		exitError(err)
	}
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
