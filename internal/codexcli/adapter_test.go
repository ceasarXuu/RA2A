package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/gorilla/websocket"
)

func testEnvelope(text string) agentbridge.MessageEnvelope {
	return agentbridge.MessageEnvelope{
		ID: "msg-1", ProtocolVersion: agentbridge.ProtocolVersion,
		TargetAddress: "ra2a://node-a/" + testThreadID, Text: text,
	}
}

func TestDeliverConfirmsOnTurnCompleted(t *testing.T) {
	server := newFakeAppServer(t)
	server.addThread(testThreadID, false, true)
	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatalf("register: %v", err)
	}

	result := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("hello"))

	if !result.Delivered() {
		t.Fatalf("delivery must be confirmed by turn/completed, got %+v", result)
	}
	if result.TurnID == "" {
		t.Fatal("delivered result must carry the turn id")
	}
	order := server.callOrder()
	resumeAt, startAt := indexOf(order, "thread/resume"), indexOf(order, "turn/start")
	if resumeAt < 0 || startAt < 0 || resumeAt > startAt {
		t.Fatalf("thread/resume must precede turn/start, got %v", order)
	}
	if server.unsubscribeCount() == 0 {
		t.Fatalf("adapter must unsubscribe so the host can unload the thread, got %v", order)
	}
}

func TestDeliverSteersActiveTurnWithExpectedTurnID(t *testing.T) {
	server := newFakeAppServer(t)
	thread := server.addThread(testThreadID, true, true)
	thread.turnSequence = []string{"33333333-3333-4333-8333-333333333333"}
	server.mu.Lock()
	server.activeTurns[testThreadID] = "33333333-3333-4333-8333-333333333333"
	server.mu.Unlock()

	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatalf("register: %v", err)
	}
	result := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("follow-up"))
	if !result.Delivered() {
		t.Fatalf("active follow-up must be delivered, got %+v", result)
	}
	targets := server.steerTargets()
	if len(targets) != 1 || targets[0] != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("steer must carry the active turn id, got %v", targets)
	}
	if indexOf(server.callOrder(), "turn/start") >= 0 {
		t.Fatal("active thread must not open a second turn")
	}
}

func TestDeliverRejectsUnregisteredAndMalformedTargets(t *testing.T) {
	server := newFakeAppServer(t)
	server.addThread(testThreadID, false, true)
	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })

	unregistered := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("hi"))
	if unregistered.Code != agentbridge.ResultNotFound {
		t.Fatalf("unregistered thread must be not_found, got %+v", unregistered)
	}
	malformed := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: "not-a-thread"}, testEnvelope("hi"))
	if malformed.Code != agentbridge.ResultUnsupported {
		t.Fatalf("malformed thread id must be unsupported, got %+v", malformed)
	}
	if len(server.callOrder()) != 0 {
		t.Fatalf("rejected deliveries must not reach the host, got %v", server.callOrder())
	}
}

func TestDeliverRefusesWhenHostRejectsDirectInput(t *testing.T) {
	server := newFakeAppServer(t)
	server.addThread(testThreadID, false, true)
	server.mu.Lock()
	server.noDirectIn[testThreadID] = true
	server.mu.Unlock()
	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatal(err)
	}
	result := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("hi"))
	if result.Code != agentbridge.ResultUnsupported {
		t.Fatalf("host capability refusal must be unsupported, got %+v", result)
	}
	if result.NativeErrorClass != "capability_rejected" {
		t.Fatalf("native class must identify the refusal, got %q", result.NativeErrorClass)
	}
	if indexOf(server.callOrder(), "turn/start") >= 0 {
		t.Fatal("adapter must refuse before writing to the host")
	}
}

func TestDeliverReportsUnconfirmedWhenTerminalEventNeverArrives(t *testing.T) {
	server := newFakeAppServer(t)
	server.addThread(testThreadID, false, true)
	server.mu.Lock()
	server.suppressDone[testThreadID] = true
	server.mu.Unlock()
	adapter := New("node-a", Config{
		CodexPath: server.codexPath, Stderr: os.Stderr,
		ConfirmWindow: 200 * time.Millisecond, CallTimeout: 3 * time.Second,
	})
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatal(err)
	}
	result := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("hi"))
	if result.Delivered() {
		t.Fatalf("a missing terminal event must never count as delivered, got %+v", result)
	}
	if result.Code != agentbridge.ResultUnknown {
		t.Fatalf("unconfirmed delivery must be unknown, got %+v", result)
	}
	order := server.callOrder()
	if countCalls(order, "turn/start") != 1 {
		t.Fatalf("unconfirmed delivery must not be retried, got %v", order)
	}
}

