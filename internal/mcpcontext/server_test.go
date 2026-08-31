package mcpcontext

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeReportsOnlyMetadataKeysAndIdentityCandidates(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"probe_context","arguments":{},"_meta":{"traceId":"secret-trace","threadId":"thread-123","nested":{"session_id":"session-9"}}}}`,
	}, "\n")
	var output bytes.Buffer

	if err := Serve(strings.NewReader(input), &output); err != nil {
		t.Fatalf("serve: %v", err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 3 {
		t.Fatalf("response count = %d", len(responses))
	}
	initialize := responses[0]["result"].(map[string]any)
	if initialize["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocol version = %v", initialize["protocolVersion"])
	}
	tools := responses[1]["result"].(map[string]any)["tools"].([]any)
	if tools[0].(map[string]any)["name"] != "probe_context" {
		t.Fatalf("tools = %#v", tools)
	}
	report := responses[2]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if report["hasIdentity"] != true {
		t.Fatalf("report = %#v", report)
	}
	candidates := report["identityCandidates"].(map[string]any)
	if candidates["threadId"] != "thread-123" || candidates["nested.session_id"] != "session-9" {
		t.Fatalf("candidates = %#v", candidates)
	}
	encoded, _ := json.Marshal(report)
	if bytes.Contains(encoded, []byte("secret-trace")) {
		t.Fatalf("non-identity metadata leaked: %s", encoded)
	}
}

func TestServeReportsMissingMetadataWithoutError(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"probe_context","arguments":{}}}`
	var output bytes.Buffer

	if err := Serve(strings.NewReader(input), &output); err != nil {
		t.Fatalf("serve: %v", err)
	}
	responses := decodeResponses(t, output.Bytes())
	report := responses[0]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if report["hasIdentity"] != false {
		t.Fatalf("report = %#v", report)
	}
}

func decodeResponses(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var responses []map[string]any
	for decoder.More() {
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		responses = append(responses, response)
	}
	return responses
}
