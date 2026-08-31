package control

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/lannode"
)

type fakeLAN struct {
	peers    []lannode.Peer
	sessions map[string][]lannode.Session
	blocked  map[string]bool
	sentPeer lannode.Peer
	sent     lannode.Message
	sendErr  error
}

func (lan *fakeLAN) Peers() []lannode.Peer { return lan.peers }
func (lan *fakeLAN) Peer(id string) (lannode.Peer, bool) {
	for _, peer := range lan.peers {
		if peer.ID == id {
			return peer, true
		}
	}
	return lannode.Peer{}, false
}
func (lan *fakeLAN) ListSessions(ctx context.Context, peer lannode.Peer) ([]lannode.Session, error) {
	if lan.blocked[peer.ID] {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	sessions, ok := lan.sessions[peer.ID]
	if !ok {
		return nil, errors.New("offline")
	}
	return sessions, nil
}

func TestCoordinatorReturnsReachableTargetsWhenAnotherPeerHangs(t *testing.T) {
	lan := &fakeLAN{
		peers:   []lannode.Peer{{ID: "blocked", Name: "Blocked"}, {ID: "online", Name: "Online"}},
		blocked: map[string]bool{"blocked": true},
		sessions: map[string][]lannode.Session{
			"online": {{ID: "thread-1", Title: "Ready", Status: "idle"}},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	targets, err := NewCoordinator("local", lan).ListTargets(ctx)
	if err != nil {
		t.Fatalf("healthy peer should survive blocked peer: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "online" {
		t.Fatalf("targets = %#v, want online peer", targets)
	}
}
func (lan *fakeLAN) SendMessage(_ context.Context, peer lannode.Peer, message lannode.Message) error {
	lan.sentPeer, lan.sent = peer, message
	return lan.sendErr
}

func TestCoordinatorListsOnlyReachableTargets(t *testing.T) {
	lan := &fakeLAN{
		peers: []lannode.Peer{{ID: "online", Name: "Online"}, {ID: "stale", Name: "Stale"}},
		sessions: map[string][]lannode.Session{
			"online": {{ID: "thread-1", Title: "Ready", Status: "idle"}},
		},
	}
	targets, err := NewCoordinator("local", lan).ListTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "online" || targets[0].Sessions[0].ID != "thread-1" {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestCoordinatorSendsWithCallerAddressAndMessageID(t *testing.T) {
	lan := &fakeLAN{peers: []lannode.Peer{{ID: "remote", Name: "Remote"}}}
	err := NewCoordinator("local", lan).Send(context.Background(), SendRequest{
		To: "ra2a://remote/thread-2", Text: "hello", SourceSessionID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if lan.sentPeer.ID != "remote" || lan.sent.TargetSessionID != "thread-2" || lan.sent.Text != "hello" {
		t.Fatalf("peer=%#v message=%#v", lan.sentPeer, lan.sent)
	}
	if lan.sent.Source != "ra2a://local/thread-1" || lan.sent.MessageID == "" {
		t.Fatalf("message identity = %#v", lan.sent)
	}
}

func TestCoordinatorRejectsUnknownTarget(t *testing.T) {
	err := NewCoordinator("local", &fakeLAN{}).Send(context.Background(), SendRequest{
		To: "ra2a://missing/thread-2", Text: "hello", SourceSessionID: "thread-1",
	})
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPClientRoundTripsTargetsAndSend(t *testing.T) {
	backend := &fakeBackend{targets: []Target{{ID: "node-b", Name: "Node B"}}}
	server := httptest.NewServer(NewHandler(backend))
	defer server.Close()
	client := NewClient(server.URL)
	targets, err := client.ListTargets(context.Background())
	if err != nil || len(targets) != 1 || targets[0].ID != "node-b" {
		t.Fatalf("targets=%#v err=%v", targets, err)
	}
	request := SendRequest{To: "ra2a://node-b/thread", Text: "hello", SourceSessionID: "caller"}
	if err := client.Send(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if backend.sent != request {
		t.Fatalf("sent=%#v", backend.sent)
	}
}

func TestHTTPClientPreservesDeliveryErrorCode(t *testing.T) {
	backend := &fakeBackend{sendErr: errors.New("SESSION_BUSY")}
	server := httptest.NewServer(NewHandler(backend))
	defer server.Close()
	err := NewClient(server.URL).Send(context.Background(), SendRequest{To: "ra2a://n/s", Text: "x", SourceSessionID: "c"})
	if err == nil || !strings.Contains(err.Error(), "SESSION_BUSY") {
		t.Fatalf("error = %v", err)
	}
}

type fakeBackend struct {
	targets []Target
	sent    SendRequest
	sendErr error
}

func (backend *fakeBackend) ListTargets(context.Context) ([]Target, error) {
	return backend.targets, nil
}
func (backend *fakeBackend) Send(_ context.Context, request SendRequest) error {
	backend.sent = request
	return backend.sendErr
}
