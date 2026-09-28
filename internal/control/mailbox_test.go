package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/mailbox"
)

func newMailboxServer(t *testing.T) (*httptest.Server, *mailbox.Store) {
	t.Helper()
	store, err := mailbox.OpenStore(filepath.Join(t.TempDir(), "mailbox"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	server := httptest.NewServer(NewHandler(&fakeBackend{}, store))
	t.Cleanup(server.Close)
	return server, store
}

func postJSON(t *testing.T, server *httptest.Server, path string, payload any) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Post(server.URL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer response.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return response.StatusCode, decoded
}

func getJSON(t *testing.T, server *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	response, err := server.Client().Get(server.URL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer response.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return response.StatusCode, decoded
}

func TestMailboxDeliveryIsStoredWithoutAnyAgentCall(t *testing.T) {
	backend := &fakeBackend{}
	store, err := mailbox.OpenStore(filepath.Join(t.TempDir(), "mailbox"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(backend, store))
	defer server.Close()

	status, payload := postJSON(t, server, "/v1/mailbox", map[string]any{
		"to": "harness-1", "text": "status?", "from": "ra2a://rog306/cli-1",
	})
	if status != http.StatusOK {
		t.Fatalf("mailbox delivery must succeed, got %d %+v", status, payload)
	}
	if payload["delivery"] != "stored" {
		t.Fatalf("mailbox delivery must be reported as stored, got %+v", payload)
	}
	if backend.sent.To != "" || backend.sent.Text != "" {
		t.Fatalf("mailbox delivery must never reach an agent, got %+v", backend.sent)
	}
}

func TestMailboxReadIsPollingAndConsuming(t *testing.T) {
	server, _ := newMailboxServer(t)
	if status, _ := postJSON(t, server, "/v1/mailbox", map[string]any{"to": "bob", "text": "first"}); status != http.StatusOK {
		t.Fatalf("seed: %d", status)
	}
	if status, _ := postJSON(t, server, "/v1/mailbox", map[string]any{"to": "bob", "text": "second"}); status != http.StatusOK {
		t.Fatalf("seed: %d", status)
	}

	status, peeked := getJSON(t, server, "/v1/mailbox?to=bob&peek=true")
	if status != http.StatusOK {
		t.Fatalf("peek: %d", status)
	}
	if int(peeked["pending"].(float64)) != 2 || len(peeked["messages"].([]any)) != 2 {
		t.Fatalf("peek must not consume, got %+v", peeked)
	}
	status, consumed := getJSON(t, server, "/v1/mailbox?to=bob")
	if status != http.StatusOK {
		t.Fatalf("read: %d", status)
	}
	messages := consumed["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("consuming read must return the batch, got %+v", consumed)
	}
	if messages[0].(map[string]any)["text"] != "first" {
		t.Fatalf("messages must be returned oldest first, got %+v", messages[0])
	}
	_, drained := getJSON(t, server, "/v1/mailbox?to=bob")
	if int(drained["pending"].(float64)) != 0 || len(drained["messages"].([]any)) != 0 {
		t.Fatalf("a consumed message must not reappear, got %+v", drained)
	}
}

func TestMailboxRoutesRejectUnusableInput(t *testing.T) {
	server, _ := newMailboxServer(t)
	if status, _ := postJSON(t, server, "/v1/mailbox", map[string]any{"to": "bad recipient", "text": "x"}); status != http.StatusBadRequest {
		t.Fatalf("an invalid recipient must be rejected, got %d", status)
	}
	if status, _ := postJSON(t, server, "/v1/mailbox", map[string]any{"to": "bob", "text": "  "}); status != http.StatusBadRequest {
		t.Fatalf("empty text must be rejected, got %d", status)
	}
	if status, _ := getJSON(t, server, "/v1/mailbox?to="); status != http.StatusBadRequest {
		t.Fatalf("a missing recipient must be rejected, got %d", status)
	}
}

// A mailbox address must never be routed to an agent, and an endpoint address
// must never be treated as a mailbox.
func TestMailboxAddressNeverReachesAnAdapter(t *testing.T) {
	registry := &stubRegistry{
		result: agentbridge.Delivered("turn-1"),
		caller: agentbridge.Address{NodeID: "node-a", EndpointID: "caller-1"},
	}
	registry.endpoints = []agentbridge.Endpoint{endpointFixture("node-a", "caller-1", agentbridge.AgentCodexApp)}
	store, err := mailbox.OpenStore(filepath.Join(t.TempDir(), "mailbox"))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewAdapterCoordinator("node-a", &failingLAN{}, registry).WithMailbox(store)

	if err := coordinator.Send(context.Background(), SendRequest{
		To: "ra2a://node-a/mailbox/harness-1", Text: "hello", From: "ra2a://node-a/caller-1",
	}); err != nil {
		t.Fatalf("mailbox delivery must succeed: %v", err)
	}
	if len(registry.deliveries) != 0 {
		t.Fatalf("a mailbox address must not reach an adapter, got %+v", registry.deliveries)
	}
	messages, pending, err := store.Read("harness-1", 10, true)
	if err != nil || pending != 1 || len(messages) != 1 {
		t.Fatalf("mailbox must hold the message, pending=%d err=%v", pending, err)
	}
	if messages[0].From != "ra2a://node-a/caller-1" {
		t.Fatalf("mailbox must keep the sender, got %q", messages[0].From)
	}

	if err := coordinator.Send(context.Background(), SendRequest{
		To: "ra2a://node-a/app-1", Text: "hello", From: "ra2a://node-a/caller-1",
	}); err != nil {
		t.Fatalf("endpoint delivery must still work: %v", err)
	}
	if len(registry.deliveries) != 1 {
		t.Fatalf("an endpoint address must still reach its adapter, got %+v", registry.deliveries)
	}
}

func TestMailboxRejectsMalformedNamespaceAddress(t *testing.T) {
	registry := &stubRegistry{
		result: agentbridge.Delivered("turn-1"),
		caller: agentbridge.Address{NodeID: "node-a", EndpointID: "caller-1"},
	}
	registry.endpoints = []agentbridge.Endpoint{endpointFixture("node-a", "caller-1", agentbridge.AgentCodexApp)}
	store, err := mailbox.OpenStore(filepath.Join(t.TempDir(), "mailbox"))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewAdapterCoordinator("node-a", &failingLAN{}, registry).WithMailbox(store)
	err = coordinator.Send(context.Background(), SendRequest{
		To: "ra2a://node-a/mailbox/bad recipient", Text: "hello", From: "ra2a://node-a/caller-1",
	})
	if err == nil {
		t.Fatal("a malformed mailbox recipient must be rejected")
	}
	if len(registry.deliveries) != 0 {
		t.Fatalf("a malformed mailbox address must not reach an adapter, got %+v", registry.deliveries)
	}
}

func TestMailboxesRouteListsRecipients(t *testing.T) {
	server, store := newMailboxServer(t)
	if status, _ := postJSON(t, server, "/v1/mailbox", map[string]any{"to": "bob", "text": "x"}); status != http.StatusOK {
		t.Fatal(status)
	}
	if status, _ := postJSON(t, server, "/v1/mailbox", map[string]any{"to": "alice", "text": "x"}); status != http.StatusOK {
		t.Fatal(status)
	}
	status, payload := getJSON(t, server, "/v1/mailboxes")
	if status != http.StatusOK {
		t.Fatalf("list: %d", status)
	}
	names := payload["mailboxes"].([]any)
	if len(names) != 2 || names[0] != "alice" || names[1] != "bob" {
		t.Fatalf("unexpected mailbox list: %+v", names)
	}
	stored, err := store.Recipients()
	if err != nil || len(stored) != 2 {
		t.Fatalf("store and route must agree, got %+v err=%v", stored, err)
	}
}

func TestMailboxRoutesAbsentWithoutStore(t *testing.T) {
	server := httptest.NewServer(NewHandler(&fakeBackend{}, nil))
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/v1/mailbox", "application/json",
		bytes.NewReader([]byte(`{"to":"bob","text":"x"}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("without a store the route must be absent, got %d", response.StatusCode)
	}
}

// A LAN message addressed to a mailbox must be stored by the receiving node
// without touching an adapter, and must report the stored delivery.
func TestDeliverMailboxFromLANEnvelope(t *testing.T) {
	store, err := mailbox.OpenStore(filepath.Join(t.TempDir(), "mailbox"))
	if err != nil {
		t.Fatal(err)
	}
	envelope := agentbridge.MessageEnvelope{
		ID: "m1", ProtocolVersion: agentbridge.ProtocolVersion,
		SourceAddress: "ra2a://rog306/cli-1",
		TargetAddress: mailbox.Address("node-a", "harness-1"),
		Text:          "ping", CreatedAt: time.Now().UTC(),
	}
	result, handled := DeliverMailbox(store, "node-a", envelope)
	if !handled {
		t.Fatal("a mailbox address must be handled by the mailbox service")
	}
	if !result.Delivered() {
		t.Fatalf("mailbox storage must report delivered, got %+v", result)
	}
	messages, _, err := store.Read("harness-1", 10, true)
	if err != nil || len(messages) != 1 {
		t.Fatalf("message must be stored, got %+v err=%v", messages, err)
	}
	if messages[0].From != "ra2a://rog306/cli-1" || messages[0].Text != "ping" {
		t.Fatalf("message must keep sender and text, got %+v", messages[0])
	}
}

func TestDeliverMailboxIgnoresEndpointAddresses(t *testing.T) {
	store, err := mailbox.OpenStore(filepath.Join(t.TempDir(), "mailbox"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"ra2a://node-a/thread-1",
		"ra2a://node-a/mailbox",
		"ra2a://node-a/inbox/harness",
	} {
		if _, handled := DeliverMailbox(store, "node-a", agentbridge.MessageEnvelope{
			ProtocolVersion: agentbridge.ProtocolVersion, TargetAddress: target, Text: "x",
		}); handled {
			t.Fatalf("address %q must not be treated as a mailbox", target)
		}
	}
	if _, handled := DeliverMailbox(nil, "node-a", agentbridge.MessageEnvelope{
		TargetAddress: mailbox.Address("node-a", "harness-1"), Text: "x",
	}); handled {
		t.Fatal("without a store the envelope must fall through to endpoint routing")
	}
}

// A mailbox on a peer must be forwarded so the peer stores it, not refused as
// unreachable. This is what makes the channel cross-node.
func TestRemoteMailboxIsForwardedNotRefused(t *testing.T) {
	registry := &stubRegistry{
		result: agentbridge.Delivered("turn-1"),
		caller: agentbridge.Address{NodeID: "node-a", EndpointID: "caller-1"},
	}
	registry.endpoints = []agentbridge.Endpoint{endpointFixture("node-a", "caller-1", agentbridge.AgentCodexApp)}
	lan := &recordingLAN{}
	store, err := mailbox.OpenStore(filepath.Join(t.TempDir(), "mailbox"))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewAdapterCoordinator("node-a", lan, registry).WithMailbox(store)
	if err := coordinator.Send(context.Background(), SendRequest{
		To: "ra2a://node-b/mailbox/harness", Text: "hello", From: "ra2a://node-a/caller-1",
	}); err != nil {
		t.Fatalf("a remote mailbox must be forwarded, got %v", err)
	}
	if len(lan.sent) != 1 {
		t.Fatalf("forwarding must go over LAN, got %+v", lan.sent)
	}
	if lan.sent[0].TargetSessionID != "mailbox/harness" {
		t.Fatalf("the mailbox path must survive the LAN hop, got %+v", lan.sent[0])
	}
	if len(registry.deliveries) != 0 {
		t.Fatalf("a remote mailbox must not touch a local adapter, got %+v", registry.deliveries)
	}
	if messages, _, _ := store.Read("harness", 10, true); len(messages) != 0 {
		t.Fatal("a remote mailbox must not be stored locally")
	}
}

// A legacy caller that only sets sourceSessionId must keep working.
func TestLegacySourceSessionIDStillResolves(t *testing.T) {
	registry := &stubRegistry{
		result: agentbridge.Delivered("turn-1"),
		caller: agentbridge.Address{NodeID: "node-a", EndpointID: "legacy-caller"},
	}
	registry.endpoints = []agentbridge.Endpoint{endpointFixture("node-a", "legacy-caller", agentbridge.AgentCodexApp)}
	coordinator := NewAdapterCoordinator("node-a", &failingLAN{}, registry)
	if err := coordinator.Send(context.Background(), SendRequest{
		To: "ra2a://node-a/cli-1", Text: "hi", SourceSessionID: "legacy-caller",
	}); err != nil {
		t.Fatalf("legacy caller must still be accepted, got %v", err)
	}
	if len(registry.deliveries) != 1 {
		t.Fatalf("delivery must still reach the adapter, got %+v", registry.deliveries)
	}
	if registry.deliveries[0].SourceAddress != "ra2a://node-a/legacy-caller" {
		t.Fatalf("source address must be normalised, got %+q", registry.deliveries[0].SourceAddress)
	}
	unknown := &stubRegistry{}
	unknown.endpoints = []agentbridge.Endpoint{endpointFixture("node-a", "caller", agentbridge.AgentCodexApp)}
	strict := NewAdapterCoordinator("node-a", &failingLAN{}, unknown)
	err := strict.Send(context.Background(), SendRequest{
		To: "ra2a://node-a/cli-1", Text: "hi", SourceSessionID: "not-published",
	})
	if err == nil || !strings.Contains(err.Error(), "CALLER_SESSION_UNKNOWN") {
		t.Fatalf("an unpublished legacy caller must be refused, got %v", err)
	}
}
