package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/lannode"
)

func TestDefaultAppServerSocketUsesCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "/state/codex")
	if got := defaultAppServerSocket(); got != "/state/codex/app-server-control/app-server-control.sock" {
		t.Fatalf("socket = %q", got)
	}
}

type fakeSessionSource struct {
	sessions []lannode.Session
	messages chan deliveredMessage
}

type deliveredMessage struct {
	target string
	prompt string
}

func (source *fakeSessionSource) ListSessions(context.Context) ([]lannode.Session, error) {
	return source.sessions, nil
}

func (source *fakeSessionSource) Close() error { return nil }

func (source *fakeSessionSource) SendMessage(_ context.Context, target, prompt string) error {
	if source.messages != nil {
		source.messages <- deliveredMessage{target: target, prompt: prompt}
	}
	return nil
}

func fakeSourceFactory(sessions []lannode.Session) sessionSourceFactory {
	return func(context.Context, string, string, io.Writer) (sessionSource, error) {
		return &fakeSessionSource{sessions: sessions}, nil
	}
}

func TestRunSelfTestDiscoversAndCallsLocalNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var output bytes.Buffer

	err := run(ctx, []string{
		"selftest",
		"--pin", "A2B3C4",
		"--id", "cli-selftest-node",
		"--name", "CLI Selftest Node",
	}, &output, fakeSourceFactory([]lannode.Session{{ID: "thread-1", Title: "Test", Status: "idle"}}))
	if err != nil {
		t.Fatalf("run selftest: %v", err)
	}
	for _, want := range []string{"discovered=ra2a://cli-selftest-node", "sessions=1", "selftest=ok"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output %q does not contain %q", output.String(), want)
		}
	}
}

func TestRunSendDiscoversPeerAndDeliversMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var output bytes.Buffer
	messages := make(chan deliveredMessage, 1)
	factory := func(context.Context, string, string, io.Writer) (sessionSource, error) {
		return &fakeSessionSource{messages: messages}, nil
	}

	err := run(ctx, []string{
		"send",
		"--pin", "A2B3C4",
		"--id", "cli-send-node",
		"--peer", "cli-send-node",
		"--session", "thread-target",
		"--message", "hello",
	}, &output, factory)
	if err != nil {
		t.Fatalf("run send: %v", err)
	}
	got := <-messages
	if got.target != "thread-target" || !strings.Contains(got.prompt, "from: ra2a://cli-send-node") || !strings.HasSuffix(got.prompt, "\n\nhello") {
		t.Fatalf("delivered message = %#v", got)
	}
	if !strings.Contains(output.String(), "delivered=ra2a://cli-send-node/thread-target") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunServeStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer

	err := run(ctx, []string{
		"serve",
		"--pin", "A2B3C4",
		"--id", "cli-serve-node",
		"--name", "CLI Serve Node",
		"--control-address", "127.0.0.1:0",
	}, &output, fakeSourceFactory(nil))
	if err != nil {
		t.Fatalf("run serve: %v", err)
	}
	if !strings.Contains(output.String(), "node=ra2a://cli-serve-node status=running") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunMCPExposesProductionTools(t *testing.T) {
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\",\"params\":{}}\n")
	var output bytes.Buffer
	if err := runMCP(context.Background(), []string{"--control-url", "http://127.0.0.1:1"}, input, &output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_targets", "send_message"} {
		if !strings.Contains(output.String(), name) {
			t.Fatalf("output %q missing %q", output.String(), name)
		}
	}
}
