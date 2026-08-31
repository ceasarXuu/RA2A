package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/appserverprobe"
)

func TestRunListsThreadsByDefault(t *testing.T) {
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"data":[{"id":"thread-1"}]}}`,
	}, "\n")
	var requests, output bytes.Buffer
	client := appserverprobe.New(strings.NewReader(responses), &requests)

	err := run(client, options{}, &output)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output.String(), `"thread-1"`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestRunRequiresExplicitWritePermission(t *testing.T) {
	client := appserverprobe.New(strings.NewReader(""), &bytes.Buffer{})

	err := run(client, options{threadID: "thread-1", message: "hello"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "allow-write") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunCallsContextProbeWithoutTurnWrite(t *testing.T) {
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thread-1"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"structuredContent":{"hasIdentity":true,"identityCandidates":{"threadId":"thread-1"}}}}`,
	}, "\n")
	var requests, output bytes.Buffer
	client := appserverprobe.New(strings.NewReader(responses), &requests)

	err := run(client, options{threadID: "thread-1", probeContext: true}, &output)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output.String(), `"hasIdentity": true`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestRunStartsTurnInEphemeralThread(t *testing.T) {
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"ephemeral-1"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}`,
	}, "\n")
	var requests, output bytes.Buffer
	client := appserverprobe.New(strings.NewReader(responses), &requests)

	err := run(client, options{ephemeralMessage: "probe", cwd: "/workspace", allowWrite: true}, &output)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output.String(), `"status": "inProgress"`) {
		t.Fatalf("output = %s", output.String())
	}
}
