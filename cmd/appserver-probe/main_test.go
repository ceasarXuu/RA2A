package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/appserverprobe"
	"github.com/gorilla/websocket"
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
	if !strings.Contains(output.String(), `"threadId": "ephemeral-1"`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestOpenRemoteClientRunsProbeOverUnixSocket(t *testing.T) {
	placeholder, err := os.CreateTemp(".", ".app-server-probe-*.sock")
	if err != nil {
		t.Fatalf("reserve socket path: %v", err)
	}
	socketPath := placeholder.Name()
	_ = placeholder.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, upgradeErr := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if upgradeErr != nil {
			return
		}
		defer connection.Close()
		for {
			var incoming map[string]any
			if readErr := connection.ReadJSON(&incoming); readErr != nil {
				return
			}
			switch incoming["method"] {
			case "initialize":
				_ = connection.WriteJSON(map[string]any{"id": incoming["id"], "result": map[string]any{}})
			case "thread/list":
				_ = connection.WriteJSON(map[string]any{"id": incoming["id"], "result": map[string]any{
					"data": []map[string]string{{"id": "remote-thread"}},
				}})
			}
		}
	})}
	go server.Serve(listener)
	defer server.Close()

	client, closer, err := openRemoteClient(context.Background(), socketPath)
	if err != nil {
		t.Fatalf("open remote client: %v", err)
	}
	defer closer.Close()
	var output bytes.Buffer
	if err := run(client, options{}, &output); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output.String(), `"remote-thread"`) {
		t.Fatalf("output = %s", output.String())
	}
}
