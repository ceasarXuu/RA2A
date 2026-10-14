package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

// fakeOpenCode reproduces the real server surface: session listing, message
// posting, and an SSE event stream that publishes busy/idle.
type fakeOpenCode struct {
	server        *httptest.Server
	messages      chan string
	mu            sync.Mutex
	turnCount     int
	postErr       int
	slowPost      time.Duration
	dropPost      bool
	neverRecord   bool
	delayedRecord time.Duration
	lastPosted    string
	recorded      bool
}

func newFakeOpenCode(t *testing.T) *fakeOpenCode {
	t.Helper()
	leaseDir := t.TempDir()
	t.Setenv("RA2A_OC_SESSION_DIR", leaseDir)
	for _, id := range []string{"ses_1", "ses_2"} {
		closeLease, err := ocsession.Register(leaseDir, id)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(closeLease)
	}
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
		fake.mu.Lock()
		userText := "unrelated earlier message"
		if fake.lastPosted != "" && fake.recorded {
			userText = fake.lastPosted
		}
		fake.mu.Unlock()
		_ = json.NewEncoder(writer).Encode([]map[string]any{
			{"info": map[string]any{"sessionID": request.PathValue("id"), "role": "user",
				"time": map[string]any{"completed": float64(1)}},
				"parts": []map[string]any{{"type": "text", "text": userText}}},
			{"info": map[string]any{"sessionID": request.PathValue("id"), "role": "assistant",
				"time": map[string]any{"completed": float64(2)}, "error": nil},
				"parts": []map[string]any{{"type": "text", "text": "done"}}},
		})
	})
	mux.HandleFunc("POST /session/{id}/prompt_async", func(writer http.ResponseWriter, request *http.Request) {
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
		fake.mu.Lock()
		reject := fake.postErr > 0
		if reject {
			fake.postErr--
		}
		fake.mu.Unlock()
		if reject {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"name":"BadRequest"}`))
			return
		}
		fake.mu.Lock()
		fake.lastPosted = payload.Parts[0].Text
		fake.turnCount++
		dropPost, neverRecord := fake.dropPost, fake.neverRecord
		slow, delayed := fake.slowPost, fake.delayedRecord
		fake.mu.Unlock()
		if dropPost {
			// The host neither acknowledges nor records: the caller's deadline
			// expires with the message taken by nobody.
			if slow > 0 {
				time.Sleep(slow)
			}
			return
		}
		if slow > 0 {
			time.Sleep(slow)
		}
		if !neverRecord {
			// prompt_async is acknowledged as soon as the host has taken the
			// message; the queued message shows up in history later, which is
			// what delivery into a busy session looks like. nothing here waits
			// for that, because the acknowledgement is the delivery signal.
			record := func() {
				fake.mu.Lock()
				fake.recorded = true
				fake.mu.Unlock()
			}
			if delayed > 0 {
				go func() {
					time.Sleep(delayed)
					record()
				}()
			} else {
				record()
			}
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	// The blocking endpoint must never be used for delivery. It streams the
	// assistant's answer, so a delivery through it would take as long as the
	// recipient's turn and blow every transport budget that carries the answer.
	// Registering it here turns a regression into a loud failure instead of a
	// silent reappearance of the timeout.
	mux.HandleFunc("POST /session/{id}/message", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"name":"BlockingEndpointUsedForDelivery"}`))
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
	adapter := New("node-a", Config{BaseURL: fake.server.URL, CallTimeout: 3 * time.Second,
		IdleWait: 3 * time.Second}, nil)
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

func TestListEndpointsPublishesEveryReportedSession(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("every session the shared server reports must be published, got %+v", endpoints)
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

// Nothing is published when the server reports no sessions at all.
func TestListEndpointsIsEmptyWhenServerHasNoSessions(t *testing.T) {
	fake := newFakeOpenCode(t)
	empty := newFakeAppServerWithNoSessions(t, fake)
	adapter := New("node-a", Config{BaseURL: empty, CallTimeout: 2 * time.Second}, nil)
	defer adapter.Close()
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("an empty server must publish nothing, got %+v", endpoints)
	}
}

// The host's acknowledgement is the delivery verdict. Nothing waits for the
// turn the message starts, because that turn belongs to the recipient.
func TestDeliverConfirmsOnHostAcknowledgement(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter.Watch(ctx)
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if !result.Delivered() {
		t.Fatalf("delivery must confirm on the host acknowledgement, got %+v", result)
	}
	if total := fake.turnTotal(); total != 1 {
		t.Fatalf("exactly one turn must be started, got %d", total)
	}
}

// Any session the shared server reports must be deliverable: a local registry
// must never be able to hide sessions from the mesh.
func TestDeliverReachesAnyReportedSession(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter.Watch(ctx)
	result := adapter.Deliver(context.Background(), address("ses_2"), envelope())
	if !result.Delivered() {
		t.Fatalf("a reported session must be deliverable without adoption, got %+v", result)
	}
}

func TestDeliverReportsUnreachableServer(t *testing.T) {
	fake := newFakeOpenCode(t)
	fake.server.Close()
	adapter := newTestAdapter(t, fake)
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if result.Code != agentbridge.ResultUnreachable {
		t.Fatalf("a closed server must be unreachable, got %+v", result)
	}
}

func TestDeliverReportsPostRejection(t *testing.T) {
	fake := newFakeOpenCode(t)
	fake.configure(func(f *fakeOpenCode) { f.postErr = 1 })
	adapter := newTestAdapter(t, fake)
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if result.Code != agentbridge.ResultUnknown {
		t.Fatalf("a rejected post must not report delivered, got %+v", result)
	}
}