func TestDeliverSurfacesHostTerminalFailure(t *testing.T) {
	server := newFakeAppServer(t)
	server.addThread(testThreadID, false, true)
	server.mu.Lock()
	server.failTurn[testThreadID] = "unexpected status 401 Unauthorized"
	server.mu.Unlock()
	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatal(err)
	}
	result := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("hi"))
	if result.Delivered() {
		t.Fatalf("failed turn must not be delivered, got %+v", result)
	}
	if !strings.Contains(result.Detail, "401") {
		t.Fatalf("terminal host failure must be preserved for diagnosis, got %+v", result)
	}
	if result.NativeErrorClass != "turn_failed" {
		t.Fatalf("native class must record the terminal state, got %q", result.NativeErrorClass)
	}
}

func TestDeliverMapsResumeRejectionToNotFound(t *testing.T) {
	server := newFakeAppServer(t)
	server.addThread(testThreadID, false, true)
	server.mu.Lock()
	server.rejectResume[testThreadID] = "no rollout found for thread id " + testThreadID
	server.mu.Unlock()
	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatal(err)
	}
	result := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("hi"))
	if result.Code != agentbridge.ResultNotFound {
		t.Fatalf("resume rejection must be not_found, got %+v", result)
	}
	if result.NativeErrorClass != "thread_not_found" {
		t.Fatalf("native class must classify the host error, got %q", result.NativeErrorClass)
	}
}

func TestDeliverReportsStartRequiredWhenDaemonAbsent(t *testing.T) {
	offline := filepath.Join(t.TempDir(), "codex-offline")
	payload := []byte("#!/bin/sh\nexit 1\n")
	if runtime.GOOS == "windows" {
		offline += ".cmd"
		payload = []byte("@echo off\r\nexit /b 1\r\n")
	}
	if err := os.WriteFile(offline, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := New("node-a", Config{CodexPath: offline, Stderr: os.Stderr, CallTimeout: time.Second})
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatal(err)
	}
	result := adapter.Deliver(context.Background(),
		agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("hi"))
	if result.Code != agentbridge.ResultStartRequired {
		t.Fatalf("absent daemon must be start_required, got %+v", result)
	}
	health := adapter.Health(context.Background())
	if health.Ready || health.Code != agentbridge.ResultStartRequired {
		t.Fatalf("health must mirror start_required, got %+v", health)
	}
}

func TestListEndpointsPublishesOnlyRegisteredLoadedThreads(t *testing.T) {
	server := newFakeAppServer(t)
	busy := "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
	server.addThread(testThreadID, false, true)
	server.addThread(busy, true, true)
	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Register(busy); err != nil {
		t.Fatal(err)
	}
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("list endpoints: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("expected both registered threads, got %+v", endpoints)
	}
	byID := map[string]agentbridge.Endpoint{}
	for _, endpoint := range endpoints {
		byID[endpoint.ID] = endpoint
	}
	if byID[testThreadID].Status != agentbridge.EndpointReady {
		t.Fatalf("idle thread must be ready, got %+v", byID[testThreadID])
	}
	if byID[busy].Status != agentbridge.EndpointBusy {
		t.Fatalf("active thread must be busy, got %+v", byID[busy])
	}
	if !byID[testThreadID].Has(agentbridge.CapabilitySteerActiveTurn) {
		t.Fatal("threads accepting direct input must advertise active steering")
	}
	if byID[testThreadID].Address.String() != "ra2a://node-a/"+testThreadID {
		t.Fatalf("unexpected address: %s", byID[testThreadID].Address.String())
	}
}

func TestListEndpointsSkipsUnloadedRegisteredThread(t *testing.T) {
	adapter := newTestAdapter(t, newFakeAppServer(t))
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatal(err)
	}
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("list endpoints: %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("unloaded thread must not be published, got %+v", endpoints)
	}
}

func TestEnsureThreadRegistersCreatedThread(t *testing.T) {
	server := newFakeAppServer(t)
	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })
	threadID, err := adapter.EnsureThread(context.Background(), t.TempDir(), "mock-model")
	if err != nil {
		t.Fatalf("ensure thread: %v", err)
	}
	if !validThreadID(threadID) {
		t.Fatalf("created thread id must be usable, got %q", threadID)
	}
	registered := adapter.Registered()
	if len(registered) != 1 || registered[0] != threadID {
		t.Fatalf("created thread must be registered, got %v", registered)
	}
}

func TestRegisterRejectsInvalidThreadID(t *testing.T) {
	adapter := New("node-a", Config{CodexPath: "codex", Stderr: os.Stderr})
	for _, invalid := range []string{"", "abc", "1234", "aaaaaaaa-bbbb-4ccc-8ddd"} {
		if err := adapter.Register(invalid); err == nil {
			t.Fatalf("thread id %q must be rejected", invalid)
		}
	}
	if err := adapter.Register("urn:uuid:aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"); err != nil {
		t.Fatalf("prefixed uuid must be accepted: %v", err)
	}
}

