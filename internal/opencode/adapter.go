package opencode

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

// AgentKind is the agent identity this adapter publishes. OpenCode is an
// adapted agent: it holds real conversations, so it gets a first-class kind
// registered in the shared contract rather than being folded into the mailbox
// channel.
const AgentKind = agentbridge.AgentOpenCode

type Adapter struct {
	nodeID        string
	client        *Client
	baseURL       string
	logger        *slog.Logger
	unattachedLog sync.Once
	mu            sync.RWMutex
	adopted       map[string]struct{}
	landedBudget  time.Duration
	landedPoll    time.Duration
	watcher       context.CancelFunc
	lastProbe     time.Time
	reachable     bool
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

// ResolveCaller reports this node's attached OpenCode sessions when one of
// them acts as a sender. OpenCode does not put a stable caller identity in MCP
// metadata, so when exactly one attached session exists it can answer;
// otherwise it asks the caller to declare which session it is rather than
// attributing the message to the wrong conversation.
func (adapter *Adapter) ResolveCaller(_ context.Context, caller agentbridge.CallerContext) (agentbridge.Address, error) {
	sessions, err := adapter.client.ListSessions(context.Background())
	active := ocsession.Active(ocsession.Directory())
	eligible := sessions[:0]
	for _, session := range sessions {
		if active[session.ID] {
			eligible = append(eligible, session)
		}
	}
	sessions = eligible
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
		disconnected := false
		for {
			if err := adapter.client.Watch(watchCtx); watchCtx.Err() == nil {
				if !disconnected {
					adapter.logger.Info("opencode_event_stream_dropped", "error", err)
					disconnected = true
				}
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

// ListEndpoints publishes only sessions with a live attached RA2A TUI. The
// global session store also lists sessions owned by private OpenCode servers;
// sending to those sessions can be acknowledged without ever being executed.
func (adapter *Adapter) ListEndpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	sessions, err := adapter.client.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]agentbridge.Endpoint, 0, len(sessions))
	active := ocsession.Active(ocsession.Directory())
	unattached := 0
	for _, session := range sessions {
		if !active[session.ID] {
			unattached++
			continue
		}
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
	if unattached > 0 {
		adapter.unattachedLog.Do(func() {
			adapter.logger.Info("opencode_sessions_unattached", "count", unattached,
				"note", "sessions without a live --ra2a TUI are not published")
		})
	}
	return endpoints, nil
}

func (adapter *Adapter) Deliver(ctx context.Context, address agentbridge.Address, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	// Registry lookup gated delivery on the live attachment lease. Recheck here
	// because the TUI may have exited between lookup and POST.
	sessionID := address.EndpointID
	if !ocsession.Active(ocsession.Directory())[sessionID] {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultNotFound, Detail: "OpenCode session has no active RA2A attachment"}
	}
	marker := agentbridge.RenderIncomingText(envelope)
	postErr := adapter.client.PostMessage(ctx, sessionID, marker)
	if postErr == nil {
		// The host acknowledged the message. prompt_async answers as soon as it
		// has taken the message, and that acknowledgement is the delivery
		// verdict: the turn it starts may run for minutes on an active agent
		// session, which must never be mistaken for the message not arriving.
		adapter.logger.Info("opencode_delivery_accepted", "endpoint_id", sessionID, "message_id", envelope.ID)
		return agentbridge.Delivered(sessionID)
	}
	if IsUnreachable(postErr) {
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnreachable, NativeErrorClass: "server_unreachable", Detail: postErr.Error(),
		}
	}
	if !IsOutcomeUnknown(postErr) {
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "post_failed", Detail: postErr.Error(),
		}
	}
	// The deadline expired before the host acknowledged, which can happen when
	// the acknowledgement itself is lost even though the message was taken. The
	// history decides in that case, never the transport.
	budget, poll := adapter.landedBudget, adapter.landedPoll
	confirmCtx, cancelConfirm := context.WithTimeout(context.WithoutCancel(ctx), budget)
	defer cancelConfirm()
	landed, confirmErr := adapter.client.WaitForLanded(confirmCtx, sessionID, envelope.ID, budget, poll)
	if confirmErr == nil && landed {
		adapter.logger.Info("opencode_delivery_reconciled", "endpoint_id", sessionID,
			"message_id", envelope.ID, "post_error", postErr.Error(),
			"note", "the host recorded the message after the client deadline")
		return agentbridge.Delivered(sessionID)
	}
	adapter.logger.Info("opencode_delivery_unconfirmed", "endpoint_id", sessionID,
		"message_id", envelope.ID, "post_error", postErr.Error(), "confirm_error", confirmErr)
	return agentbridge.DeliveryResult{
		Code: agentbridge.ResultUnknown, NativeErrorClass: "not_accepted",
		Detail: fmt.Sprintf("opencode neither accepted the message nor recorded it in the session: %v", postErr),
	}
}

// landedBudget and landedPoll govern how long a delivery waits for the host to
// record the message. They only apply when the acknowledgement itself was lost,
// which is rare, so the budget is a reconciliation window rather than the normal
// delivery path: it has to stay comfortably inside every transport that carries
// the answer, and it must never grow into "wait for the recipient's turn".
const (
	defaultLandedBudget = 8 * time.Second
	defaultLandedPoll   = 400 * time.Millisecond
)

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
	if !probedAt.IsZero() && cached != reachable {
		if reachable {
			adapter.logger.Info("opencode_server_recovered", "server", adapter.baseURL)
		} else {
			adapter.logger.Info("opencode_server_unreachable", "server", adapter.baseURL)
		}
	}
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
