package agentbridge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeAdapter struct {
	kind       AgentKind
	endpoints  []Endpoint
	health     Health
	deliveries []Address
	result     DeliveryResult
	listErr    error
}

func (adapter *fakeAdapter) Kind() AgentKind { return adapter.kind }

func (adapter *fakeAdapter) ListEndpoints(context.Context) ([]Endpoint, error) {
	if adapter.listErr != nil {
		return nil, adapter.listErr
	}
	return append([]Endpoint(nil), adapter.endpoints...), nil
}

func (adapter *fakeAdapter) Deliver(_ context.Context, address Address, _ MessageEnvelope) DeliveryResult {
	adapter.deliveries = append(adapter.deliveries, address)
	return adapter.result
}

func (adapter *fakeAdapter) Health(context.Context) Health { return adapter.health }

func (adapter *fakeAdapter) Close() error { return nil }

func readyEndpoint(nodeID, id string, kind AgentKind, capabilities ...Capability) Endpoint {
	return Endpoint{
		ID: id, Agent: kind, NativeSessionID: "native-" + id, Title: id,
		Status: EndpointReady, Capabilities: capabilities,
		Address: Address{NodeID: nodeID, EndpointID: id},
	}
}

func envelope(target, text string) MessageEnvelope {
	return MessageEnvelope{
		ID: "msg-1", ProtocolVersion: ProtocolVersion,
		TargetAddress: target, Text: text,
	}
}

func TestRegisterRejectsDuplicateKind(t *testing.T) {
	registry := NewRegistry("node-a")
	if err := registry.Register(&fakeAdapter{kind: AgentCodexApp, health: Ready()}); err != nil {
		t.Fatalf("register first adapter: %v", err)
	}
	err := registry.Register(&fakeAdapter{kind: AgentCodexApp, health: Ready()})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate kind must be rejected, got %v", err)
	}
	if err := registry.Register(&fakeAdapter{kind: AgentKind("codex-unknown")}); err == nil {
		t.Fatal("unknown agent kind must be rejected")
	}
}

func TestEndpointsAggregatesAcrossAdaptersWithoutHostBranches(t *testing.T) {
	registry := NewRegistry("node-a")
	app := &fakeAdapter{kind: AgentCodexApp, health: Ready(), endpoints: []Endpoint{
		readyEndpoint("node-a", "app-1", AgentCodexApp, CapabilityReceiveText),
	}}
	cli := &fakeAdapter{kind: AgentCodexCLI, health: Ready(), endpoints: []Endpoint{
		readyEndpoint("node-a", "cli-1", AgentCodexCLI, CapabilityReceiveText, CapabilitySteerActiveTurn),
	}}
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(cli); err != nil {
		t.Fatal(err)
	}
	endpoints, problems := registry.Endpoints(context.Background())
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if len(endpoints) != 2 || endpoints[0].ID != "app-1" || endpoints[1].ID != "cli-1" {
		t.Fatalf("unexpected endpoint table: %+v", endpoints)
	}
	if endpoints[0].Address.String() != "ra2a://node-a/app-1" {
		t.Fatalf("unexpected address: %s", endpoints[0].Address.String())
	}
}

