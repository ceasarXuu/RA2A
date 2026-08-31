package appserverprobe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestListThreadsInitializesAndSkipsNotifications(t *testing.T) {
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"codex","version":"test"}}}`,
		`{"jsonrpc":"2.0","method":"thread/status/changed","params":{"threadId":"ignored"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"data":[{"id":"thread-1","name":"Probe"}],"nextCursor":null}}`,
	}, "\n")
	var requests bytes.Buffer
	client := New(strings.NewReader(responses), &requests)

	if err := client.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	result, err := client.ListThreads([]string{"appServer", "vscode"})
	if err != nil {
		t.Fatalf("list threads: %v", err)
	}
	if !bytes.Contains(result, []byte(`"thread-1"`)) {
		t.Fatalf("unexpected result: %s", result)
	}

	lines := decodeRequests(t, requests.Bytes())
	if got := lines[0]["method"]; got != "initialize" {
		t.Fatalf("initialize method = %v", got)
	}
	if got := lines[1]["method"]; got != "initialized" {
		t.Fatalf("initialized notification method = %v", got)
	}
	if _, exists := lines[1]["id"]; exists {
		t.Fatalf("initialized notification has id: %#v", lines[1])
	}
	params := lines[2]["params"].(map[string]any)
	if got := params["sourceKinds"].([]any); len(got) != 2 || got[0] != "appServer" {
		t.Fatalf("sourceKinds = %#v", got)
	}
	if params["useStateDbOnly"] != true {
		t.Fatalf("useStateDbOnly = %v", params["useStateDbOnly"])
	}
}

func TestInjectMessageResumesThreadBeforeSendingText(t *testing.T) {
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{"thread":{"id":"thread-1"}}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"turn":{"id":"turn-1"}}}`,
	}, "\n")
	var requests bytes.Buffer
	client := New(strings.NewReader(responses), &requests)

	result, err := client.InjectMessage("thread-1", "hello")
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if !bytes.Contains(result, []byte(`"turn-1"`)) {
		t.Fatalf("unexpected result: %s", result)
	}

	lines := decodeRequests(t, requests.Bytes())
	if lines[0]["method"] != "thread/resume" || lines[1]["method"] != "turn/start" {
		t.Fatalf("methods = %v, %v", lines[0]["method"], lines[1]["method"])
	}
	params := lines[1]["params"].(map[string]any)
	input := params["input"].([]any)[0].(map[string]any)
	if input["type"] != "text" || input["text"] != "hello" {
		t.Fatalf("input = %#v", input)
	}
}

func TestStartTurnDoesNotResumeLoadedThread(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":1,"result":{"turn":{"id":"turn-1"}}}`
	var requests bytes.Buffer
	client := New(strings.NewReader(response), &requests)

	if _, err := client.StartTurn("loaded-1", "probe"); err != nil {
		t.Fatalf("start turn: %v", err)
	}
	requestList := decodeRequests(t, requests.Bytes())
	if len(requestList) != 1 || requestList[0]["method"] != "turn/start" {
		t.Fatalf("requests = %#v", requestList)
	}
}

func TestRPCErrorIsReturned(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad params"}}`
	client := New(strings.NewReader(response), &bytes.Buffer{})

	_, err := client.ListThreads(nil)
	if err == nil || !strings.Contains(err.Error(), "bad params") {
		t.Fatalf("error = %v", err)
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32602 || rpcErr.Message != "bad params" {
		t.Fatalf("typed error = %#v", rpcErr)
	}
}

func TestListThreadSummariesFollowsPagination(t *testing.T) {
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{"data":[{"id":"thread-1","name":"Named thread","preview":"ignored","status":{"type":"idle"}}],"nextCursor":"page-2"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"data":[{"id":"thread-2","preview":"Preview title","status":{"type":"notLoaded"}}],"nextCursor":null}}`,
	}, "\n")
	var requests bytes.Buffer
	client := New(strings.NewReader(responses), &requests)

	threads, err := client.ListThreadSummaries()
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("thread count = %d", len(threads))
	}
	if threads[0].ID != "thread-1" || threads[0].Title != "Named thread" || threads[0].Status != "idle" {
		t.Fatalf("first thread = %#v", threads[0])
	}
	if threads[1].Title != "Preview title" || threads[1].Status != "notLoaded" {
		t.Fatalf("second thread = %#v", threads[1])
	}

	requestList := decodeRequests(t, requests.Bytes())
	params := requestList[1]["params"].(map[string]any)
	if params["cursor"] != "page-2" {
		t.Fatalf("second page cursor = %#v", params["cursor"])
	}
}

func TestListThreadSummariesBoundsPreviewTitleSize(t *testing.T) {
	preview := strings.Repeat("你", 100)
	response := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"result":{"data":[{"id":"thread-1","preview":%q,"status":{"type":"notLoaded"}}],"nextCursor":null}}`,
		preview,
	)
	client := New(strings.NewReader(response), &bytes.Buffer{})

	threads, err := client.ListThreadSummaries()
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if got := threads[0].Title; len(got) > 160 || !strings.HasSuffix(got, "…") {
		t.Fatalf("bounded title = %q (%d bytes)", got, len(got))
	}
}

func TestCallMCPToolCarriesTargetThreadAndTool(t *testing.T) {
	responses := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{"thread":{"id":"thread-1"}}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"structuredContent":{"hasIdentity":true,"identityCandidates":{"threadId":"thread-1"}}}}`,
	}, "\n")
	var requests bytes.Buffer
	client := New(strings.NewReader(responses), &requests)

	result, err := client.CallMCPTool("thread-1", "ra2a_probe", "probe_context", map[string]any{})
	if err != nil {
		t.Fatalf("call MCP tool: %v", err)
	}
	if !bytes.Contains(result, []byte(`"thread-1"`)) {
		t.Fatalf("unexpected result: %s", result)
	}

	requestList := decodeRequests(t, requests.Bytes())
	if len(requestList) != 2 {
		t.Fatalf("request count = %d, want resume then tool call", len(requestList))
	}
	if requestList[0]["method"] != "thread/resume" || requestList[1]["method"] != "mcpServer/tool/call" {
		t.Fatalf("methods = %v, %v", requestList[0]["method"], requestList[1]["method"])
	}
	request := requestList[1]
	params := request["params"].(map[string]any)
	if params["threadId"] != "thread-1" || params["server"] != "ra2a_probe" || params["tool"] != "probe_context" {
		t.Fatalf("params = %#v", params)
	}
}

func TestStartEphemeralThreadUsesRequestedWorkspace(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":1,"result":{"thread":{"id":"ephemeral-1"}}}`
	var requests bytes.Buffer
	client := New(strings.NewReader(response), &requests)

	threadID, err := client.StartEphemeralThread("/workspace")
	if err != nil {
		t.Fatalf("start ephemeral thread: %v", err)
	}
	if threadID != "ephemeral-1" {
		t.Fatalf("thread ID = %q", threadID)
	}

	request := decodeRequests(t, requests.Bytes())[0]
	params := request["params"].(map[string]any)
	if request["method"] != "thread/start" || params["ephemeral"] != true || params["cwd"] != "/workspace" {
		t.Fatalf("request = %#v", request)
	}
}

func decodeRequests(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var requests []map[string]any
	for decoder.More() {
		var request map[string]any
		if err := decoder.Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
	}
	return requests
}
