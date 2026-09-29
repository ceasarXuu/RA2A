package opencode

import (
	"context"
	"errors"
	"fmt"
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
	nodeID       string
	client       *Client
	baseURL      string
	logger       *slog.Logger
	mu           sync.RWMutex
	adopted      map[string]struct{}
	landedBudget time.Duration
	landedPoll   time.Duration
	watcher      context.CancelFunc
	lastProbe    time.Time
	reachable    bool
}

func New(nodeID string, config Config, stderr io.Writer) *Adapter {
	if stderr == nil {
		stderr = io.Discard
	}
	client := NewClient(config)
	return &Adapter{
		nodeID: nodeID, client: client, baseURL: client.BaseURL(),
		logger:       slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		adopted:      make(map[string]struct{}),
		landedBudget: orDuration(config.LandedBudget, defaultLandedBudget),
		landedPoll:   orDuration(config.LandedPoll, defaultLandedPoll),
	}
}

func (adapter *Adapter) Kind() agentbridge.AgentKind { return AgentKind }

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
	// No ownership gate here. OpenCode sessions are globally listed and reachable
	// through the one shared server, so gating delivery on a local registry would
	// silently hide sessions from the mesh for no safety gain: a caller that can
	// reach this endpoint can already address any session.
	sessionID := address.EndpointID
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
	// OpenCode's message POST blocks for the whole turn, and it exposes no
	// per-message terminal event the way Codex's turn/completed does. Waiting for
	// session.idle to decide delivery is therefore wrong: on a busy session the
	// message is queued and the session stays busy until the queue drains, which
	// can be many minutes. The sender would be told the delivery failed while the
	// message is in fact accepted and visible to the recipient.
	//
	// The host's own acknowledgement is the message appearing in the session's
	// user messages. That is the delivery signal; turn completion only supplies
	// the reply, never the verdict.
	budget, poll := adapter.landedBudget, adapter.landedPoll
	confirmCtx, cancelConfirm := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancelConfirm()
	if postErr != nil {
		landed, confirmErr := adapter.client.WaitForLanded(confirmCtx, sessionID, envelope.ID, budget, poll)
		if confirmErr == nil && landed {
			adapter.logger.Info("opencode_delivery_reconciled", "endpoint_id", sessionID,
				"message_id", envelope.ID, "post_error", postErr.Error(),
				"note", "queued or accepted while the session was busy")
			return agentbridge.Delivered(sessionID)
		}
		adapter.logger.Info("opencode_delivery_unconfirmed", "endpoint_id", sessionID,
			"message_id", envelope.ID, "post_error", postErr.Error(), "confirm_error", confirmErr)
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "not_accepted",
			Detail: fmt.Sprintf("opencode neither accepted the message nor recorded it in the session: %v", postErr),
		}
	}
	// The POST returned a completed turn, so the reply is already known. The
	// history still has to be consulted, and it is consulted the same way: a
	// single check races the host's own write-back, so the first miss is not
	// evidence of anything.
	landed, confirmErr := adapter.client.WaitForLanded(confirmCtx, sessionID, envelope.ID, budget, poll)
	if confirmErr != nil || !landed {
		adapter.logger.Info("opencode_delivery_unconfirmed", "endpoint_id", sessionID,
			"message_id", envelope.ID, "reason", "history_missing_after_completed_turn")
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "history_missing",
			Detail: "turn completed but the message never appeared in the session history",
		}
	}
	if final, err := adapter.awaitTurnReply(ctx, sessionID); err == nil && final.Err != nil {
		adapter.logger.Info("opencode_turn_failed", "endpoint_id", sessionID, "error", final.Err.Name)
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "turn_" + final.Err.Name,
			Detail: final.Err.Name,
		}
	}
	adapter.logger.Info("opencode_turn_delivered", "endpoint_id", sessionID)
	return agentbridge.Delivered(sessionID)
}

// landedBudget and landedPoll govern how long a delivery waits for the host to
// record the message. The budget has to exceed the host's own call timeout
// because a queued message is written to history some time after acceptance.
const (
	defaultLandedBudget = 25 * time.Second
	defaultLandedPoll   = 400 * time.Millisecond
)

// awaitTurnReply reads the last assistant message so a host-side turn failure can
// still be reported. It is best effort: the delivery verdict does not depend on
// it, because a turn that never finishes must not turn a delivered message into a
// failed one.
func (adapter *Adapter) awaitTurnReply(ctx context.Context, sessionID string) (Message, error) {
	replyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	messages, err := adapter.client.Messages(replyCtx, sessionID)
	if err != nil {
		return Message{}, err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return messages[i], nil
		}
	}
	return Message{}, errors.New("no assistant message was produced")
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

// orDuration keeps a zero-valued tuning field meaning "use the default" instead
// of collapsing the budget to zero.
func orDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}
