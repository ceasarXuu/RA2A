package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

// fakeOpenCode reproduces the real server surface: session listing, message
// posting, and an SSE event stream that publishes busy/idle.
type fakeOpenCode struct {
	server     *httptest.Server
	messages   chan string
	turnCount  int
	postErr    int
	slowPost   time.Duration
	dropPost   bool
	lastPosted string
}

func newFakeOpenCode(t *testing.T) *fakeOpenCode {
	t.Helper()
	fake := &fakeOpenCode{messages: make(chan string, 32)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode([]sessionPayload{
			{ID: "ses_1", Title: "first", Directory: "/tmp/a", Agent: "build", Version: "1.18.33",
				Time: struct {
					Created int64 `json:"created"`
					Updated int64 `json:"updated"`
				}{Created: 1, Updated: 2}},
			{ID: "ses_2", Title: "second", Directory: "/tmp/b", Agent: "plan", Version: "1.18.33",
				Time: struct {
					Created int64 `json:"created"`
					Updated int64 `json:"updated"`
				}{Created: 1, Updated: 3}},
		})
	})
	mux.HandleFunc("GET /session/{id}/message", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		userText := fake.lastPosted
		if userText == "" {
			userText = "unrelated earlier message"
		}
		_ = json.NewEncoder(writer).Encode([]map[string]any{
			{"info": map[string]any{"sessionID": request.PathValue("id"), "role": "user",
				"time": map[string]any{"completed": float64(1)}},
				"parts": []map[string]any{{"type": "text", "text": userText}}},
			{"info": map[string]any{"sessionID": request.PathValue("id"), "role": "assistant",
				"time": map[string]any{"completed": float64(2)}, "error": nil},
				"parts": []map[string]any{{"type": "text", "text": "done"}}},
		})
	})
	mux.HandleFunc("POST /session/{id}/message", func(writer http.ResponseWriter, request *http.Request) {
		if fake.postErr > 0 {
			fake.postErr--
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"name":"BadRequest"}`))
			return
		}
		var payload struct {
			Parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"parts"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || len(payload.Parts) == 0 {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"name":"BadRequest","data":{"message":"Missing key"}}`))
			return
		}
		if len(payload.Parts) > 0 {
			fake.lastPosted = payload.Parts[0].Text
		}
		fake.turnCount++
		if fake.dropPost {
			// Simulate a request whose response never reaches the client and
			// which left no trace in the session.
			fake.lastPosted = ""
			if fake.slowPost > 0 {
				time.Sleep(fake.slowPost)
			}
			return
		}
		if fake.slowPost > 0 {
			time.Sleep(fake.slowPost)
		}
		// Real OpenCode answers with an in-progress assistant message.
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"info": map[string]any{"sessionID": request.PathValue("id"), "role": "assistant",
				"time": map[string]any{"completed": nil}, "error": nil},
			"parts": []map[string]any{{"type": "step-start"}, {"type": "text", "text": "working"}},
		})
		if fake.turnCount == 1 && !fake.dropPost {
			fake.push(t, map[string]any{"type": "session.status",
				"properties": map[string]any{"sessionID": request.PathValue("id"),
					"status": map[string]any{"type": "busy"}}})
			go func() {
				time.Sleep(80 * time.Millisecond)
				fake.push(t, map[string]any{"type": "session.idle",
					"properties": map[string]any{"sessionID": request.PathValue("id")}})
			}()
		}
	})
	mux.HandleFunc("GET /event", fake.serveEvents)
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *fakeOpenCode) push(t *testing.T, event map[string]any) {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	select {
	case fake.messages <- "data: " + string(payload):
	default:
	}
}

