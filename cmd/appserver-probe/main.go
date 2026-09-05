package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/ceasarXuu/RA2A/internal/appserverprobe"
	"github.com/ceasarXuu/RA2A/internal/codexhost"
)

type options struct {
	threadID         string
	message          string
	queueMessage     string
	ephemeralMessage string
	cwd              string
	allowWrite       bool
	probeContext     bool
	sourceKinds      []string
}

func run(client *appserverprobe.Client, opts options, output io.Writer) error {
	if opts.ephemeralMessage != "" {
		if opts.threadID != "" || opts.message != "" || opts.probeContext || opts.queueMessage != "" {
			return errors.New("--ephemeral-message cannot be combined with thread or context options")
		}
		if !opts.allowWrite {
			return errors.New("refusing to start an ephemeral turn without --allow-write")
		}
	} else if opts.queueMessage != "" {
		if opts.threadID == "" || opts.message == "" || opts.probeContext || opts.ephemeralMessage != "" {
			return errors.New("--queue-add requires thread-id and message and no other write options")
		}
	} else if opts.probeContext {
		if opts.threadID == "" || opts.message != "" {
			return errors.New("--probe-context requires thread-id and no message")
		}
	} else if opts.threadID != "" || opts.message != "" {
		if opts.threadID == "" || opts.message == "" {
			return errors.New("thread-id and message must be provided together")
		}
		if !opts.allowWrite {
			return errors.New("refusing to modify a session without --allow-write")
		}
	}
	if err := client.Initialize(); err != nil {
		return fmt.Errorf("initialize app server: %w", err)
	}

	var (
		result json.RawMessage
		err    error
	)
	if opts.ephemeralMessage != "" {
		threadID, threadResult, startErr := client.StartEphemeralThreadDetails(opts.cwd)
		if startErr != nil {
			return startErr
		}
		result, err = client.StartTurn(threadID, opts.ephemeralMessage)
		if err == nil {
			result, err = json.Marshal(map[string]any{
				"threadId": threadID,
				"thread":   json.RawMessage(threadResult),
				"turn":     json.RawMessage(result),
			})
		}
	} else if opts.queueMessage != "" {
		submission, queueErr := client.QueueMessage(opts.threadID, appserverprobe.NewMessageID(), opts.queueMessage)
		if queueErr != nil {
			err = queueErr
		} else {
			result, err = json.Marshal(map[string]any{"queuedSubmission": submission})
		}
	} else if opts.probeContext {
		result, err = client.CallMCPTool(opts.threadID, "ra2a_probe", "probe_context", map[string]any{})
	} else if opts.threadID == "" {
		sourceKinds := opts.sourceKinds
		if len(sourceKinds) == 0 {
			sourceKinds = []string{
				"cli", "vscode", "exec", "appServer", "subAgent", "subAgentReview",
				"subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown",
			}
		}
		result, err = client.ListThreads(sourceKinds)
	} else {
		result, err = client.InjectMessage(opts.threadID, opts.message)
	}
	if err != nil {
		return err
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, result, "", "  "); err != nil {
		return fmt.Errorf("format response: %w", err)
	}
	_, err = fmt.Fprintln(output, pretty.String())
	return err
}

func openRemoteClient(ctx context.Context, socketPath string) (*appserverprobe.Client, io.Closer, error) {
	connection, err := codexhost.DialUnixWebSocket(ctx, socketPath)
	if err != nil {
		return nil, nil, err
	}
	return appserverprobe.New(connection, connection), connection, nil
}

func openExperimentalRemoteClient(ctx context.Context, socketPath string) (*appserverprobe.Client, io.Closer, error) {
	connection, err := codexhost.DialUnixWebSocket(ctx, socketPath)
	if err != nil {
		return nil, nil, err
	}
	return appserverprobe.NewExperimental(connection, connection), connection, nil
}

func newClientFor(input io.Reader, output io.Writer, experimental bool) *appserverprobe.Client {
	if experimental {
		return appserverprobe.NewExperimental(input, output)
	}
	return appserverprobe.New(input, output)
}

func main() {
	var opts options
	var codexPath string
	var mcpProbePath string
	var socketPath string
	var timeout time.Duration
	flag.StringVar(&codexPath, "codex", "codex", "path to the Codex CLI binary")
	flag.StringVar(&opts.threadID, "thread-id", "", "target thread ID; omit for a read-only list")
	flag.StringVar(&opts.message, "message", "", "message for the target thread")
	flag.StringVar(&opts.queueMessage, "queue-add", "", "queue a message for the target thread (CLI delivery surface)")
	flag.StringVar(&opts.ephemeralMessage, "ephemeral-message", "", "message for a non-persistent probe thread")
	flag.BoolVar(&opts.allowWrite, "allow-write", false, "allow resume and turn/start on the target thread")
	flag.BoolVar(&opts.probeContext, "probe-context", false, "call the RA2A MCP context probe on thread-id")
	flag.StringVar(&mcpProbePath, "mcp-probe-bin", "", "absolute path to mcp-context-probe")
	flag.StringVar(&socketPath, "socket", "", "connect to an existing App Server Unix socket")
	flag.Func("source-kind", "limit thread listing to a source kind; repeatable", func(value string) error {
		opts.sourceKinds = append(opts.sourceKinds, value)
		return nil
	})
	flag.DurationVar(&timeout, "timeout", 20*time.Second, "probe timeout")
	flag.Parse()
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	opts.cwd = workingDirectory

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if socketPath != "" {
		client, closer, openErr := openRemoteClient(ctx, socketPath)
		if opts.queueMessage != "" {
			client, closer, openErr = openExperimentalRemoteClient(ctx, socketPath)
		}
		if openErr != nil {
			fmt.Fprintln(os.Stderr, openErr)
			os.Exit(1)
		}
		defer closer.Close()
		if err := run(client, opts, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	arguments := []string{}
	if opts.probeContext {
		if mcpProbePath == "" {
			fmt.Fprintln(os.Stderr, "--probe-context requires --mcp-probe-bin")
			os.Exit(1)
		}
		arguments = append(arguments, "-c", fmt.Sprintf("mcp_servers.ra2a_probe={command=%s}", strconv.Quote(mcpProbePath)))
	}
	arguments = append(arguments, "app-server", "--stdio")
	command := exec.CommandContext(ctx, codexPath, arguments...)
	stdin, err := command.StdinPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}()

	if err := run(newClientFor(stdout, stdin, opts.queueMessage != ""), opts, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
