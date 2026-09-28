package agentbridge

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Adapter is the only host-specific surface in RA2A. Adapters never reference
// each other; the registry is what turns a set of adapters into a routable
// endpoint table.
type Adapter interface {
	Kind() AgentKind
	ListEndpoints(context.Context) ([]Endpoint, error)
	Deliver(context.Context, Address, MessageEnvelope) DeliveryResult
	Health(context.Context) Health
	Close() error
}

// Registry owns the endpoint table and the deterministic address-to-adapter
// mapping. It contains no host type branching: routing decisions are made from
// Endpoint and ResultCode values only.
type Registry struct {
	nodeID   string
	mu       sync.RWMutex
	adapters []Adapter
	byKind   map[AgentKind]Adapter
	claimed  map[string]AgentKind
}

func NewRegistry(nodeID string) *Registry {
	return &Registry{
		nodeID:  nodeID,
		byKind:  make(map[AgentKind]Adapter),
		claimed: make(map[string]AgentKind),
	}
}

func (registry *Registry) NodeID() string { return registry.nodeID }

func (registry *Registry) Register(adapter Adapter) error {
	if adapter == nil {
		return fmt.Errorf("register adapter: adapter is nil")
	}
	kind := adapter.Kind()
	if !kind.Valid() {
		return fmt.Errorf("register adapter: unknown agent kind %q", kind)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.byKind[kind]; exists {
		return fmt.Errorf("register adapter: agent kind %q already registered", kind)
	}
	registry.adapters = append(registry.adapters, adapter)
	registry.byKind[kind] = adapter
	return nil
}

func (registry *Registry) Adapters() []Adapter {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return append([]Adapter(nil), registry.adapters...)
}

func (registry *Registry) Close() error {
	registry.mu.Lock()
	adapters := registry.adapters
	registry.adapters = nil
	registry.byKind = make(map[AgentKind]Adapter)
	registry.claimed = make(map[string]AgentKind)
	registry.mu.Unlock()
	var firstErr error
	for _, adapter := range adapters {
		if err := adapter.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Endpoints aggregates every adapter's endpoints into one addressable table.
// An endpoint that fails validation is dropped rather than published, because
// an unowned or ambiguous endpoint must never be advertised as ready.
func (registry *Registry) Endpoints(ctx context.Context) ([]Endpoint, []error) {
	adapters := registry.Adapters()
	collected := make([]Endpoint, 0, len(adapters))
	problems := make([]error, 0)
	seen := make(map[string]AgentKind)

	for _, adapter := range adapters {
		endpoints, err := adapter.ListEndpoints(ctx)
		if err != nil {
			problems = append(problems, fmt.Errorf("list %s endpoints: %w", adapter.Kind(), err))
			continue
		}
		for _, endpoint := range endpoints {
			endpoint.Agent = adapter.Kind()
			if endpoint.Address.NodeID == "" {
				endpoint.Address.NodeID = registry.nodeID
			}
			if err := endpoint.Validate(); err != nil {
				problems = append(problems, fmt.Errorf("%s endpoint rejected: %w", adapter.Kind(), err))
				continue
			}
			if owner, exists := seen[endpoint.ID]; exists {
				problems = append(problems, fmt.Errorf(
					"endpoint %s claimed by both %s and %s", endpoint.ID, owner, adapter.Kind()))
				continue
			}
			seen[endpoint.ID] = adapter.Kind()
			collected = append(collected, endpoint)
		}
	}
	sort.Slice(collected, func(i, j int) bool { return collected[i].ID < collected[j].ID })
	return collected, problems
}

// Lookup resolves an address to its endpoint and owning adapter. It is the only
// path from an opaque address to a delivery call.
func (registry *Registry) Lookup(ctx context.Context, address Address) (Endpoint, Adapter, error) {
	if !address.Valid() {
		return Endpoint{}, nil, fmt.Errorf("invalid target address")
	}
	if address.NodeID != registry.nodeID {
		return Endpoint{}, nil, fmt.Errorf("target node %q is not served by this daemon", address.NodeID)
	}
	endpoints, _ := registry.Endpoints(ctx)
	for _, endpoint := range endpoints {
		if endpoint.Address == address {
			registry.mu.RLock()
			adapter := registry.byKind[endpoint.Agent]
			registry.mu.RUnlock()
			if adapter == nil {
				return Endpoint{}, nil, fmt.Errorf("no adapter owns endpoint %s", endpoint.ID)
			}
			return endpoint, adapter, nil
		}
	}
	return Endpoint{}, nil, fmt.Errorf("endpoint %s not found", address.EndpointID)
}

// Deliver routes one envelope. Capability gating happens here so no adapter
// has to re-implement it, and unknown outcomes stay unknown instead of being
// downgraded into a retry.
func (registry *Registry) Deliver(ctx context.Context, envelope MessageEnvelope) DeliveryResult {
	if err := envelope.Validate(); err != nil {
		return DeliveryResult{Code: ResultUnsupported, Detail: err.Error()}
	}
	address, err := parseEnvelopeTarget(envelope.TargetAddress)
	if err != nil {
		return DeliveryResult{Code: ResultUnsupported, Detail: err.Error()}
	}
	endpoint, adapter, err := registry.Lookup(ctx, address)
	if err != nil {
		return DeliveryResult{Code: ResultNotFound, Detail: err.Error()}
	}
	if !endpoint.Has(CapabilityReceiveText) {
		return DeliveryResult{
			Code:   ResultUnsupported,
			Detail: fmt.Sprintf("endpoint %s cannot receive text", endpoint.ID),
		}
	}
	health := adapter.Health(ctx)
	if !health.Ready {
		code := health.Code
		if !code.Valid() || code == ResultDelivered {
			code = ResultUnknown
		}
		return DeliveryResult{Code: code, Detail: health.Detail}
	}
	return adapter.Deliver(ctx, address, envelope)
}

func (registry *Registry) Health(ctx context.Context) map[AgentKind]Health {
	report := make(map[AgentKind]Health, len(registry.Adapters()))
	for _, adapter := range registry.Adapters() {
		health := adapter.Health(ctx)
		if err := health.Validate(); err != nil {
			health = Unhealthy(ResultUnknown, err.Error())
		}
		report[adapter.Kind()] = health
	}
	return report
}
