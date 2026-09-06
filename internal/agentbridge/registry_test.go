package agentbridge

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type fakeAdapter struct {
	mu           sync.Mutex
	agentType    AgentType
	endpoints    []Endpoint
	deliverErr   error
	deliverCodes []string // consumed per call, in order
	closeErr     error
	closed       bool
	listErr      error
}

func newFake(agentType AgentType, endpoints []Endpoint) *fakeAdapter {
	return &fakeAdapter{agentType: agentType, endpoints: endpoints}
}

func (fake *fakeAdapter) Descriptor() AdapterDescriptor {
	return AdapterDescriptor{AgentType: fake.agentType, DisplayName: string(fake.agentType)}
}

func (fake *fakeAdapter) ListEndpoints(context.Context) ([]Endpoint, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.listErr != nil {
		return nil, fake.listErr
	}
	return append([]Endpoint(nil), fake.endpoints...), nil
}

func (fake *fakeAdapter) Deliver(_ context.Context, endpoint Endpoint, text string) (DeliveryResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.deliverErr != nil {
		return DeliveryResult{}, fake.deliverErr
	}
	if len(fake.deliverCodes) > 0 {
		code := fake.deliverCodes[0]
		fake.deliverCodes = fake.deliverCodes[1:]
		return DeliveryResult{Code: code}, nil
	}
	_ = text
	return DeliveryResult{Code: Delivered, Detail: endpoint.EndpointID}, nil
}

func (fake *fakeAdapter) Health(context.Context) error { return nil }

func (fake *fakeAdapter) Close() error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.closed = true
	return fake.closeErr
}

func endpoint(id string, agentType AgentType) Endpoint {
	return Endpoint{
		EndpointID: id, AgentType: agentType, NativeSessionID: id,
		Title: "t-" + id, Status: StatusReady,
		Capabilities: map[Capability]bool{CapabilityReceiveText: true},
		Address:      "addr:" + string(agentType) + ":" + id,
	}
}

func TestRegistryListsEndpointsAcrossAdaptersDeterministically(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()
	app := newFake("codex-app", []Endpoint{endpoint("b", "codex-app"), endpoint("a", "codex-app")})
	cli := newFake("codex-cli", []Endpoint{endpoint("x", "codex-cli")})
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(cli); err != nil {
		t.Fatal(err)
	}
	refs, err := registry.ListEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 {
		t.Fatalf("refs = %d, want 3", len(refs))
	}
	for i, want := range []string{"a", "b", "x"} {
		if refs[i].Endpoint.EndpointID != want {
			t.Fatalf("refs[%d] = %q, want %q", i, refs[i].Endpoint.EndpointID, want)
		}
	}
	if refs[0].Endpoint.AgentType != "codex-app" || refs[2].Endpoint.AgentType != "codex-cli" {
		t.Fatalf("agent types = %q, %q", refs[0].Endpoint.AgentType, refs[2].Endpoint.AgentType)
	}
}

func TestRegistryRegisterReplacesSameAgentType(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()
	first := newFake("codex-app", []Endpoint{endpoint("a", "codex-app")})
	second := newFake("codex-app", []Endpoint{endpoint("b", "codex-app")})
	if err := registry.Register(first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(second); err != nil {
		t.Fatal(err)
	}
	refs, err := registry.ListEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Endpoint.EndpointID != "b" {
		t.Fatalf("refs = %#v, want only endpoint b from the replacement adapter", refs)
	}
}

func TestRegistryReportsCrossAdapterConflicts(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()
	app := newFake("codex-app", []Endpoint{endpoint("shared", "codex-app")})
	cli := newFake("codex-cli", []Endpoint{endpoint("shared", "codex-cli")})
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(cli); err != nil {
		t.Fatal(err)
	}
	conflicts, err := registry.Conflicts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || conflicts[0] != "shared" {
		t.Fatalf("conflicts = %#v, want [shared]", conflicts)
	}
}

func TestRegistryNoConflictWhenSameAdapterReusesID(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()
	app := newFake("codex-app", []Endpoint{endpoint("a", "codex-app"), endpoint("a", "codex-app")})
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	conflicts, err := registry.Conflicts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %#v, want none inside one adapter", conflicts)
	}
}

func TestRegistryResolvesAndDeliversByOpaqueAddress(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()
	app := newFake("codex-app", []Endpoint{endpoint("a", "codex-app")})
	if err := registry.Register(app); err != nil {
		t.Fatal(err)
	}
	ref, found, err := registry.Resolve(ctx, "addr:codex-app:a")
	if err != nil || !found {
		t.Fatalf("resolve: found=%v err=%v", found, err)
	}
	if ref.Adapter != AgentAdapter(app) {
		t.Fatal("resolved to wrong adapter")
	}

	result, err := registry.Deliver(ctx, "addr:codex-app:a", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != Delivered || result.Detail != "a" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRegistryDeliverUnknownAddress(t *testing.T) {
	registry := NewRegistry()
	result, err := registry.Deliver(context.Background(), "addr:missing:1", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != NotFound {
		t.Fatalf("code = %q, want not_found", result.Code)
	}
}

func TestRegistryMapsAdapterNativeCodesToDeliveryResults(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()
	cli := newFake("codex-cli", []Endpoint{endpoint("c", "codex-cli")})
	cli.deliverCodes = []string{Busy, Unsupported, StartRequired, DeliveryUnknown}
	if err := registry.Register(cli); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{Busy, Unsupported, StartRequired, DeliveryUnknown} {
		result, err := registry.Deliver(ctx, "addr:codex-cli:c", "msg")
		if err != nil {
			t.Fatal(err)
		}
		if result.Code != want {
			t.Fatalf("code = %q, want %q", result.Code, want)
		}
	}
}

func TestRegistryPropagatesAdapterListError(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()
	broken := newFake("codex-cli", nil)
	broken.listErr = errors.New("host down")
	if err := registry.Register(broken); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ListEndpoints(ctx); err == nil {
		t.Fatal("expected list error propagation")
	}
}

func TestRegistryCloseClosesAdaptersAndGuardsUsage(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()
	adapter := newFake("codex-app", []Endpoint{endpoint("a", "codex-app")})
	if err := registry.Register(adapter); err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if !adapter.closed {
		t.Fatal("adapter was not closed")
	}
	if err := registry.Register(newFake("codex-cli", nil)); !errors.Is(err, ErrClosed) {
		t.Fatalf("register after close = %v, want ErrClosed", err)
	}
	if _, err := registry.ListEndpoints(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("list after close = %v, want ErrClosed", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("double close: %v", err)
	}
}

func TestRegistryCloseJoinsAdapterErrors(t *testing.T) {
	registry := NewRegistry()
	bad := newFake("codex-cli", nil)
	bad.closeErr = errors.New("cleanup failed")
	if err := registry.Register(bad); err != nil {
		t.Fatal(err)
	}
	err := registry.Close()
	if err == nil || !errors.Is(err, bad.closeErr) {
		t.Fatalf("close error = %v, want adapter close error joined", err)
	}
}
