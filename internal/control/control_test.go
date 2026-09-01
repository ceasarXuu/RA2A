package control

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/lannode"
)

type fakeLAN struct {
	peers       []lannode.Peer
	sessions    map[string][]lannode.Session
	blocked     map[string]bool
	delays      map[string]time.Duration
	sentPeer    lannode.Peer
	sentPeers   []lannode.Peer
	sent        lannode.Message
	sendErr     error
	sendErrs    []error
	changedPeer lannode.Peer
	changeErr   error
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

func (lan *fakeLAN) RefreshPeer(context.Context, string, string) (lannode.Peer, error) {
	return lan.changedPeer, lan.changeErr
}
func (lan *fakeLAN) ListSessions(ctx context.Context, peer lannode.Peer) ([]lannode.Session, error) {
	if lan.blocked[peer.ID] {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if delay := lan.delays[peer.ID]; delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	sessions, ok := lan.sessions[peer.ID]
	if !ok {
		return nil, errors.New("offline")
	}
	return sessions, nil
}

func TestCoordinatorAllowsSlowCodexSessionEnumeration(t *testing.T) {
	lan := &fakeLAN{
		peers:  []lannode.Peer{{ID: "remote", Name: "Remote"}},
		delays: map[string]time.Duration{"remote": 3200 * time.Millisecond},
		sessions: map[string][]lannode.Session{
			"remote": {{ID: "thread-1", Title: "Ready", Status: "idle"}},
		},
	}
	targets, err := NewCoordinator("local", lan).ListTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "remote" || targets[0].Status != "ready" || targets[0].SessionsStale {
		t.Fatalf("targets = %#v, want slow remote peer", targets)
	}
}

func TestCoordinatorKeepsDiscoveredTargetWhenAnotherPeerHangs(t *testing.T) {
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
	if len(targets) != 2 || targets[0].ID != "blocked" || targets[0].Status != "unreachable" || targets[1].ID != "online" || targets[1].Status != "ready" {
		t.Fatalf("targets = %#v, want visible blocked and online peers", targets)
	}
}
func (lan *fakeLAN) SendMessage(_ context.Context, peer lannode.Peer, message lannode.Message) error {
	lan.sentPeer, lan.sent = peer, message
	lan.sentPeers = append(lan.sentPeers, peer)
	if len(lan.sendErrs) > 0 {
		err := lan.sendErrs[0]
		lan.sendErrs = lan.sendErrs[1:]
		return err
	}
	return lan.sendErr
}

func TestCoordinatorMarksDiscoveredOfflineTargetUnreachable(t *testing.T) {
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
	if len(targets) != 2 || targets[0].ID != "online" || targets[0].Status != "ready" || targets[0].Sessions[0].ID != "thread-1" {
		t.Fatalf("targets = %#v", targets)
	}
	if targets[1].ID != "stale" || targets[1].Status != "unreachable" || targets[1].SessionsStale || len(targets[1].Sessions) != 0 {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestCoordinatorRetainsLastSessionsWhenTargetDegrades(t *testing.T) {
	lan := &fakeLAN{
		peers: []lannode.Peer{{ID: "remote", Name: "Remote"}},
		sessions: map[string][]lannode.Session{
			"remote": {{ID: "thread-1", Title: "Ready", Status: "idle"}},
		},
	}
	coordinator := NewCoordinator("local", lan)
	first, err := coordinator.ListTargets(context.Background())
	if err != nil || len(first) != 1 || first[0].Status != "ready" {
		t.Fatalf("first targets=%#v err=%v", first, err)
	}
	delete(lan.sessions, "remote")
	second, err := coordinator.ListTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Status != "degraded" || !second[0].SessionsStale || len(second[0].Sessions) != 1 || second[0].Sessions[0].ID != "thread-1" {
		t.Fatalf("second targets=%#v", second)
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

func TestCoordinatorPreservesRemoteDeliveryUnknown(t *testing.T) {
	lan := &fakeLAN{
		peers:   []lannode.Peer{{ID: "remote", Name: "Remote"}},
		sendErr: errors.New("send message: DELIVERY_UNKNOWN: timeout"),
	}
	err := NewCoordinator("local", lan).Send(context.Background(), SendRequest{
		To: "ra2a://remote/thread-2", Text: "hello", SourceSessionID: "thread-1",
	})
	if !errors.Is(err, ErrDeliveryUnknown) {
		t.Fatalf("error = %v", err)
	}
}

func TestCoordinatorRetriesChangedEndpointAfterHandshakeFailure(t *testing.T) {
	oldPeer := lannode.Peer{ID: "remote", Name: "Remote", Address: "192.0.2.10:4000"}
	newPeer := lannode.Peer{ID: "remote", Name: "Remote", Address: "192.0.2.10:5000"}
	lan := &fakeLAN{
		peers:       []lannode.Peer{oldPeer},
		sendErrs:    []error{fmt.Errorf("%w: handshake timeout", lannode.ErrPeerUnreachable), nil},
		changedPeer: newPeer,
	}
	err := NewCoordinator("local", lan).Send(context.Background(), SendRequest{
		To: "ra2a://remote/thread-2", Text: "hello", SourceSessionID: "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lan.sentPeers, []lannode.Peer{oldPeer, newPeer}) {
		t.Fatalf("sent peers = %#v", lan.sentPeers)
	}
}

func TestCoordinatorReportsHandshakeFailureAsTargetUnreachable(t *testing.T) {
	peer := lannode.Peer{ID: "remote", Name: "Remote", Address: "192.0.2.10:4000"}
	lan := &fakeLAN{
		peers:     []lannode.Peer{peer},
		sendErr:   fmt.Errorf("%w: handshake timeout", lannode.ErrPeerUnreachable),
		changeErr: context.DeadlineExceeded,
	}
	err := NewCoordinator("local", lan).Send(context.Background(), SendRequest{
		To: "ra2a://remote/thread-2", Text: "hello", SourceSessionID: "thread-1",
	})
	if !errors.Is(err, ErrTargetUnreachable) {
		t.Fatalf("error = %v, want TARGET_UNREACHABLE", err)
	}
	if errors.Is(err, ErrDeliveryUnknown) {
		t.Fatalf("pre-delivery failure must not be DELIVERY_UNKNOWN: %v", err)
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

func TestHTTPHandlerMapsTargetUnreachableToServiceUnavailable(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(
		`{"to":"ra2a://n/s","text":"x","sourceSessionId":"c"}`,
	))
	recorder := httptest.NewRecorder()
	NewHandler(&fakeBackend{sendErr: ErrTargetUnreachable}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
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
