package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/control"
)

type fakeBackend struct {
	targets []control.Target
	sent    control.SendRequest
	sendErr error
}

func (backend *fakeBackend) ListTargets(context.Context) ([]control.Target, error) {
	return backend.targets, nil
}
func (backend *fakeBackend) Send(_ context.Context, request control.SendRequest) error {
	backend.sent = request
	return backend.sendErr
}

func TestServeListsOnlyProductionTools(t *testing.T) {
	responses := serveRequests(t, &fakeBackend{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	)
	result := responses[1]["result"].(map[string]any)
	initialize := responses[0]["result"].(map[string]any)
	if initialize["serverInfo"].(map[string]any)["version"] != "v0.0.6" {
		t.Fatalf("serverInfo = %#v", initialize["serverInfo"])
	}
	tools := result["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["name"] != "list_targets" || tools[1].(map[string]any)["name"] != "send_message" {
		t.Fatalf("tools = %#v", tools)
	}
	if description := tools[0].(map[string]any)["description"].(string); !strings.Contains(description, "discovered") || !strings.Contains(description, "status") {
		t.Fatalf("list_targets description = %q", description)
	}
}

func TestListTargetsReturnsStructuredTargets(t *testing.T) {
	backend := &fakeBackend{targets: []control.Target{{ID: "node-b", Name: "Node B", Status: "degraded", SessionsStale: true}}}
	responses := serveRequests(t, backend,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_targets","arguments":{}}}`,
	)
	result := responses[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if result["isError"] != false || len(structured["targets"].([]any)) != 1 {
		t.Fatalf("result = %#v", result)
	}
	target := structured["targets"].([]any)[0].(map[string]any)
	if target["status"] != "degraded" || target["sessionsStale"] != true {
		t.Fatalf("target = %#v", target)
	}
}

func TestSendMessageUsesCallingThreadFromMetadata(t *testing.T) {
	backend := &fakeBackend{}
	responses := serveRequests(t, backend,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_message","arguments":{"to":"ra2a://node-b/thread-b","text":"hello"},"_meta":{"threadId":"thread-a"}}}`,
	)
	if backend.sent.To != "ra2a://node-b/thread-b" || backend.sent.Text != "hello" || backend.sent.SourceSessionID != "thread-a" {
		t.Fatalf("sent = %#v", backend.sent)
	}
	result := responses[0]["result"].(map[string]any)
	if result["isError"] != false || result["structuredContent"].(map[string]any)["status"] != "accepted" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSendMessageRejectsMissingCallingThread(t *testing.T) {
	backend := &fakeBackend{}
	responses := serveRequests(t, backend,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_message","arguments":{"to":"ra2a://node-b/thread-b","text":"hello"},"_meta":{}}}`,
	)
	result := responses[0]["result"].(map[string]any)
	if result["isError"] != true || !strings.Contains(result["content"].([]any)[0].(map[string]any)["text"].(string), "CALLER_SESSION_UNKNOWN") {
		t.Fatalf("result = %#v", result)
	}
	if backend.sent != (control.SendRequest{}) {
		t.Fatalf("unexpected send = %#v", backend.sent)
	}
}

func TestSendMessagePreservesBusyError(t *testing.T) {
	backend := &fakeBackend{sendErr: errors.New("SESSION_BUSY")}
	responses := serveRequests(t, backend,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_message","arguments":{"to":"ra2a://node-b/thread-b","text":"hello"},"_meta":{"threadId":"thread-a"}}}`,
	)
	result := responses[0]["result"].(map[string]any)
	if result["isError"] != true || result["structuredContent"].(map[string]any)["error"] != "SESSION_BUSY" {
		t.Fatalf("result = %#v", result)
	}
}

func serveRequests(t *testing.T, backend Backend, requests ...string) []map[string]any {
	t.Helper()
	var input strings.Builder
	for _, request := range requests {
		input.WriteString(request)
		input.WriteByte('\n')
	}
	var output bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(input.String()), &output, backend); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var responses []map[string]any
	for decoder.More() {
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	return responses
}
