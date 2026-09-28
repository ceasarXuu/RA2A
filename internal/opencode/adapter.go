package opencode

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

// AgentKind is the agent identity this adapter publishes. OpenCode is an
// adapted agent: it holds real conversations, so it gets a first-class kind
// registered in the shared contract rather than being folded into the mailbox
// channel.
const AgentKind = agentbridge.AgentOpenCode

type Adapter struct {
	nodeID    string
	client    *Client
	baseURL   string
	logger    *slog.Logger
	mu        sync.RWMutex
	restrict  bool
	adopted   map[string]struct{}
	watcher   context.CancelFunc
	lastProbe time.Time
	reachable bool
}

func New(nodeID string, client *Client, stderr io.Writer) *Adapter {
	if stderr == nil {
		stderr = io.Discard
	}
	return &Adapter{
		nodeID: nodeID, client: client, baseURL: client.BaseURL(),
		logger:  slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		adopted: make(map[string]struct{}),
	}
}

func (adapter *Adapter) Kind() agentbridge.AgentKind { return AgentKind }

// Adopt restricts publication to specific sessions. It exists only for
// operators who want a smaller surface; the default publishes every session the
// shared server reports, because requiring per-session setup would make the
// mesh unusable.
func (adapter *Adapter) Adopt(sessionID string) error {
	if sessionID == "" {
		return errors.New("adopt opencode session: empty session id")
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.restrict = true
	adapter.adopted[sessionID] = struct{}{}
	return nil
}

// Unrestrict returns the adapter to publishing every session.
func (adapter *Adapter) Unrestrict() {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.restrict = false
	adapter.adopted = make(map[string]struct{})
}

// ResolveCaller reports this node's OpenCode sessions when one of them acts as
// a sender. OpenCode does not put a stable caller identity in MCP metadata, so
// when exactly one session exists the adapter can answer without guessing;
// otherwise it asks the caller to declare which session it is rather than
// attributing the message to the wrong conversation.
func (adapter *Adapter) ResolveCaller(_ context.Context, caller agentbridge.CallerContext) (agentbridge.Address, error) {
	sessions, err := adapter.client.ListSessions(context.Background())
	if err != nil || len(sessions) == 0 {
		return agentbridge.Address{}, agentbridge.CallerHint(
			"no OpenCode session is available on this node to act as the caller")
	}
	if len(sessions) == 1 {
		return agentbridge.Address{NodeID: adapter.nodeID, EndpointID: sessions[0].ID}, nil
	}
	return agentbridge.Address{}, agentbridge.CallerHint(
		"this node has %d OpenCode sessions; pass `from` with this session's address from list_targets",
		len(sessions))
}

// Reachable reports whether an OpenCode server answers at the URL. The adapter
// uses it to stay absent instead of unhealthy when OpenCode is simply not
// running, so the other adapters keep serving.
func Reachable(ctx context.Context, baseURL string) bool {
	client := NewClient(Config{BaseURL: baseURL})
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := client.ListSessions(ctx)
	return err == nil
}

func (adapter *Adapter) Adopted() []string {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()
	ids := make([]string, 0, len(adapter.adopted))
	for id := range adapter.adopted {
		ids = append(ids, id)
	}
	return ids
}

// Watch starts consuming the event stream. Busy/idle and turn completion come
// from OpenCode itself rather than from inference.
func (adapter *Adapter) Watch(ctx context.Context) {
	watchCtx, cancel := context.WithCancel(ctx)
	adapter.mu.Lock()
	if adapter.watcher != nil {
		adapter.watcher()
	}
	adapter.watcher = cancel
	adapter.mu.Unlock()
	go func() {
		for {
			if err := adapter.client.Watch(watchCtx); err != nil && watchCtx.Err() == nil {
				adapter.logger.Info("opencode_event_stream_dropped", "error", err.Error())
			}
			if watchCtx.Err() != nil {
				return
			}
			select {
			case <-watchCtx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}

// ListEndpoints publishes every session the shared server reports. OpenCode
// sessions are globally listed and reachable through the one shared server, so
// there is no ownership ambiguity to resolve and no reason to make the operator
// register anything.
func (adapter *Adapter) ListEndpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	sessions, err := adapter.client.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	adapter.mu.RLock()
	restrict, adopted := adapter.restrict, adapter.Adopted()
	adapter.mu.RUnlock()
	if restrict {
		allowed := make(map[string]bool, len(adopted))
		for _, sessionID := range adopted {
			allowed[sessionID] = true
		}
		filtered := sessions[:0]
		for _, session := range sessions {
			if allowed[session.ID] {
				filtered = append(filtered, session)
			}
		}
		sessions = filtered
	}
	endpoints := make([]agentbridge.Endpoint, 0, len(sessions))
	for _, session := range sessions {
		sessionID := session.ID
		status := agentbridge.EndpointReady
		if adapter.client.Busy(sessionID) {
			status = agentbridge.EndpointBusy
		}
		title := session.Title
		if title == "" {
			title = sessionID
		}
		endpoints = append(endpoints, agentbridge.Endpoint{
			ID: sessionID, Agent: AgentKind, NativeSessionID: sessionID,
			Title: title, Status: status,
			Capabilities: []agentbridge.Capability{
				agentbridge.CapabilityReceiveText, agentbridge.CapabilityReplyAddress,
			},
			Address: agentbridge.Address{NodeID: adapter.nodeID, EndpointID: sessionID},
		})
	}
	return endpoints, nil
}

func (adapter *Adapter) Deliver(ctx context.Context, address agentbridge.Address, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	sessionID := address.EndpointID
	adapter.mu.RLock()
	_, known := adapter.adopted[sessionID]
	adapter.mu.RUnlock()
	if !known {
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultNotFound, NativeErrorClass: "ownership_unknown",
			Detail: "opencode session is not adopted by this node",
		}
	}
	marker := agentbridge.RenderIncomingText(envelope)
	_, postErr := adapter.client.PostMessage(ctx, sessionID, marker)
	if postErr != nil && !IsOutcomeUnknown(postErr) {
		if IsUnreachable(postErr) {
			return agentbridge.DeliveryResult{
				Code: agentbridge.ResultUnreachable, NativeErrorClass: "server_unreachable", Detail: postErr.Error(),
			}
		}
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "post_failed", Detail: postErr.Error(),
		}
	}
	// The POST blocks for the whole turn, so a deadline here is ambiguous: the
	// message may already be in the session. Reconcile against history and the
	// idle signal before deciding, and never retry the post itself.
	confirmCtx, cancelConfirm := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelConfirm()
	if postErr != nil {
		landed, confirmErr := adapter.client.ConfirmsLanded(confirmCtx, sessionID, envelope.ID)
		if confirmErr == nil && landed {
			adapter.logger.Info("opencode_delivery_reconciled", "endpoint_id", sessionID,
				"message_id", envelope.ID, "post_error", postErr.Error())
		} else {
			adapter.logger.Info("opencode_delivery_unconfirmed", "endpoint_id", sessionID,
				"message_id", envelope.ID, "post_error", postErr.Error())
			return agentbridge.DeliveryResult{
				Code: agentbridge.ResultUnknown, NativeErrorClass: "idle_unconfirmed", Detail: postErr.Error(),
			}
		}
	}
	final, err := adapter.client.AwaitIdle(confirmCtx, sessionID)
	if err != nil {
		adapter.logger.Info("opencode_delivery_unconfirmed", "endpoint_id", sessionID, "error", err.Error())
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "idle_unconfirmed", Detail: err.Error(),
		}
	}
	if final.Err != nil {
		adapter.logger.Info("opencode_turn_failed", "endpoint_id", sessionID, "error", final.Err.Name)
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "turn_" + final.Err.Name,
			Detail: final.Err.Name,
		}
	}
	adapter.logger.Info("opencode_turn_delivered", "endpoint_id", sessionID, "texts", len(final.Texts))
	return agentbridge.Delivered(final.SessionID)
}