func TestEndpointsDropsDuplicateIDAcrossAdapters(t *testing.T) {
	registry := NewRegistry("node-a")
	first := &fakeAdapter{kind: AgentCodexApp, health: Ready(), endpoints: []Endpoint{
		readyEndpoint("node-a", "shared", AgentCodexApp, CapabilityReceiveText),
	}}
	second := &fakeAdapter{kind: AgentCodexCLI, health: Ready(), endpoints: []Endpoint{
		readyEndpoint("node-a", "shared", AgentCodexCLI, CapabilityReceiveText),
	}}
	if err := registry.Register(first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(second); err != nil {
		t.Fatal(err)
	}
	endpoints, problems := registry.Endpoints(context.Background())
	if len(endpoints) != 1 {
		t.Fatalf("conflicting endpoint must be dropped, got %+v", endpoints)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "claimed by both") {
		t.Fatalf("conflict must be reported, got %v", problems)
	}
}

func TestEndpointsRejectsUnknownAndMalformed(t *testing.T) {
	registry := NewRegistry("node-a")
	unknown := readyEndpoint("node-a", "unknown-1", AgentCodexApp, CapabilityReceiveText)
	unknown.Status = EndpointUnknown
	noCapabilities := readyEndpoint("node-a", "bare-1", AgentCodexApp)
	mismatched := readyEndpoint("node-a", "mismatch-1", AgentCodexApp, CapabilityReceiveText)
	mismatched.Address = Address{NodeID: "node-a", EndpointID: "other"}
	adapter := &fakeAdapter{kind: AgentCodexApp, health: Ready(), endpoints: []Endpoint{unknown, noCapabilities, mismatched}}
	if err := registry.Register(adapter); err != nil {
		t.Fatal(err)
	}
	endpoints, problems := registry.Endpoints(context.Background())
	if len(endpoints) != 0 {
		t.Fatalf("invalid endpoints must never be published: %+v", endpoints)
	}
	if len(problems) != 3 {
		t.Fatalf("expected one problem per rejected endpoint, got %v", problems)
	}
}

func TestBusyEndpointRemainsAddressableForSteering(t *testing.T) {
	registry := NewRegistry("node-a")
	busy := readyEndpoint("node-a", "busy-1", AgentCodexApp, CapabilityReceiveText, CapabilitySteerActiveTurn)
	busy.Status = EndpointBusy
	adapter := &fakeAdapter{kind: AgentCodexApp, health: Ready(), result: Delivered("turn-1"), endpoints: []Endpoint{busy}}
	if err := registry.Register(adapter); err != nil {
		t.Fatal(err)
	}
	endpoints, problems := registry.Endpoints(context.Background())
	if len(problems) != 0 || len(endpoints) != 1 || endpoints[0].Status != EndpointBusy {
		t.Fatalf("busy endpoint must remain published, got %+v, problems %v", endpoints, problems)
	}
	result := registry.Deliver(context.Background(), envelope("ra2a://node-a/busy-1", "follow-up"))
	if !result.Delivered() || len(adapter.deliveries) != 1 {
		t.Fatalf("busy endpoint must reach its adapter, got %+v, deliveries %+v", result, adapter.deliveries)
	}
}

func TestEndpointsPropagatesListFailureWithoutDroppingOthers(t *testing.T) {
	registry := NewRegistry("node-a")
	broken := &fakeAdapter{kind: AgentCodexApp, listErr: errors.New("host down")}
	working := &fakeAdapter{kind: AgentCodexCLI, health: Ready(), endpoints: []Endpoint{
		readyEndpoint("node-a", "cli-1", AgentCodexCLI, CapabilityReceiveText),
	}}
	if err := registry.Register(broken); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(working); err != nil {
		t.Fatal(err)
	}
	endpoints, problems := registry.Endpoints(context.Background())
	if len(endpoints) != 1 || len(problems) != 1 {
		t.Fatalf("partial failure must be reported per adapter: %+v %v", endpoints, problems)
	}
}

func TestDeliverRoutesToOwningAdapterAndGatesCapabilities(t *testing.T) {
	registry := NewRegistry("node-a")
	app := &fakeAdapter{kind: AgentCodexApp, health: Ready(), result: Delivered("turn-app"), endpoints: []Endpoint{
		readyEndpoint("node-a", "app-1", AgentCodexApp, CapabilityReceiveText),
	}}
	cli := &fakeAdapter{kind: AgentCodexCLI, health: Ready(), result: Delivered("turn-cli"), endpoints: []Endpoint{
		readyEndpoint("node-a", "cli-1", AgentCodexCLI, CapabilityReceiveText),
		readyEndpoint("node-a", "cli-2", AgentCodexCLI, CapabilityInteractiveSafe),
	}}
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(cli); err != nil {
		t.Fatal(err)
	}

	result := registry.Deliver(context.Background(), envelope("ra2a://node-a/cli-1", "hello"))
	if !result.Delivered() || result.TurnID != "turn-cli" {
		t.Fatalf("delivery must reach the owning adapter, got %+v", result)
	}
	if len(app.deliveries) != 0 || len(cli.deliveries) != 1 {
		t.Fatalf("only the owning adapter may be called: app=%v cli=%v", app.deliveries, cli.deliveries)
	}

	unsupported := registry.Deliver(context.Background(), envelope("ra2a://node-a/cli-2", "hello"))
	if unsupported.Code != ResultUnsupported {
		t.Fatalf("endpoint without receiveText must be unsupported, got %+v", unsupported)
	}
	if len(cli.deliveries) != 1 {
		t.Fatalf("rejected delivery must not reach the adapter: %v", cli.deliveries)
	}
}

func TestDeliverMapsHealthAndRoutingFailures(t *testing.T) {
	registry := NewRegistry("node-a")
	offline := &fakeAdapter{kind: AgentCodexCLI, health: Unhealthy(ResultStartRequired, "codex is not running"), endpoints: []Endpoint{
		readyEndpoint("node-a", "cli-1", AgentCodexCLI, CapabilityReceiveText),
	}}
	if err := registry.Register(offline); err != nil {
		t.Fatal(err)
	}
	if result := registry.Deliver(context.Background(), envelope("ra2a://node-a/cli-1", "hi")); result.Code != ResultStartRequired {
		t.Fatalf("unhealthy adapter must surface start_required, got %+v", result)
	}
	if result := registry.Deliver(context.Background(), envelope("ra2a://node-a/missing", "hi")); result.Code != ResultNotFound {
		t.Fatalf("unknown endpoint must be not_found, got %+v", result)
	}
	if result := registry.Deliver(context.Background(), envelope("ra2a://node-b/cli-1", "hi")); result.Code != ResultNotFound {
		t.Fatalf("foreign node must not be routable, got %+v", result)
	}
	if result := registry.Deliver(context.Background(), envelope("not-an-address", "hi")); result.Code != ResultUnsupported {
		t.Fatalf("malformed target must be unsupported, got %+v", result)
	}
	empty := envelope("ra2a://node-a/cli-1", "")
	if result := registry.Deliver(context.Background(), empty); result.Code != ResultUnsupported {
		t.Fatalf("empty text must be unsupported, got %+v", result)
	}
	oldVersion := envelope("ra2a://node-a/cli-1", "hi")
	oldVersion.ProtocolVersion = ProtocolVersion + 1
	if result := registry.Deliver(context.Background(), oldVersion); result.Code != ResultUnsupported {
		t.Fatalf("unknown protocol version must be unsupported, got %+v", result)
	}
}

func TestHealthRejectsInvalidUnhealthyReport(t *testing.T) {
	registry := NewRegistry("node-a")
	if err := registry.Register(&fakeAdapter{kind: AgentCodexApp}); err != nil {
		t.Fatal(err)
	}
	report := registry.Health(context.Background())
	health := report[AgentCodexApp]
	if health.Ready {
		t.Fatal("zero-value health must not read as ready")
	}
	if health.Code != ResultUnknown {
		t.Fatalf("invalid unhealthy report must degrade to unknown, got %+v", health)
	}
}

func TestParseAddressRoundTrip(t *testing.T) {
	address, err := ParseAddress("ra2a://node-a/cli-1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if address.String() != "ra2a://node-a/cli-1" {
		t.Fatalf("round trip failed: %s", address.String())
	}
	for _, invalid := range []string{"", "ra2a://", "ra2a://node", "ra2a://node/", "ra2a://node/a/b", "http://node/a"} {
		if _, err := ParseAddress(invalid); err == nil {
			t.Fatalf("address %q must be rejected", invalid)
		}
	}
}

func TestCloseClosesEveryAdapterOnce(t *testing.T) {
	registry := NewRegistry("node-a")
	first := &countingAdapter{kind: AgentCodexApp}
	second := &countingAdapter{kind: AgentCodexCLI}
	if err := registry.Register(first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(second); err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if first.closed != 1 || second.closed != 1 {
		t.Fatalf("each adapter must close exactly once: %d %d", first.closed, second.closed)
	}
	if len(registry.Adapters()) != 0 {
		t.Fatal("registry must be empty after close")
	}
}

type countingAdapter struct {
	kind   AgentKind
	closed int
}

func (adapter *countingAdapter) Kind() AgentKind                                   { return adapter.kind }
func (adapter *countingAdapter) ListEndpoints(context.Context) ([]Endpoint, error) { return nil, nil }
func (adapter *countingAdapter) Deliver(context.Context, Address, MessageEnvelope) DeliveryResult {
	return DeliveryResult{Code: ResultUnknown}
}
func (adapter *countingAdapter) Health(context.Context) Health { return Ready() }
func (adapter *countingAdapter) Close() error                  { adapter.closed++; return nil }

type shapeAwareAdapter struct {
	fakeAdapter
	reject map[string]bool
}

func (adapter *shapeAwareAdapter) CheckTargetID(endpointID string) error {
	if adapter.reject[endpointID] {
		return errors.New("not a valid id for this host")
	}
	return nil
}

func TestDeliverDistinguishesMalformedFromUnpublishedTarget(t *testing.T) {
	registry := NewRegistry("node-a")
	cli := &shapeAwareAdapter{
		fakeAdapter: fakeAdapter{kind: AgentCodexCLI, health: Ready()},
		reject:      map[string]bool{"not-a-thread-id": true},
	}
	if err := registry.Register(cli); err != nil {
		t.Fatal(err)
	}
	malformed := registry.Deliver(context.Background(), envelope("ra2a://node-a/not-a-thread-id", "hi"))
	if malformed.Code != ResultUnsupported {
		t.Fatalf("malformed id for a known host must be unsupported, got %+v", malformed)
	}
	if malformed.NativeErrorClass != "malformed_target_id" {
		t.Fatalf("native class must identify the shape error, got %q", malformed.NativeErrorClass)
	}
	unpublished := registry.Deliver(context.Background(), envelope("ra2a://node-a/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "hi"))
	if unpublished.Code != ResultNotFound {
		t.Fatalf("well formed but unpublished id must be not_found, got %+v", unpublished)
	}
}

type callerAwareAdapter struct {
	fakeAdapter
	address Address
	err     error
	seen    []CallerContext
}

func (adapter *callerAwareAdapter) ResolveCaller(_ context.Context, caller CallerContext) (Address, error) {
	adapter.seen = append(adapter.seen, caller)
	return adapter.address, adapter.err
}

func TestResolveCallerPrefersDeclaredAddress(t *testing.T) {
	registry := NewRegistry("node-a")
	app := &callerAwareAdapter{
		fakeAdapter: fakeAdapter{kind: AgentCodexApp, health: Ready()},
		address:     Address{NodeID: "node-a", EndpointID: "app-1"},
	}
	app.endpoints = []Endpoint{readyEndpoint("node-a", "app-1", AgentCodexApp, CapabilityReceiveText)}
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	address, err := registry.ResolveCaller(context.Background(), CallerContext{
		DeclaredAddress: "ra2a://node-a/app-1",
		Meta:            map[string]any{"threadId": "app-1"},
	})
	if err != nil || address.EndpointID != "app-1" {
		t.Fatalf("declared address must resolve, got %+v %v", address, err)
	}
	if len(app.seen) != 0 {
		t.Fatalf("a declared address must not be second-guessed by adapters, got %+v", app.seen)
	}
}

func TestResolveCallerRejectsUntrustedDeclaredAddress(t *testing.T) {
	registry := NewRegistry("node-a")
	app := &callerAwareAdapter{
		fakeAdapter: fakeAdapter{kind: AgentCodexApp, health: Ready()},
		address:     Address{NodeID: "node-a", EndpointID: "app-1"},
	}
	app.endpoints = []Endpoint{readyEndpoint("node-a", "app-1", AgentCodexApp, CapabilityReceiveText)}
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	for _, declared := range []string{"ra2a://node-a/not-published", "ra2a://node-b/app-1", "garbage"} {
		if _, err := registry.ResolveCaller(context.Background(), CallerContext{DeclaredAddress: declared}); err == nil {
			t.Fatalf("declared address %q must be refused", declared)
		}
	}
}

func TestResolveCallerFallsBackToAdapters(t *testing.T) {
	registry := NewRegistry("node-a")
	app := &callerAwareAdapter{
		fakeAdapter: fakeAdapter{kind: AgentCodexApp, health: Ready()},
		address:     Address{NodeID: "node-a", EndpointID: "app-1"},
	}
	app.endpoints = []Endpoint{readyEndpoint("node-a", "app-1", AgentCodexApp, CapabilityReceiveText)}
	cli := &callerAwareAdapter{
		fakeAdapter: fakeAdapter{kind: AgentCodexCLI, health: Ready()},
		err:         CallerHint("no stable caller identity"),
	}
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(cli); err != nil {
		t.Fatal(err)
	}
	address, err := registry.ResolveCaller(context.Background(), CallerContext{
		Meta: map[string]any{"threadId": "app-1"},
	})
	if err != nil || address.EndpointID != "app-1" {
		t.Fatalf("adapter-resolved caller must be accepted, got %+v %v", address, err)
	}
	if len(app.seen) != 1 || app.seen[0].Meta["threadId"] != "app-1" {
		t.Fatalf("adapter must receive the raw metadata, got %+v", app.seen)
	}
}

func TestResolveCallerRefusesUnpublishedAdapterGuess(t *testing.T) {
	registry := NewRegistry("node-a")
	guesser := &callerAwareAdapter{
		fakeAdapter: fakeAdapter{kind: AgentCodexCLI, health: Ready()},
		address:     Address{NodeID: "node-a", EndpointID: "never-published"},
	}
	if err := registry.Register(guesser); err != nil {
		t.Fatal(err)
	}
	_, err := registry.ResolveCaller(context.Background(), CallerContext{})
	if err == nil || !strings.Contains(err.Error(), "from") {
		t.Fatalf("an unpublished guess must be refused with actionable guidance, got %v", err)
	}
}

func TestEveryRegisteredKindIsPartOfTheSharedContract(t *testing.T) {
	// A new adapter must declare its kind in agentbridge rather than inventing
	// one locally, otherwise the registry silently refuses it at runtime.
	for _, kind := range []AgentKind{AgentCodexApp, AgentCodexCLI, AgentOpenCode} {
		if !kind.Valid() {
			t.Fatalf("kind %q must be valid", kind)
		}
		registry := NewRegistry("node-a")
		if err := registry.Register(&fakeAdapter{kind: kind, health: Ready()}); err != nil {
			t.Fatalf("kind %q must be registrable: %v", kind, err)
		}
	}
	if AgentKind("some-future-agent").Valid() {
		t.Fatal("an unregistered kind must stay invalid")
	}
}
