package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunSelfTestDiscoversAndCallsLocalNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var output bytes.Buffer

	err := run(ctx, []string{
		"selftest",
		"--pin", "A2B3C4",
		"--id", "cli-selftest-node",
		"--name", "CLI Selftest Node",
	}, &output)
	if err != nil {
		t.Fatalf("run selftest: %v", err)
	}
	for _, want := range []string{"discovered=ra2a://cli-selftest-node", "sessions=0", "selftest=ok"} {
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
	}, &output)
	if err != nil {
		t.Fatalf("run serve: %v", err)
	}
	if !strings.Contains(output.String(), "node=ra2a://cli-serve-node status=running") {
		t.Fatalf("output = %q", output.String())
	}
}