func (fake *fakeOpenCode) serveEvents(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)
	flusher, _ := writer.(http.Flusher)
	_, _ = writer.Write([]byte("data: {\"type\":\"server.connected\",\"properties\":{}}\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
	for {
		select {
		case <-request.Context().Done():
			return
		case line := <-fake.messages:
			if line == "" {
				continue
			}
			if _, err := writer.Write([]byte(line + "\n\n")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func newTestAdapter(t *testing.T, fake *fakeOpenCode) *Adapter {
	t.Helper()
	client := NewClient(Config{BaseURL: fake.server.URL, CallTimeout: 3 * time.Second,
		IdleWait: 3 * time.Second, Stderr: nil})
	adapter := New("node-a", client, nil)
	t.Cleanup(func() { _ = adapter.Close() })
	return adapter
}

func address(id string) agentbridge.Address {
	return agentbridge.Address{NodeID: "node-a", EndpointID: id}
}

func envelope() agentbridge.MessageEnvelope {
	return agentbridge.MessageEnvelope{ID: "m1", ProtocolVersion: agentbridge.ProtocolVersion,
		SourceAddress: "ra2a://node-a/ses_1", TargetAddress: "ra2a://node-a/ses_1", Text: "hi"}
}

func TestListEndpointsPublishesOnlyAdoptedSessions(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	if err := adapter.Adopt("ses_1"); err != nil {
		t.Fatal(err)
	}
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].ID != "ses_1" {
		t.Fatalf("only adopted sessions may be published, got %+v", endpoints)
	}
	if endpoints[0].Agent != AgentKind {
		t.Fatalf("agent kind must be opencode, got %q", endpoints[0].Agent)
	}
	if endpoints[0].Title != "first" {
		t.Fatalf("title must come from the session, got %q", endpoints[0].Title)
	}
	if endpoints[0].Address.String() != "ra2a://node-a/ses_1" {
		t.Fatalf("unexpected address %q", endpoints[0].Address.String())
	}
}

func TestListEndpointsSkipsAdoptedSessionThatNoLongerExists(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	if err := adapter.Adopt("ses_gone"); err != nil {
		t.Fatal(err)
	}
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("a session the server does not know must not be published, got %+v", endpoints)
	}
}

// The POST response is an in-progress assistant message, so delivery must wait
// for session.idle on the event stream.
func TestDeliverConfirmsOnSessionIdle(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter.Watch(ctx)
	if err := adapter.Adopt("ses_1"); err != nil {
		t.Fatal(err)
	}
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if !result.Delivered() {
		t.Fatalf("delivery must confirm on session.idle, got %+v", result)
	}
	if fake.turnCount != 1 {
		t.Fatalf("exactly one turn must be started, got %d", fake.turnCount)
	}
}

func TestDeliverRefusesUnadoptedSession(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if result.Code != agentbridge.ResultNotFound {
		t.Fatalf("an unadopted session must be not_found, got %+v", result)
	}
	if fake.turnCount != 0 {
		t.Fatal("a refused delivery must not start a turn")
	}
}

func TestDeliverReportsUnreachableServer(t *testing.T) {
	fake := newFakeOpenCode(t)
	fake.server.Close()
	adapter := newTestAdapter(t, fake)
	if err := adapter.Adopt("ses_1"); err != nil {
		t.Fatal(err)
	}
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if result.Code != agentbridge.ResultUnreachable {
		t.Fatalf("a closed server must be unreachable, got %+v", result)
	}
}

func TestDeliverReportsPostRejection(t *testing.T) {
	fake := newFakeOpenCode(t)
	fake.postErr = 1
	adapter := newTestAdapter(t, fake)
	if err := adapter.Adopt("ses_1"); err != nil {
		t.Fatal(err)
	}
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if result.Code != agentbridge.ResultUnknown {
		t.Fatalf("a rejected post must not report delivered, got %+v", result)
	}
}

func TestEventStreamDrivesBusyState(t *testing.T) {
	fake := newFakeOpenCode(t)
	client := NewClient(Config{BaseURL: fake.server.URL, CallTimeout: time.Second})
	adapter := New("node-a", client, nil)
	defer adapter.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter.Watch(ctx)
	fake.push(t, map[string]any{"type": "session.status",
		"properties": map[string]any{"sessionID": "ses_2", "status": map[string]any{"type": "busy"}}})
	waitFor(t, func() bool { return client.Busy("ses_2") })
	fake.push(t, map[string]any{"type": "session.idle", "properties": map[string]any{"sessionID": "ses_2"}})
	waitFor(t, func() bool { return !client.Busy("ses_2") })
}

func TestAwaitIdleFailsWhenNoIdleIsPublished(t *testing.T) {
	fake := newFakeOpenCode(t)
	client := NewClient(Config{BaseURL: fake.server.URL, IdleWait: 200 * time.Millisecond})
	if _, err := client.AwaitIdle(context.Background(), "ses_never_idle"); err == nil {
		t.Fatal("a session that never reports idle must not be treated as delivered")
	}
}

func TestMessagesExposeAssistantError(t *testing.T) {
	fake := newFakeOpenCode(t)
	client := NewClient(Config{BaseURL: fake.server.URL, CallTimeout: time.Second})
	messages, err := client.Messages(context.Background(), "ses_1")
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected two messages, got %d", len(messages))
	}
	if messages[0].Role != "user" || len(messages[0].Texts) == 0 {
		t.Fatalf("user message must round trip, got %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Texts[0] != "done" {
		t.Fatalf("assistant message must round trip, got %+v", messages[1])
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met in time")
}

func TestUnreachableClassification(t *testing.T) {
	if !IsUnreachable(fmt.Errorf("wrapped: %w", ErrUnreachable)) {
		t.Fatal("wrapped unreachable must be classified")
	}
	if IsUnreachable(errors.New("other")) {
		t.Fatal("unrelated errors must not be classified as unreachable")
	}
}

// A POST that times out after the message landed must be reconciled into
// delivered, never retried and never reported as a failure.
func TestDeliverReconcilesAmbiguousPost(t *testing.T) {
	fake := newFakeOpenCode(t)
	client := NewClient(Config{BaseURL: fake.server.URL, CallTimeout: 80 * time.Millisecond,
		IdleWait: 3 * time.Second})
	adapter := New("node-a", client, nil)
	defer adapter.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter.Watch(ctx)
	if err := adapter.Adopt("ses_1"); err != nil {
		t.Fatal(err)
	}
	// The fake records the user message, then answers slowly enough to blow the
	// client deadline, then publishes idle.
	fake.slowPost = 250 * time.Millisecond
	message := envelope()
	message.ID = "msg-reconcile"
	result := adapter.Deliver(context.Background(), address("ses_1"), message)
	if !result.Delivered() {
		t.Fatalf("an ambiguous post that landed must be delivered, got %+v", result)
	}
	if fake.turnCount != 1 {
		t.Fatalf("an ambiguous delivery must never be retried, got %d turns", fake.turnCount)
	}
}

// A POST that times out and left no trace must stay unknown.
func TestDeliverKeepsUnknownWhenAmbiguousPostDidNotLand(t *testing.T) {
	fake := newFakeOpenCode(t)
	client := NewClient(Config{BaseURL: fake.server.URL, CallTimeout: 60 * time.Millisecond,
		IdleWait: 150 * time.Millisecond})
	adapter := New("node-a", client, nil)
	defer adapter.Close()
	if err := adapter.Adopt("ses_1"); err != nil {
		t.Fatal(err)
	}
	fake.dropPost = true
	fake.slowPost = 200 * time.Millisecond
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if result.Delivered() {
		t.Fatalf("a post that never landed must not be delivered, got %+v", result)
	}
	if result.Code != agentbridge.ResultUnknown {
		t.Fatalf("an ambiguous outcome must stay unknown, got %+v", result)
	}
}

// Health must reflect the server, not a remembered flag: OpenCode is optional
// and the user can stop it at any moment.
func TestHealthFollowsTheServerRatherThanAMemory(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	if health := adapter.Health(context.Background()); !health.Ready {
		t.Fatalf("a running server must read as ready, got %+v", health)
	}
	fake.server.Close()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if !adapter.Health(context.Background()).Ready {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("health must stop claiming ready once the server is gone")
}

// Every session the shared server reports must be publishable: the requirement
// is that any session can reach any other, so no per-session setup is allowed.
func TestListEndpointsPublishesEverySessionByDefault(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("both server-reported sessions must be published, got %+v", endpoints)
	}
}

func TestAdoptNarrowsPublicationOnlyWhenAsked(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	if err := adapter.Adopt("ses_1"); err != nil {
		t.Fatal(err)
	}
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].ID != "ses_1" {
		t.Fatalf("adopt must narrow publication, got %+v", endpoints)
	}
	adapter.Unrestrict()
	if endpoints, err = adapter.ListEndpoints(context.Background()); err != nil || len(endpoints) != 2 {
		t.Fatalf("unrestrict must publish everything again, got %+v err=%v", endpoints, err)
	}
}

// OpenCode publishes no stable caller identity in MCP metadata, so the adapter
// answers only when it can do so without guessing.
func TestResolveCallerRefusesToGuessBetweenSessions(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	if _, err := adapter.ResolveCaller(context.Background(), agentbridge.CallerContext{}); err == nil {
		t.Fatal("several sessions must not be guessed between")
	}
}
