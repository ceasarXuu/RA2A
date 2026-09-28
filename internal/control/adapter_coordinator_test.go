package control

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/lannode"
)

type stubRegistry struct {
	endpoints  []agentbridge.Endpoint
	listErr    error
	deliveries []agentbridge.MessageEnvelope
	result     agentbridge.DeliveryResult
	caller     agentbridge.Address
	callerErr  error
}

func (registry *stubRegistry) ResolveCaller(context.Context, agentbridge.CallerContext) (agentbridge.Address, error) {
	if registry.callerErr != nil {
		return agentbridge.Address{}, registry.callerErr
	}
	return registry.caller, nil
}

func (registry *stubRegistry) Endpoints(context.Context) ([]agentbridge.Endpoint, error) {
	return registry.endpoints, registry.listErr
}

func (registry *stubRegistry) Deliver(_ context.Context, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	registry.deliveries = append(registry.deliveries, envelope)
	return registry.result
}

func (registry *stubRegistry) Health(context.Context) map[agentbridge.AgentKind]agentbridge.Health {
	return map[agentbridge.AgentKind]agentbridge.Health{agentbridge.AgentCodexCLI: agentbridge.Ready()}
}

func endpointFixture(nodeID, id string, kind agentbridge.AgentKind) agentbridge.Endpoint {
	return agentbridge.Endpoint{
		ID: id, Agent: kind, Title: id, Status: agentbridge.EndpointReady,
		Capabilities: []agentbridge.Capability{agentbridge.CapabilityReceiveText},
		Address:      agentbridge.Address{NodeID: nodeID, EndpointID: id},
	}
}

func TestLocalSessionsCarryAgentTypeAndCapabilities(t *testing.T) {
	registry := &stubRegistry{endpoints: []agentbridge.Endpoint{
		endpointFixture("node-a", "app-1", agentbridge.AgentCodexApp),
		endpointFixture("node-a", "cli-1", agentbridge.AgentCodexCLI),
	}}
	coordinator := NewAdapterCoordinator("node-a", nil, registry)
	sessions, err := coordinator.localSessions(context.Background())
	if err != nil {
		t.Fatalf("local sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected both endpoints, got %+v", sessions)
	}
	byID := map[string]lannode.Session{}
	for _, session := range sessions {
		byID[session.ID] = session
	}
	if byID["cli-1"].Agent != string(agentbridge.AgentCodexCLI) {
		t.Fatalf("agent type must reach the LAN view, got %+v", byID["cli-1"])
	}
	if len(byID["cli-1"].Capabilities) == 0 {
		t.Fatalf("capabilities must reach the LAN view, got %+v", byID["cli-1"])
	}
}

func TestLocalSendRoutesThroughRegistry(t *testing.T) {
	registry := &stubRegistry{
		result: agentbridge.Delivered("turn-1"),
		caller: agentbridge.Address{NodeID: "node-a", EndpointID: "caller-1"},
	}
	coordinator := NewAdapterCoordinator("node-a", &failingLAN{}, registry)
	err := coordinator.Send(context.Background(), SendRequest{
		To: "ra2a://node-a/cli-1", Text: "hello", SourceSessionID: "caller-1",
	})
	if err != nil {
		t.Fatalf("local delivery must succeed: %v", err)
	}
	if len(registry.deliveries) != 1 {
		t.Fatalf("delivery must reach the registry, got %+v", registry.deliveries)
	}
	envelope := registry.deliveries[0]
	if envelope.TargetAddress != "ra2a://node-a/cli-1" || envelope.SourceAddress != "ra2a://node-a/caller-1" {
		t.Fatalf("envelope must carry both opaque addresses, got %+v", envelope)
	}
	if envelope.ProtocolVersion != agentbridge.ProtocolVersion {
		t.Fatalf("envelope must carry the current protocol version, got %d", envelope.ProtocolVersion)
	}
	if envelope.ID == "" {
		t.Fatal("envelope must carry a message id")
	}
}

func TestLocalSendFallsBackToLANForRemoteNodes(t *testing.T) {
	registry := &stubRegistry{
		result: agentbridge.Delivered("turn-1"),
		caller: agentbridge.Address{NodeID: "node-a", EndpointID: "caller-1"},
	}
	lan := &recordingLAN{}
	coordinator := NewAdapterCoordinator("node-a", lan, registry)
	if err := coordinator.Send(context.Background(), SendRequest{
		To: "ra2a://node-b/cli-1", Text: "hello", SourceSessionID: "caller-1",
	}); err != nil {
		t.Fatalf("remote delivery must keep the LAN path: %v", err)
	}
	if len(registry.deliveries) != 0 {
		t.Fatalf("remote delivery must not touch the local registry: %+v", registry.deliveries)
	}
	if len(lan.sent) != 1 {
		t.Fatalf("remote delivery must go over LAN, got %+v", lan.sent)
	}
}

