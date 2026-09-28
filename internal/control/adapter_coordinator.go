package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/lannode"
)

var ErrStartRequired = errors.New("START_REQUIRED")
var ErrCallerUnknown = errors.New("CALLER_SESSION_UNKNOWN")

// LocalRegistry is the daemon-side view of the adapters that own endpoints on
// this node. The coordinator uses it to answer local deliveries and to describe
// the local node in target listings; it never inspects a concrete host type.
type LocalRegistry interface {
	Endpoints(context.Context) ([]agentbridge.Endpoint, error)
	Deliver(context.Context, agentbridge.MessageEnvelope) agentbridge.DeliveryResult
	Health(context.Context) map[agentbridge.AgentKind]agentbridge.Health
	ResolveCaller(context.Context, agentbridge.CallerContext) (agentbridge.Address, error)
}

type EndpointLister interface {
	Endpoints(context.Context) ([]agentbridge.Endpoint, error)
}

// AdapterCoordinator joins LAN peers with the local adapter registry so both
// local and remote targets are published through one listing shape.
type AdapterCoordinator struct {
	localID  string
	lan      LAN
	registry LocalRegistry
	mu       sync.RWMutex
	cache    map[string][]lannode.Session
}

func NewAdapterCoordinator(localID string, lan LAN, registry LocalRegistry) *AdapterCoordinator {
	return &AdapterCoordinator{localID: localID, lan: lan, registry: registry, cache: make(map[string][]lannode.Session)}
}

func (coordinator *AdapterCoordinator) ListTargets(ctx context.Context) ([]Target, error) {
	targets, err := NewCoordinator(coordinator.localID, coordinator.lan).ListTargets(ctx)
	if err != nil {
		return nil, err
	}
	if coordinator.registry == nil {
		return targets, nil
	}
	localSessions, localErr := coordinator.localSessions(ctx)
	local := Target{
		ID: coordinator.localID, Name: coordinator.localID,
		Status: localStatus(localErr), Sessions: localSessions,
	}
	replaced := false
	for index := range targets {
		if targets[index].ID == coordinator.localID {
			targets[index] = local
			replaced = true
			break
		}
	}
	if !replaced {
		targets = append(targets, local)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets, nil
}

func localStatus(err error) string {
	if err != nil {
		return "degraded"
	}
	return "ready"
}

func (coordinator *AdapterCoordinator) localSessions(ctx context.Context) ([]lannode.Session, error) {
	endpoints, err := coordinator.registry.Endpoints(ctx)
	if err != nil {
		coordinator.mu.Lock()
		cached, ok := coordinator.cache[coordinator.localID]
		coordinator.mu.Unlock()
		if ok {
			return cached, err
		}
		return []lannode.Session{}, err
	}
	sessions := make([]lannode.Session, 0, len(endpoints))
	for _, endpoint := range endpoints {
		capabilities := make([]string, 0, len(endpoint.Capabilities))
		for _, capability := range endpoint.Capabilities {
			capabilities = append(capabilities, string(capability))
		}
		sessions = append(sessions, lannode.Session{
			ID: endpoint.Address.EndpointID, Title: endpoint.Title,
			Status: string(endpoint.Status), Agent: string(endpoint.Agent),
			Capabilities: capabilities,
		})
	}
	coordinator.mu.Lock()
	coordinator.cache[coordinator.localID] = sessions
	coordinator.mu.Unlock()
	return sessions, nil
}

// Send delivers locally when the target belongs to this node, and otherwise
// keeps the established LAN path unchanged.
func (coordinator *AdapterCoordinator) Send(ctx context.Context, request SendRequest) error {
	nodeID, _, err := parseTarget(request.To)
	if err != nil || request.Text == "" {
		return ErrInvalidRequest
	}
	if nodeID != coordinator.localID || coordinator.registry == nil {
		return NewCoordinator(coordinator.localID, coordinator.lan).Send(ctx, request)
	}
	source, err := coordinator.resolveSource(ctx, request)
	if err != nil {
		return err
	}
	envelope := agentbridge.MessageEnvelope{
		ID:              request.MessageID,
		ProtocolVersion: agentbridge.ProtocolVersion,
		SourceAddress:   source,
		TargetAddress:   request.To,
		Text:            request.Text,
		CreatedAt:       time.Now().UTC(),
	}
	if envelope.ID == "" {
		generated, genErr := newMessageID()
		if genErr != nil {
			return genErr
		}
		envelope.ID = generated
	}
	result := coordinator.registry.Deliver(ctx, envelope)
	return resultError(result)
}

// resolveSource turns whatever the caller supplied into one published opaque
// address. Callers are never trusted: an identity that this node does not
// publish is refused rather than forwarded.
func (coordinator *AdapterCoordinator) resolveSource(ctx context.Context, request SendRequest) (string, error) {
	address, err := coordinator.registry.ResolveCaller(ctx, agentbridge.CallerContext{
		DeclaredAddress: request.From, Meta: request.Meta,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCallerUnknown, err)
	}
	return address.String(), nil
}

// DeliveryConfirmed reports whether a success response for this target means
// the adapter confirmed it. Local deliveries are confirmed before the control
// plane answers; cross-node sends are only handed to the LAN transport.
func (coordinator *AdapterCoordinator) DeliveryConfirmed(target string) bool {
	nodeID, _, err := parseTarget(target)
	return err == nil && nodeID == coordinator.localID
}

// resultError maps the unified result codes onto the control-plane error set so
// HTTP status and MCP error codes stay stable for existing callers.
func resultError(result agentbridge.DeliveryResult) error {
	switch result.Code {
	case agentbridge.ResultDelivered:
		return nil
	case agentbridge.ResultNotFound:
		return fmt.Errorf("%w: %s", ErrTargetNotFound, result.Detail)
	case agentbridge.ResultStartRequired:
		return fmt.Errorf("%w: %s", ErrStartRequired, result.Detail)
	case agentbridge.ResultUnreachable:
		return fmt.Errorf("%w: %s", ErrTargetUnreachable, result.Detail)
	case agentbridge.ResultBusy:
		return fmt.Errorf("SESSION_BUSY: %s", result.Detail)
	case agentbridge.ResultUnsupported:
		return fmt.Errorf("TARGET_UNSUPPORTED: %s", result.Detail)
	default:
		return fmt.Errorf("%w: %s", ErrDeliveryUnknown, result.Detail)
	}
}
