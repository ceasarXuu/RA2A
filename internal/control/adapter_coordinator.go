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
	"github.com/ceasarXuu/RA2A/internal/mailbox"
)

var ErrStartRequired = errors.New("START_REQUIRED")
var ErrCallerUnknown = errors.New("CALLER_SESSION_UNKNOWN")

// LocalRegistry is the daemon-side view of the adapters that own endpoints on
// this node. The coordinator uses it to answer local deliveries and to describe
// the local node in target listings; it never inspects a concrete host type.
type LocalRegistry interface {
	Endpoints(context.Context) ([]agentbridge.Endpoint, error)
	Lookup(context.Context, agentbridge.Address) (agentbridge.Endpoint, agentbridge.Adapter, error)
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
	mailbox  *mailbox.Store
	mu       sync.RWMutex
	cache    map[string][]lannode.Session
}

func NewAdapterCoordinator(localID string, lan LAN, registry LocalRegistry) *AdapterCoordinator {
	return &AdapterCoordinator{localID: localID, lan: lan, registry: registry, cache: make(map[string][]lannode.Session)}
}

// WithMailbox attaches a mailbox store so local deliveries can be addressed to
// a mailbox instead of an agent endpoint.
func (coordinator *AdapterCoordinator) WithMailbox(store *mailbox.Store) *AdapterCoordinator {
	coordinator.mailbox = store
	return coordinator
}

// LocalNodeID exposes this node's identity for the control plane.
func (coordinator *AdapterCoordinator) LocalNodeID() string { return coordinator.localID }

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
	if request.Text == "" {
		return ErrInvalidRequest
	}
	nodeID, _, err := parseTarget(request.To)
	if err != nil {
		return ErrInvalidRequest
	}
	// The caller is resolved before the local/remote split so that a caller
	// identified by an opaque address is honoured on the LAN path too, instead
	// of being silently downgraded to the legacy session-id field.
	// Attribution is best effort. A sender that cannot be identified still gets
	// its message delivered: refusing to deliver because the caller is unknown
	// buys no security (any harness that can reach this endpoint can already
	// claim any address) and it turns an attribution gap into a delivery outage.
	// It also cannot be made reliable by configuration: a process-level hint goes
	// stale the moment the user starts a new session inside the same TUI, whereas
	// a reply address travels inside the message and is therefore never stale.
	sourceAddress := ""
	sourceEndpoint := request.SourceSessionID
	if coordinator.registry == nil {
		sourceAddress = "ra2a://" + coordinator.localID + "/anonymous"
	}
	if coordinator.registry != nil {
		if address, err := coordinator.registry.ResolveCaller(ctx, agentbridge.CallerContext{
			DeclaredAddress: request.From, Meta: request.Meta,
		}); err == nil && address.Valid() && address.NodeID == coordinator.localID {
			sourceAddress = address.String()
			sourceEndpoint = address.EndpointID
		}
	}
	// A declared or legacy source is used only when this node publishes it, so
	// the recipient is never shown a reply address that cannot exist.
	if sourceAddress == "" && sourceEndpoint != "" {
		published := false
		if _, _, err := coordinator.registry.Lookup(ctx,
			agentbridge.Address{NodeID: coordinator.localID, EndpointID: sourceEndpoint}); err == nil {
			published = true
		}
		if published {
			sourceAddress = "ra2a://" + coordinator.localID + "/" + sourceEndpoint
		}
	}
	if sourceAddress == "" {
		sourceAddress = "ra2a://" + coordinator.localID + "/anonymous"
	}
	if nodeID != coordinator.localID || coordinator.registry == nil {
		forwarded := request
		forwarded.From = sourceAddress
		forwarded.SourceSessionID = sourceEndpoint
		return NewCoordinator(coordinator.localID, coordinator.lan).Send(ctx, forwarded)
	}
	// A mailbox on this node is stored here and never reaches an adapter.
	if _, recipient, isMailbox, boxErr := mailbox.ParseAddress(request.To); isMailbox {
		if boxErr != nil {
			return fmt.Errorf("TARGET_UNSUPPORTED: %v", boxErr)
		}
		return resultError(coordinator.storeMailbox(recipient, sourceAddress, request))
	}
	envelope := agentbridge.MessageEnvelope{
		ID:              request.MessageID,
		ProtocolVersion: agentbridge.ProtocolVersion,
		SourceAddress:   sourceAddress,
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

// recipientAddress builds the local opaque address for a mailbox recipient.
func recipientAddress(nodeID, recipient string) string {
	return mailbox.Address(nodeID, recipient)
}

// storeMailbox writes one envelope into a local mailbox.
func (coordinator *AdapterCoordinator) storeMailbox(recipient, source string, request SendRequest) agentbridge.DeliveryResult {
	envelope := agentbridge.MessageEnvelope{
		ID:              request.MessageID,
		ProtocolVersion: agentbridge.ProtocolVersion,
		SourceAddress:   source,
		TargetAddress:   recipientAddress(coordinator.localID, recipient),
		Text:            request.Text,
		CreatedAt:       time.Now().UTC(),
	}
	result, _ := DeliverMailbox(coordinator.mailbox, coordinator.localID, envelope)
	return result
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
