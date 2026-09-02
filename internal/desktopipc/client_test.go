package desktopipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestWriteFrameUsesLittleEndianLengthPrefix(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })

	done := make(chan error, 1)
	go func() {
		done <- writeFrame(client, envelope{Type: "request", Method: "initialize"})
	}()

	header := make([]byte, 4)
	if _, err := server.Read(header); err != nil {
		t.Fatalf("read header: %v", err)
	}
	length := binary.LittleEndian.Uint32(header)
	body := make([]byte, length)
	if _, err := server.Read(body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("write frame: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got["type"] != "request" || got["method"] != "initialize" {
		t.Fatalf("body = %#v", got)
	}
}

func TestClientInitializesAndStartsTurnThroughDesktopOwner(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	serverDone := make(chan error, 1)
	go func() {
		initialize, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if initialize.Type != "request" || initialize.Method != "initialize" || initialize.Version != 1 {
			t.Errorf("initialize = %#v", initialize)
		}
		if initialize.SourceClientID != "initializing-client" {
			t.Errorf("initialize source = %q", initialize.SourceClientID)
		}
		if initialize.Params["clientType"] != "ra2a-bridge" {
			t.Errorf("initialize params = %#v", initialize.Params)
		}
		if err := writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  initialize.RequestID,
			ResultType: "success",
			Result:     map[string]any{"clientId": "desktop-client-1"},
		}); err != nil {
			serverDone <- err
			return
		}

		start, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if start.Method != "thread-follower-start-turn" || start.Version != 2 {
			t.Errorf("start envelope = %#v", start)
		}
		if start.SourceClientID != "desktop-client-1" {
			t.Errorf("start source = %q", start.SourceClientID)
		}
		assertStartTurnParams(t, start.Params, "thread-1", "hello from LAN", "message-1")
		serverDone <- writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  start.RequestID,
			ResultType: "success",
			Result: map[string]any{
				"result": map[string]any{"turn": map[string]any{"id": "turn-1"}},
			},
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := New(clientConn)
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	result, err := client.StartTurn(ctx, "thread-1", "hello from LAN", "message-1")
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if result.TurnID != "turn-1" {
		t.Fatalf("turn id = %q", result.TurnID)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestClientSteersActiveTurnWithoutStartingAnotherTurn(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	serverDone := make(chan error, 1)
	go func() {
		initialize, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if err := writeFrame(serverConn, successResponse(initialize, map[string]any{"clientId": "desktop-client-1"})); err != nil {
			serverDone <- err
			return
		}
		steer, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if steer.Method != "thread-follower-steer-turn" || steer.Version != 1 {
			t.Errorf("steer envelope = %#v", steer)
		}
		assertSteerTurnParams(t, steer.Params, "thread-1", "follow-up", "message-2")
		serverDone <- writeFrame(serverConn, successResponse(steer, map[string]any{
			"result": map[string]any{"turnId": "turn-active"},
		}))
	}()

	client := New(clientConn)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	result, err := client.SendMessage(context.Background(), "thread-1", "follow-up", "message-2")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if result.TurnID != "turn-active" {
		t.Fatalf("turn id = %q", result.TurnID)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestClientStartsIdleTurnOnlyAfterExplicitNoActiveTurn(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	serverDone := make(chan error, 1)
	go func() {
		initialize, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if err := writeFrame(serverConn, successResponse(initialize, map[string]any{"clientId": "desktop-client-1"})); err != nil {
			serverDone <- err
			return
		}
		steer, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if err := writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  steer.RequestID,
			ResultType: "error",
			Error:      "Cannot steer conversation thread-1 because its active turn already ended",
		}); err != nil {
			serverDone <- err
			return
		}
		start, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if start.Method != "thread-follower-start-turn" || start.Version != 2 {
			t.Errorf("start envelope = %#v", start)
		}
		serverDone <- writeFrame(serverConn, successResponse(start, map[string]any{
			"result": map[string]any{"turn": map[string]any{"id": "turn-new"}},
		}))
	}()

	client := New(clientConn)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	result, err := client.SendMessage(context.Background(), "thread-1", "idle message", "message-3")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if result.TurnID != "turn-new" {
		t.Fatalf("turn id = %q", result.TurnID)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestClientDoesNotStartAfterAmbiguousSteerDisconnect(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	serverDone := make(chan error, 1)
	go func() {
		initialize, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if err := writeFrame(serverConn, successResponse(initialize, map[string]any{"clientId": "desktop-client-1"})); err != nil {
			serverDone <- err
			return
		}
		if _, err := readFrame(serverConn); err != nil {
			serverDone <- err
			return
		}
		serverDone <- serverConn.Close()
	}()

	client := New(clientConn)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, err := client.SendMessage(context.Background(), "thread-1", "follow-up", "message-2")
	if !IsDeliveryUnknown(err) {
		t.Fatalf("error = %v, want delivery unknown", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestClientDoesNotRetryAmbiguousDesktopTimeout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	go func() {
		initialize, _ := readFrame(serverConn)
		_ = writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  initialize.RequestID,
			ResultType: "success",
			Result:     map[string]any{"clientId": "desktop-client-1"},
		})
		_, _ = readFrame(serverConn)
	}()

	initCtx, initCancel := context.WithTimeout(context.Background(), time.Second)
	defer initCancel()
	client := New(clientConn)
	if err := client.Initialize(initCtx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	turnCtx, turnCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer turnCancel()
	_, err := client.StartTurn(turnCtx, "thread-1", "hello", "message-1")
	if err == nil || !IsDeliveryUnknown(err) {
		t.Fatalf("error = %v, want delivery unknown", err)
	}
}

func TestClientTreatsDisconnectAfterStartWriteAsDeliveryUnknown(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	serverDone := make(chan error, 1)
	go func() {
		initialize, err := readFrame(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if err := writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  initialize.RequestID,
			ResultType: "success",
			Result:     map[string]any{"clientId": "desktop-client-1"},
		}); err != nil {
			serverDone <- err
			return
		}
		if _, err := readFrame(serverConn); err != nil {
			serverDone <- err
			return
		}
		serverDone <- serverConn.Close()
	}()

	client := New(clientConn)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, err := client.StartTurn(context.Background(), "thread-1", "hello", "message-1")
	if !IsDeliveryUnknown(err) {
		t.Fatalf("error = %v, want delivery unknown", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestClientTreatsRejectedStartAsDefinitelyNotDelivered(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	go func() {
		initialize, _ := readFrame(serverConn)
		_ = writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  initialize.RequestID,
			ResultType: "success",
			Result:     map[string]any{"clientId": "desktop-client-1"},
		})
		start, _ := readFrame(serverConn)
		_ = writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  start.RequestID,
			ResultType: "error",
			Error:      "no-client-found: thread stream owner is unavailable",
		})
	}()

	client := New(clientConn)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, err := client.StartTurn(context.Background(), "thread-1", "hello", "message-1")
	if !IsNotDelivered(err) || IsDeliveryUnknown(err) {
		t.Fatalf("error = %v, want definitely not delivered", err)
	}
}

func TestClientTreatsSuccessfulStartWithoutTurnIDAsDeliveryUnknown(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close(); _ = serverConn.Close() })

	go func() {
		initialize, _ := readFrame(serverConn)
		_ = writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  initialize.RequestID,
			ResultType: "success",
			Result:     map[string]any{"clientId": "desktop-client-1"},
		})
		start, _ := readFrame(serverConn)
		_ = writeFrame(serverConn, envelope{
			Type:       "response",
			RequestID:  start.RequestID,
			ResultType: "success",
			Result:     map[string]any{"result": map[string]any{}},
		})
	}()

	client := New(clientConn)
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, err := client.StartTurn(context.Background(), "thread-1", "hello", "message-1")
	if !IsDeliveryUnknown(err) {
		t.Fatalf("error = %v, want delivery unknown", err)
	}
}