func TestAdapterRejectsAppServerBelowMinimumVersion(t *testing.T) {
	directory := shortTestDir(t)
	socketPath := filepath.Join(directory, "app-server-control.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		conn, upgradeErr := upgrader.Upgrade(writer, request, nil)
		if upgradeErr != nil {
			return
		}
		defer conn.Close()
		for {
			_, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			var message struct {
				ID     *int64 `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(payload, &message) != nil || message.ID == nil {
				continue
			}
			body, _ := json.Marshal(map[string]any{"id": *message.ID, "jsonrpc": "2.0", "result": map[string]any{
				"userAgent": "ra2a_codex_cli/0.157.0 (Ubuntu)", "codexHome": "/none",
				"platformFamily": "unix", "platformOs": "linux",
			}})
			_ = conn.WriteMessage(websocket.TextMessage, body)
		}
	})
	go func() { _ = (&http.Server{Handler: mux}).Serve(listener) }()

	codexPath := filepath.Join(t.TempDir(), "codex")
	payload := `#!/bin/sh
echo '{"status":"running","socketPath":"` + socketPath + `","cliVersion":"0.157.0","appServerVersion":"0.157.0"}'
`
	if runtime.GOOS == "windows" {
		codexPath += ".cmd"
		encodedPath, _ := json.Marshal(socketPath)
		payload = "@echo off\r\necho {\"status\":\"running\",\"socketPath\":" + string(encodedPath) + ",\"cliVersion\":\"0.157.0\",\"appServerVersion\":\"0.157.0\"}\r\n"
	}
	if err := os.WriteFile(codexPath, []byte(payload), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := New("node-a", Config{CodexPath: codexPath, Stderr: os.Stderr, CallTimeout: 2 * time.Second})
	t.Cleanup(func() { _ = adapter.Close() })
	_, err = adapter.EnsureThread(context.Background(), t.TempDir(), "")
	if err == nil || !strings.Contains(err.Error(), "below the supported minimum") {
		t.Fatalf("old app-server must be refused, got %v", err)
	}
}

func TestParseVersionAndOrdering(t *testing.T) {
	cases := map[string]string{
		"ra2a_codex_cli/0.158.0 (Ubuntu 24.4.0; x86_64)": "0.158.0",
		"codex-tui/0.159.0-alpha.12 (macOS)":             "0.159.0-alpha.12",
		"":                                               "",
	}
	for input, want := range cases {
		if got := parseVersion(input); got != want {
			t.Fatalf("parseVersion(%q) = %q, want %q", input, got, want)
		}
	}
	if !versionAtLeast("0.158.0", "0.158.0") || !versionAtLeast("0.159.0", "0.158.0") {
		t.Fatal("equal and newer versions must satisfy the minimum")
	}
	if versionAtLeast("0.157.9", "0.158.0") || versionAtLeast("", "0.158.0") {
		t.Fatal("older or unknown versions must not satisfy the minimum")
	}
}

func TestSocketPathProblem(t *testing.T) {
	if got := socketPathPath(t, "short"); got != "" && runtime.GOOS == "windows" {
		t.Fatalf("short path must be accepted, got %q", got)
	}
	if got := socketPathPath(t, strings.Repeat("a", 120)); runtime.GOOS == "windows" && got == "" {
		t.Fatal("over-long path must be reported on windows")
	}
	if got := socketPathProblem(""); got == "" {
		t.Fatal("empty socket path must always be reported")
	}
}

func socketPathPath(t *testing.T, name string) string {
	t.Helper()
	directory := filepath.Join(shortTestDir(t), name)
	path := filepath.Join(directory, controlSocketRelative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return socketPathProblem(path)
}

func TestClassifyRPCError(t *testing.T) {
	cases := map[string]string{
		"invalid thread id: invalid character":                       "invalid_thread_id",
		"invalid session id: invalid character":                      "invalid_thread_id",
		"thread not found: abc":                                      "thread_not_found",
		"no rollout found for thread id abc":                         "thread_not_found",
		"thread not loaded: abc":                                     "thread_not_found",
		"failed to read thread: no rollout found for thread id abc":  "thread_not_found",
		"thread/queue/add requires experimentalApi capability":       "capability_rejected",
		"thread already has an active or pending turn":               "turn_active",
		"direct app-server input is not allowed for multi-agent sub": "subagent_input_rejected",
	}
	for message, want := range cases {
		if got := classifyRPCError(&rpcError{Code: -32600, Message: message}); got != want {
			t.Fatalf("classify(%q) = %q, want %q", message, got, want)
		}
	}
	if got := classifyRPCError(errors.New("dial failed")); got != "transport" {
		t.Fatalf("transport failures must be classified, got %q", got)
	}
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}
