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

type fakeSessionSource struct {
	sessions []lannode.Session
}

func (source *fakeSessionSource) ListSessions(context.Context) ([]lannode.Session, error) {
	return source.sessions, nil
}

func (source *fakeSessionSource) Close() error { return nil }

func fakeSourceFactory(sessions []lannode.Session) sessionSourceFactory {
	return func(context.Context, string, io.Writer) (sessionSource, error) {
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

func TestRunServeStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer

	err := run(ctx, []string{
		"serve",
		"--pin", "A2B3C4",
		"--id", "cli-serve-node",
		"--name", "CLI Serve Node",
	}, &output, fakeSourceFactory(nil))
	if err != nil {
		t.Fatalf("run serve: %v", err)
	}
	if !strings.Contains(output.String(), "node=ra2a://cli-serve-node status=running") {
		t.Fatalf("output = %q", output.String())
	}
}