func TestSocketCandidatesPreferCodexHomeThenLegacyTempPath(t *testing.T) {
	got := SocketCandidates("darwin", "/Users/test/.codex", "/private/tmp", 501)
	want := []string{
		"/Users/test/.codex/ipc/ipc.sock",
		"/private/tmp/codex-ipc/ipc-501.sock",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestSocketCandidatesUseNamedPipeOnWindows(t *testing.T) {
	got := SocketCandidates("windows", `C:\\Users\\test\\.codex`, `C:\\Temp`, 0)
	if len(got) != 1 || got[0] != `\\.\pipe\codex-ipc` {
		t.Fatalf("candidates = %#v", got)
	}
}

func TestNewMessageIDIsUnique(t *testing.T) {
	first := NewMessageID()
	second := NewMessageID()
	if first == "" || second == "" || first == second {
		t.Fatalf("message IDs = %q, %q", first, second)
	}
}

func assertStartTurnParams(t *testing.T, params map[string]any, threadID, text, messageID string) {
	t.Helper()
	if params["conversationId"] != threadID {
		t.Fatalf("conversationId = %#v", params["conversationId"])
	}
	turnStart := params["turnStart"].(map[string]any)
	request := turnStart["request"].(map[string]any)
	if request["threadId"] != threadID || request["clientUserMessageId"] != messageID {
		t.Fatalf("request = %#v", request)
	}
	input := request["input"].([]any)[0].(map[string]any)
	if input["type"] != "text" || input["text"] != text {
		t.Fatalf("input = %#v", input)
	}
	contextParams := turnStart["context"].(map[string]any)
	if contextParams["inheritThreadSettings"] != true {
		t.Fatalf("context = %#v", contextParams)
	}
}

func assertSteerTurnParams(t *testing.T, params map[string]any, threadID, text, messageID string) {
	t.Helper()
	if params["conversationId"] != threadID || params["clientUserMessageId"] != messageID {
		t.Fatalf("steer params = %#v", params)
	}
	input := params["input"].([]any)[0].(map[string]any)
	if input["type"] != "text" || input["text"] != text {
		t.Fatalf("steer input = %#v", input)
	}
	restore := params["restoreMessage"].(map[string]any)
	if _, ok := restore["context"].(map[string]any); !ok {
		t.Fatalf("restoreMessage = %#v", restore)
	}
}

func successResponse(request envelope, result map[string]any) envelope {
	return envelope{Type: "response", RequestID: request.RequestID, ResultType: "success", Result: result}
}
