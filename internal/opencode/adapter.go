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
	nodeID  string
	client  *Client
	logger  *slog.Logger
	mu      sync.RWMutex
	adopted map[string]struct{}
	watcher context.CancelFunc
	ready   bool
}

func New(nodeID string, client *Client, stderr io.Writer) *Adapter {
	if stderr == nil {
		stderr = io.Discard
	}
	return &Adapter{
		nodeID: nodeID, client: client,
		logger:  slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		adopted: make(map[string]struct{}),
	}
}

func (adapter *Adapter) Kind() agentbridge.AgentKind { return AgentKind }

// Adopt records an OpenCode session as published by this node. OpenCode exposes
// no per-client session ownership, so publication is an explicit decision, the
// same rule the Codex CLI adapter follows.
func (adapter *Adapter) Adopt(sessionID string) error {
	if sessionID == "" {
		return errors.New("adopt opencode session: empty session id")
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.adopted[sessionID] = struct{}{}
	return nil
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

func (adapter *Adapter) ListEndpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	adopted := adapter.Adopted()
	if len(adopted) == 0 {
		return nil, nil
	}
	sessions, err := adapter.client.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Session, len(sessions))
	for _, session := range sessions {
		byID[session.ID] = session
	}
	endpoints := make([]agentbridge.Endpoint, 0, len(adopted))
	for _, sessionID := range adopted {
		session, exists := byID[sessionID]
		if !exists {
			adapter.logger.Info("opencode_session_missing", "endpoint_id", sessionID)
			continue
		}
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

func (adapter *Adapter) Health(context.Context) agentbridge.Health {
	adapter.mu.RLock()
	ready := adapter.ready
	adapter.mu.RUnlock()
	if !ready {
		return agentbridge.Unhealthy(agentbridge.ResultUnknown, "opencode server has not answered yet")
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
