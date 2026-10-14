package codexcli

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type fakeTurn struct {
	ID     string
	Status string
	Error  *turnError
}

type fakeAppServer struct {
	t          *testing.T
	listener   net.Listener
	socketPath string
	codexPath  string

	// writeMu serialises websocket writes: gorilla permits one concurrent
	// writer, and the fake writes a response and a later notification from
	// different goroutines.
	writeMu sync.Mutex

	mu                sync.Mutex
	calls             []string
	threads           map[string]*fakeThread
	activeTurns       map[string]string
	steerExpectations []string
	turnCounter       int
	suppressDone      map[string]bool
	failTurn          map[string]string
	rejectResume      map[string]string
	noDirectIn        map[string]bool
	directInputFields map[string]json.RawMessage
	inputs            []json.RawMessage
	completeBeforeAck bool
	disconnectTurn    bool
	invalidReceipt    string
	unloadBeforeTurn  bool

	// codexHome is reported by initialize; accountReadErr and
	// accountReadSilent shape the account read the managed-host gate performs.
	codexHome         string
	accountReadErr    string
	accountReadSilent bool
}

type fakeThread struct {
	id           string
	active       bool
	canAccept    bool
	turnSequence []string
}

func newFakeAppServer(t *testing.T) *fakeAppServer {
	t.Helper()
	directory := shortTestDir(t)
	socketPath := filepath.Join(directory, "app-server-control.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatalf("create socket dir: %v", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &fakeAppServer{
		t: t, listener: listener, socketPath: socketPath,
		threads:           map[string]*fakeThread{},
		activeTurns:       map[string]string{},
		suppressDone:      map[string]bool{},
		failTurn:          map[string]string{},
		rejectResume:      map[string]string{},
		noDirectIn:        map[string]bool{},
		directInputFields: map[string]json.RawMessage{},
		codexHome:         "/none",
	}
	server.codexPath = writeFakeCodex(t, socketPath)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		server.serve(conn)
	})
	go func() { _ = (&http.Server{Handler: mux}).Serve(listener) }()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func writeFakeCodex(t *testing.T, socketPath string) string {
	t.Helper()
	return writeFakeCodexFixture(t, fakeCodexFixture{Default: fakeCodexResult{Output: fmt.Sprintf(
		`{"status":"running","backend":"pid","pid":4242,"managedCodexPath":"/none","managedCodexVersion":"0.158.0","socketPath":%q,"cliVersion":"0.158.0","appServerVersion":"0.158.0"}`,
		socketPath)}})
}

func shortTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func (server *fakeAppServer) addThread(id string, active, canAccept bool) *fakeThread {
	server.mu.Lock()
	defer server.mu.Unlock()
	thread := &fakeThread{id: id, active: active, canAccept: canAccept}
	server.threads[id] = thread
	return thread
}

func (server *fakeAppServer) callOrder() []string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]string(nil), server.calls...)
}

func (server *fakeAppServer) unsubscribeCount() int {
	server.mu.Lock()
	defer server.mu.Unlock()
	return countCalls(server.calls, "thread/unsubscribe")
}

func countCalls(calls []string, want string) int {
	total := 0
	for _, call := range calls {
		if call == want {
			total++
		}
	}
	return total
}

func (server *fakeAppServer) steerTargets() []string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]string(nil), server.steerExpectations...)
}

func (server *fakeAppServer) writeJSON(conn *websocket.Conn, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	server.writeMu.Lock()
	defer server.writeMu.Unlock()
	_ = conn.WriteMessage(websocket.TextMessage, payload)
}

func (server *fakeAppServer) serve(conn *websocket.Conn) {
	defer conn.Close()
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var message struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(payload, &message); err != nil {
			continue
		}
		if message.ID == nil {
			continue
		}
		if err := server.dispatch(conn, *message.ID, message.Method, message.Params); err != nil {
			return
		}
	}
}