func TestResultErrorMapping(t *testing.T) {
	if err := resultError(agentbridge.Delivered("turn-1")); err != nil {
		t.Fatalf("delivered must map to no error, got %v", err)
	}
	cases := []struct {
		result agentbridge.DeliveryResult
		want   error
	}{
		{agentbridge.DeliveryResult{Code: agentbridge.ResultNotFound, Detail: "x"}, ErrTargetNotFound},
		{agentbridge.DeliveryResult{Code: agentbridge.ResultStartRequired, Detail: "x"}, ErrStartRequired},
		{agentbridge.DeliveryResult{Code: agentbridge.ResultUnreachable, Detail: "x"}, ErrTargetUnreachable},
		{agentbridge.DeliveryResult{Code: agentbridge.ResultUnknown, Detail: "x"}, ErrDeliveryUnknown},
	}
	for _, testCase := range cases {
		err := resultError(testCase.result)
		if !errors.Is(err, testCase.want) {
			t.Fatalf("result %+v mapped to %v, want %v", testCase.result, err, testCase.want)
		}
	}
	if err := resultError(agentbridge.DeliveryResult{Code: agentbridge.ResultUnsupported, Detail: "no"}); !strings.Contains(err.Error(), "TARGET_UNSUPPORTED") {
		t.Fatalf("unsupported must be machine readable, got %v", err)
	}
	if err := resultError(agentbridge.DeliveryResult{Code: agentbridge.ResultBusy, Detail: "no"}); !strings.Contains(err.Error(), "SESSION_BUSY") {
		t.Fatalf("busy must stay machine readable for existing callers, got %v", err)
	}
}

func TestListTargetsIncludesLocalNode(t *testing.T) {
	registry := &stubRegistry{endpoints: []agentbridge.Endpoint{
		endpointFixture("node-a", "cli-1", agentbridge.AgentCodexCLI),
	}}
	coordinator := NewAdapterCoordinator("node-a", &recordingLAN{}, registry)
	targets, err := coordinator.ListTargets(context.Background())
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "node-a" {
		t.Fatalf("local node must be published, got %+v", targets)
	}
	if targets[0].Status != "ready" || len(targets[0].Sessions) != 1 {
		t.Fatalf("local target must carry its endpoints, got %+v", targets[0])
	}
}

func TestListTargetsReplacesDiscoveredLocalNode(t *testing.T) {
	registry := &stubRegistry{endpoints: []agentbridge.Endpoint{
		endpointFixture("node-a", "cli-1", agentbridge.AgentCodexCLI),
	}}
	coordinator := NewAdapterCoordinator("node-a", &selfLAN{}, registry)
	targets, err := coordinator.ListTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "node-a" || len(targets[0].Sessions) != 1 || targets[0].Sessions[0].ID != "cli-1" {
		t.Fatalf("discovered local node must be replaced by the registry view once, got %+v", targets)
	}
}

func TestListTargetsDegradesWhenLocalListingFails(t *testing.T) {
	registry := &stubRegistry{listErr: errors.New("host down")}
	coordinator := NewAdapterCoordinator("node-a", &recordingLAN{}, registry)
	targets, err := coordinator.ListTargets(context.Background())
	if err != nil {
		t.Fatalf("list targets must not fail because of one adapter: %v", err)
	}
	if targets[0].Status != "degraded" {
		t.Fatalf("local listing failure must be visible, got %+v", targets[0])
	}
}

type recordingLAN struct{ sent []lannode.Message }

func (lan *recordingLAN) Peers() []lannode.Peer { return nil }
func (lan *recordingLAN) Peer(string) (lannode.Peer, bool) {
	return lannode.Peer{ID: "node-b", Address: "127.0.0.1:1"}, true
}
func (lan *recordingLAN) RefreshPeer(context.Context, string, string) (lannode.Peer, error) {
	return lannode.Peer{}, errors.New("unreachable")
}
func (lan *recordingLAN) ListSessions(context.Context, lannode.Peer) ([]lannode.Session, error) {
	return nil, errors.New("unreachable")
}
func (lan *recordingLAN) SendMessage(_ context.Context, _ lannode.Peer, message lannode.Message) error {
	lan.sent = append(lan.sent, message)
	return nil
}

type failingLAN struct{ recordingLAN }

type selfLAN struct{ recordingLAN }

func (lan *selfLAN) Peers() []lannode.Peer {
	return []lannode.Peer{{ID: "node-a", Name: "node-a", Address: "127.0.0.1:1"}}
}

func (lan *selfLAN) ListSessions(context.Context, lannode.Peer) ([]lannode.Session, error) {
	return []lannode.Session{{ID: "stale-local", Title: "stale"}}, nil
}

func (lan failingLAN) SendMessage(context.Context, lannode.Peer, lannode.Message) error {
	return errors.New("local delivery must not use LAN")
}