// healthProbeInterval keeps a health check from turning into a request per
// delivery; the answer only changes when the user's server does.
const healthProbeInterval = 5 * time.Second

// Health reports live reachability rather than a remembered flag: OpenCode is an
// optional dependency that the user may stop at any moment, and a stale "ready"
// would let the router believe messages are deliverable.
func (adapter *Adapter) Health(ctx context.Context) agentbridge.Health {
	adapter.mu.RLock()
	cached, probedAt := adapter.reachable, adapter.lastProbe
	adapter.mu.RUnlock()
	if time.Since(probedAt) < healthProbeInterval {
		if cached {
			return agentbridge.Ready()
		}
		return agentbridge.Unhealthy(agentbridge.ResultUnreachable, "OpenCode server is not answering")
	}
	reachable := Reachable(ctx, adapter.baseURL)
	adapter.mu.Lock()
	adapter.reachable, adapter.lastProbe = reachable, time.Now()
	adapter.mu.Unlock()
	if !reachable {
		return agentbridge.Unhealthy(agentbridge.ResultUnreachable, "OpenCode server is not answering")
	}
	return agentbridge.Ready()
}

func (adapter *Adapter) Close() error {
	adapter.mu.Lock()
	cancel := adapter.watcher
	adapter.watcher = nil
	adapter.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}