func TestEventStreamDrivesBusyState(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := New("node-a", Config{BaseURL: fake.server.URL, CallTimeout: 3 * time.Second,
		IdleWait: 3 * time.Second}, nil)
	// The adapter owns the client that subscribes to the event stream, so the
	// busy assertions must read that one rather than a second connection.
	client := adapter.client
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
	adapter := New("node-a", Config{BaseURL: fake.server.URL, CallTimeout: 80 * time.Millisecond,
		IdleWait: 3 * time.Second, LandedBudget: 2 * time.Second, LandedPoll: 50 * time.Millisecond}, nil)
	defer adapter.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adapter.Watch(ctx)
	// The fake records the user message, then answers slowly enough to blow the
	// client deadline, then publishes idle.
	fake.configure(func(f *fakeOpenCode) { f.slowPost = 250 * time.Millisecond })
	message := envelope()
	message.ID = "msg-reconcile"
	result := adapter.Deliver(context.Background(), address("ses_1"), message)
	if !result.Delivered() {
		t.Fatalf("an ambiguous post that landed must be delivered, got %+v", result)
	}
	if total := fake.turnTotal(); total != 1 {
		t.Fatalf("an ambiguous delivery must never be retried, got %d turns", total)
	}
}

// A POST that times out and left no trace must stay unknown.
func TestDeliverKeepsUnknownWhenAmbiguousPostDidNotLand(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := New("node-a", Config{BaseURL: fake.server.URL, CallTimeout: 60 * time.Millisecond,
		IdleWait: 150 * time.Millisecond, LandedBudget: 400 * time.Millisecond,
		LandedPoll: 50 * time.Millisecond}, nil)
	defer adapter.Close()
	fake.configure(func(f *fakeOpenCode) { f.dropPost, f.slowPost = true, 200*time.Millisecond })
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

// OpenCode publishes no stable caller identity in MCP metadata, so the adapter
// answers only when it can do so without guessing.
func TestResolveCallerRefusesToGuessBetweenSessions(t *testing.T) {
	fake := newFakeOpenCode(t)
	adapter := newTestAdapter(t, fake)
	if _, err := adapter.ResolveCaller(context.Background(), agentbridge.CallerContext{}); err == nil {
		t.Fatal("several sessions must not be guessed between")
	}
}

// newFakeAppServerWithNoSessions serves an empty session list, standing in for an
// OpenCode server that has not been used yet.
func newFakeAppServerWithNoSessions(t *testing.T, reference *fakeOpenCode) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("[]"))
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return "http://" + listener.Addr().String()
}

// Delivery into a busy session must not wait for the queued message to become
// visible. A previous implementation posted to the blocking endpoint and then
// polled the history for the queued message, which made a delivery take as long
// as the recipient's turn: minutes on an active agent session, far beyond what
// any transport carrying the answer is willing to wait. The acknowledgement is
// the delivery signal, and the history is only consulted when it is lost.
func TestDeliverReturnsWithoutWaitingForTheQueuedMessage(t *testing.T) {
	fake := newFakeOpenCode(t)
	// The host records the queued message long after it acknowledged it, which
	// is what a busy session looks like.
	fake.configure(func(f *fakeOpenCode) { f.delayedRecord = 3 * time.Second })
	adapter := New("node-a", Config{
		BaseURL: fake.server.URL, CallTimeout: 500 * time.Millisecond,
		IdleWait: 150 * time.Millisecond, LandedBudget: 500 * time.Millisecond,
		LandedPoll: 50 * time.Millisecond,
	}, nil)
	defer adapter.Close()
	started := time.Now()
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	elapsed := time.Since(started)
	if !result.Delivered() {
		t.Fatalf("an acknowledged message must be delivered, got %+v", result)
	}
	if elapsed > time.Second {
		t.Fatalf("delivery must not wait for the queued message to be recorded, took %s", elapsed)
	}
	if total := fake.turnTotal(); total != 1 {
		t.Fatalf("the message must never be re-posted, got %d posts", total)
	}
}

// If the host neither accepted the message nor records it, the delivery really
// did fail and must not be reported as success.
func TestDeliverFailsWhenAQueuedMessageNeverLands(t *testing.T) {
	fake := newFakeOpenCode(t)
	fake.configure(func(f *fakeOpenCode) {
		f.dropPost, f.neverRecord, f.slowPost = true, true, 300*time.Millisecond
	})
	adapter := New("node-a", Config{
		BaseURL: fake.server.URL, CallTimeout: 60 * time.Millisecond,
		IdleWait: 150 * time.Millisecond, LandedBudget: 500 * time.Millisecond,
		LandedPoll: 50 * time.Millisecond,
	}, nil)
	defer adapter.Close()
	result := adapter.Deliver(context.Background(), address("ses_1"), envelope())
	if result.Delivered() {
		t.Fatalf("a message that never lands must not be reported delivered, got %+v", result)
	}
	if result.NativeErrorClass != "not_accepted" {
		t.Fatalf("the failure must be classified as not_accepted, got %q", result.NativeErrorClass)
	}
}

func (fake *fakeOpenCode) turnTotal() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.turnCount
}

// configure mutates the fake under its lock. Tests must use it instead of
// writing fields directly, because the HTTP handlers read them concurrently.
func (fake *fakeOpenCode) configure(apply func(*fakeOpenCode)) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	apply(fake)
}