func (server *fakeAppServer) dispatch(conn *websocket.Conn, id int64, method string, params json.RawMessage) error {
	server.mu.Lock()
	server.calls = append(server.calls, method)
	threadID := extractString(params, "threadId")
	server.mu.Unlock()

	switch method {
	case "initialize":
		server.mu.Lock()
		home := server.codexHome
		server.mu.Unlock()
		server.writeJSON(conn, map[string]any{
			"id": id, "jsonrpc": "2.0",
			"result": map[string]any{
				"userAgent": "ra2a_codex_cli/0.158.0 (Ubuntu 24.4.0; x86_64)",
				"codexHome": home, "platformFamily": "unix", "platformOs": "linux",
			},
		})
	case "account/rateLimits/read":
		server.mu.Lock()
		accountErr := server.accountReadErr
		silent := server.accountReadSilent
		server.mu.Unlock()
		if silent {
			return nil
		}
		if accountErr != "" {
			return server.rpcError(conn, id, -32603, accountErr)
		}
		server.writeJSON(conn, map[string]any{"id": id, "jsonrpc": "2.0", "result": map[string]any{"rateLimits": map[string]any{}}})
	case "thread/loaded/list":
		server.mu.Lock()
		loaded := make([]string, 0, len(server.threads))
		for threadID := range server.threads {
			loaded = append(loaded, threadID)
		}
		server.mu.Unlock()
		server.writeJSON(conn, map[string]any{"id": id, "jsonrpc": "2.0", "result": map[string]any{"data": loaded}})
	case "thread/read":
		server.mu.Lock()
		thread := server.threads[threadID]
		noDirect := server.noDirectIn[threadID]
		directInput, override := server.directInputFields[threadID]
		server.mu.Unlock()
		if thread == nil {
			return server.rpcError(conn, id, -32600, "thread not loaded: "+threadID)
		}
		status := "idle"
		if thread.active {
			status = "active"
		}
		accept := thread.canAccept && !noDirect
		record := map[string]any{
			"id": thread.id, "sessionId": thread.id, "source": "vscode", "originator": "codex-tui",
			"cliVersion": "0.158.0", "preview": "preview of " + thread.id,
			"status": map[string]any{"type": status}, "canAcceptDirectInput": accept,
		}
		if override {
			if len(directInput) == 0 {
				delete(record, "canAcceptDirectInput")
			} else {
				record["canAcceptDirectInput"] = directInput
			}
		}
		server.writeJSON(conn, map[string]any{"id": id, "jsonrpc": "2.0", "result": map[string]any{
			"thread": record,
		}})
	case "thread/start":
		server.mu.Lock()
		server.turnCounter++
		newID := fmt.Sprintf("11111111-1111-4111-8111-%012d", server.turnCounter)
		server.threads[newID] = &fakeThread{id: newID, canAccept: true}
		server.calls = append(server.calls, "thread/created:"+newID)
		server.mu.Unlock()
		server.writeJSON(conn, map[string]any{"id": id, "jsonrpc": "2.0", "result": map[string]any{
			"thread": map[string]any{"id": newID, "status": map[string]any{"type": "idle"}},
		}})
	case "thread/resume":
		if reason, blocked := server.rejectResume[threadID]; blocked {
			return server.rpcError(conn, id, -32600, reason)
		}
		server.writeJSON(conn, map[string]any{"id": id, "jsonrpc": "2.0", "result": map[string]any{
			"thread": map[string]any{"id": threadID, "status": map[string]any{"type": "idle"}},
		}})
	case "thread/turns/list":
		server.mu.Lock()
		turnID := server.activeTurns[threadID]
		server.mu.Unlock()
		turns := []any{}
		if turnID != "" {
			turns = append(turns, map[string]any{"id": turnID, "status": "inProgress", "items": []any{}})
		}
		server.writeJSON(conn, map[string]any{"id": id, "jsonrpc": "2.0", "result": map[string]any{"data": turns}})
	case "turn/start", "turn/steer":
		server.mu.Lock()
		if server.unloadBeforeTurn {
			delete(server.threads, threadID)
		}
		if server.threads[threadID] == nil {
			server.mu.Unlock()
			return server.rpcError(conn, id, -32600, "thread not loaded: "+threadID)
		}
		server.inputs = append(server.inputs, append(json.RawMessage(nil), params...))
		if method == "turn/steer" {
			server.steerExpectations = append(server.steerExpectations, extractString(params, "expectedTurnId"))
		} else {
			server.turnCounter++
		}
		turnID := server.activeTurns[threadID]
		if method == "turn/start" {
			turnID = fmt.Sprintf("22222222-2222-4222-8222-%012d", server.turnCounter)
		}
		server.activeTurns[threadID] = turnID
		suppress := server.suppressDone[threadID]
		failure := server.failTurn[threadID]
		beforeAck := server.completeBeforeAck
		disconnect := server.disconnectTurn
		server.mu.Unlock()
		if disconnect {
			return conn.Close()
		}

		result := map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress", "error": nil}}
		if method == "turn/steer" {
			result = map[string]any{"turnId": turnID}
		}
		switch server.invalidReceipt {
		case "missing":
			result = map[string]any{}
		case "error":
			result = map[string]any{"turn": map[string]any{"id": turnID, "error": map[string]any{"message": "input rejected"}}}
		case "wrong-steer":
			result = map[string]any{"turnId": "different-turn"}
		}
		completed := fakeTurn{ID: turnID, Status: "completed"}
		if failure != "" {
			completed.Status = "failed"
			completed.Error = &turnError{Message: failure}
		}
		notifyCompleted := func() {
			if suppress {
				return
			}
			server.writeJSON(conn, map[string]any{
				"jsonrpc": "2.0", "method": "turn/completed",
				"params": map[string]any{"threadId": threadID, "turn": map[string]any{
					"id": turnID, "status": completed.Status, "error": completed.Error,
				}},
			})
		}
		if beforeAck {
			notifyCompleted()
		}
		server.writeJSON(conn, map[string]any{"id": id, "jsonrpc": "2.0", "result": result})
		if !beforeAck {
			notifyCompleted()
		}
	case "thread/unsubscribe":
		server.writeJSON(conn, map[string]any{"id": id, "jsonrpc": "2.0", "result": map[string]any{"status": "unsubscribed"}})
	default:
		return server.rpcError(conn, id, -32601, "unknown method: "+method)
	}
	return nil
}

func (server *fakeAppServer) rpcError(conn *websocket.Conn, id int64, code int, message string) error {
	server.writeJSON(conn, map[string]any{
		"id": id, "jsonrpc": "2.0", "error": map[string]any{"code": code, "message": message},
	})
	return nil
}

func extractString(params json.RawMessage, key string) string {
	var decoded map[string]any
	if err := json.Unmarshal(params, &decoded); err != nil {
		return ""
	}
	value, _ := decoded[key].(string)
	return value
}

func newTestAdapter(t *testing.T, server *fakeAppServer) *Adapter {
	t.Helper()
	return New("node-a", Config{
		CodexPath: server.codexPath, Stderr: os.Stderr,
		CallTimeout: 3 * time.Second,
	})
}

var testThreadID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
