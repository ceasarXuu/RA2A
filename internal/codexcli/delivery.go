package codexcli

import (
	"context"
	"errors"
	"fmt"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

func (adapter *Adapter) ListEndpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	registered := adapter.Registered()
	if len(registered) == 0 {
		return nil, nil
	}
	server, err := adapter.connect(ctx)
	if err != nil {
		return nil, err
	}
	loaded, err := server.threadLoadedList(ctx)
	if err != nil {
		return nil, fmt.Errorf("list loaded threads: %w", err)
	}
	loadedSet := make(map[string]bool, len(loaded))
	for _, id := range loaded {
		loadedSet[id] = true
	}
	endpoints := make([]agentbridge.Endpoint, 0, len(registered))
	for _, threadID := range registered {
		if !loadedSet[threadID] {
			adapter.logger.Info("cli_ownership_unknown", "endpoint_id", threadID, "reason", "not_loaded")
			continue
		}
		thread, err := server.threadRead(ctx, threadID)
		if err != nil {
			adapter.logger.Info("cli_ownership_unknown", "endpoint_id", threadID,
				"reason", "read_failed", "error", err.Error())
			continue
		}
		if !thread.acceptsDirectInput() {
			adapter.logger.Info("cli_capability_rejected", "endpoint_id", threadID, "capability", "canAcceptDirectInput")
			continue
		}
		status := agentbridge.EndpointReady
		if thread.active() {
			status = agentbridge.EndpointBusy
		}
		capabilities := []agentbridge.Capability{
			agentbridge.CapabilityReceiveText, agentbridge.CapabilityReplyAddress,
			agentbridge.CapabilitySteerActiveTurn, agentbridge.CapabilityInteractiveSafe,
		}
		endpoints = append(endpoints, agentbridge.Endpoint{
			ID: threadID, Agent: agentbridge.AgentCodexCLI, NativeSessionID: threadID,
			Title: thread.displayTitle(), Status: status, Capabilities: capabilities,
			Address: agentbridge.Address{NodeID: adapter.nodeID, EndpointID: threadID},
		})
	}
	return endpoints, nil
}

func (adapter *Adapter) Deliver(ctx context.Context, address agentbridge.Address, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	threadID := address.EndpointID
	if !validThreadID(threadID) {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnsupported, Detail: "target is not a codex thread id"}
	}
	adapter.mu.Lock()
	_, known := adapter.registered[threadID]
	adapter.mu.Unlock()
	if !known {
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultNotFound, NativeErrorClass: "ownership_unknown",
			Detail: "thread is not registered with this adapter",
		}
	}
	server, err := adapter.connect(ctx)
	if err != nil {
		var startRequired *StartRequiredError
		if errors.As(err, &startRequired) {
			return agentbridge.DeliveryResult{Code: agentbridge.ResultStartRequired, Detail: startRequired.Detail}
		}
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnknown, Detail: err.Error()}
	}
	return adapter.deliverToThread(ctx, server, threadID, agentbridge.RenderIncomingText(envelope))
}

// A successful start/steer response confirms receipt of the input. Replies and
// turn completion belong to the native client and never delay delivery ACKs.
func (adapter *Adapter) deliverToThread(ctx context.Context, server *appServer, threadID, text string) agentbridge.DeliveryResult {
	callCtx, cancel := context.WithTimeout(ctx, adapter.config.CallTimeout)
	defer cancel()

	// Read capability/status without loading or subscribing. Native start/steer
	// only operate on an existing in-memory thread; never acquire a writer here.
	thread, err := server.threadRead(callCtx, threadID)
	if err != nil {
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultNotFound, NativeErrorClass: classifyRPCError(err), Detail: err.Error(),
		}
	}
	if !thread.acceptsDirectInput() {
		adapter.logger.Info("cli_capability_rejected", "endpoint_id", threadID, "capability", "canAcceptDirectInput")
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnsupported, NativeErrorClass: "capability_rejected",
			Detail: "host has not enabled direct input for this thread",
		}
	}
	activeTurnID := ""
	if thread.active() {
		turns, listErr := server.threadTurnsList(callCtx, threadID)
		if listErr != nil || len(turns) == 0 {
			adapter.logger.Info("cli_capability_rejected", "endpoint_id", threadID, "capability", "steerTargetUnknown")
			return agentbridge.DeliveryResult{
				Code: agentbridge.ResultBusy, NativeErrorClass: "turn_active",
				Detail: "thread is busy and the active turn could not be resolved",
			}
		}
		activeTurnID = turns[0]
	}

	turn, err := adapter.submitTurn(ctx, server, threadID, activeTurnID, text)
	if err != nil {
		return agentbridge.DeliveryResult{
			Code:             agentbridge.ResultUnknown,
			NativeErrorClass: classifyRPCError(err),
			Detail:           err.Error(),
		}
	}
	adapter.logger.Info("cli_message_received", "endpoint_id", threadID, "turn_id", turn.ID, "mode", submitMode(activeTurnID))
	return agentbridge.Delivered(turn.ID)
}

func submitMode(activeTurnID string) string {
	if activeTurnID == "" {
		return "start"
	}
	return "steer"
}

func (adapter *Adapter) submitTurn(ctx context.Context, server *appServer, threadID, activeTurnID, text string) (turnRecord, error) {
	callCtx, cancel := context.WithTimeout(ctx, adapter.config.CallTimeout)
	defer cancel()
	if activeTurnID != "" {
		return server.turnSteer(callCtx, threadID, activeTurnID, text)
	}
	return server.turnStart(callCtx, threadID, text)
}

// CheckTargetID implements agentbridge.TargetShapeChecker so the router can tell
// a malformed Codex thread id apart from an unpublished one.
func (adapter *Adapter) CheckTargetID(endpointID string) error {
	if validThreadID(endpointID) {
		return nil
	}
	return fmt.Errorf("expected a UUID or urn:uuid: thread id, got %q", endpointID)
}

// ResolveCaller intentionally does not guess. The Codex CLI MCP client has not
// been observed to publish a stable caller identity, and inferring one from the
// loaded thread set would silently attribute messages to the wrong thread. The
// caller must therefore declare its own address.
func (adapter *Adapter) ResolveCaller(context.Context, agentbridge.CallerContext) (agentbridge.Address, error) {
	return agentbridge.Address{}, agentbridge.CallerHint(
		"Codex CLI does not publish a stable caller identity in MCP metadata; pass `from` with this thread's address")
}
