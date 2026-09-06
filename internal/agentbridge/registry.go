package agentbridge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Registry aggregates adapters into a deterministic endpoint address space.
// It does not interpret host types: selection happens by opaque address only,
// mirroring the LAN-facing contract that callers never inspect addresses.
type Registry struct {
	mu       sync.Mutex
	closed   bool
	adapters []AgentAdapter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register appends an adapter. Registering again with the same agent type
// replaces the previous adapter for that type (upgrade semantics).
func (registry *Registry) Register(adapter AgentAdapter) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.closed {
		return ErrClosed
	}
	description := adapter.Descriptor()
	for index, existing := range registry.adapters {
		if existing.Descriptor().AgentType == description.AgentType {
			registry.adapters[index] = adapter
			return nil
		}
	}
	registry.adapters = append(registry.adapters, adapter)
	return nil
}

// EndpointRef pairs an endpoint with the adapter that owns it.
type EndpointRef struct {
	Adapter  AgentAdapter
	Endpoint Endpoint
}

// ListEndpoints returns every endpoint of every registered adapter with
// stable ordering (agent type, then endpoint ID). Conflicts are surfaced in
// order; duplicates within one adapter are kept as the adapter reports them.
func (registry *Registry) ListEndpoints(ctx context.Context) ([]EndpointRef, error) {
	registry.mu.Lock()
	closed := registry.closed
	adapters := append([]AgentAdapter(nil), registry.adapters...)
	registry.mu.Unlock()
	if closed {
		return nil, ErrClosed
	}
	var refs []EndpointRef
	for _, adapter := range adapters {
		endpoints, err := adapter.ListEndpoints(ctx)
		if err != nil {
			return nil, fmt.Errorf("agentbridge: %s: %w", adapter.Descriptor().AgentType, err)
		}
		for _, endpoint := range endpoints {
			refs = append(refs, EndpointRef{Adapter: adapter, Endpoint: endpoint})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Endpoint.AgentType != refs[j].Endpoint.AgentType {
			return refs[i].Endpoint.AgentType < refs[j].Endpoint.AgentType
		}
		return refs[i].Endpoint.EndpointID < refs[j].Endpoint.EndpointID
	})
	return refs, nil
}

// Conflicts reports endpoint IDs claimed by more than one adapter — a
// deterministic check callers can run before publishing an endpoint list.
func (registry *Registry) Conflicts(ctx context.Context) ([]string, error) {
	refs, err := registry.ListEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	owners := make(map[string]AgentType)
	var conflicts []string
	for _, ref := range refs {
		if previous, exists := owners[ref.Endpoint.EndpointID]; exists {
			if previous != ref.Endpoint.AgentType {
				conflicts = append(conflicts, ref.Endpoint.EndpointID)
			}
			continue
		}
		owners[ref.Endpoint.EndpointID] = ref.Endpoint.AgentType
	}
	return conflicts, nil
}

// Resolve finds the adapter and endpoint for an opaque target address.
// Addresses are compared as full strings; nothing inspects their format.
func (registry *Registry) Resolve(ctx context.Context, address string) (EndpointRef, bool, error) {
	refs, err := registry.ListEndpoints(ctx)
	if err != nil {
		return EndpointRef{}, false, err
	}
	for _, ref := range refs {
		if ref.Endpoint.Address == address {
			return ref, true, nil
		}
	}
	return EndpointRef{}, false, nil
}

// Deliver routes one text message to the endpoint identified by the opaque
// address. It never inspects agent types; adapters own their native behavior.
func (registry *Registry) Deliver(ctx context.Context, address, text string) (DeliveryResult, error) {
	ref, found, err := registry.Resolve(ctx, address)
	if err != nil {
		return DeliveryResult{}, err
	}
	if !found {
		return DeliveryResult{Code: NotFound, Detail: "no endpoint for address"}, nil
	}
	return ref.Adapter.Deliver(ctx, ref.Endpoint, text)
}

// Close closes every adapter and makes the registry unusable.
func (registry *Registry) Close() error {
	registry.mu.Lock()
	if registry.closed {
		registry.mu.Unlock()
		return nil
	}
	registry.closed = true
	adapters := append([]AgentAdapter(nil), registry.adapters...)
	registry.mu.Unlock()
	var closeErr error
	for _, adapter := range adapters {
		if err := adapter.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}
