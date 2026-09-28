// Package codexapp isolates the Codex App host integration behind the agent
// adapter boundary. It owns the Desktop IPC writer and the managed App Server
// fallback that RA2A already ships, without the router or the MCP layer learning
// anything about either host path.
package codexapp

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/desktopipc"
)

// SessionSource is the pre-adapter Codex App surface. Keeping it as an injected
// dependency lets the adapter be tested without a live host and lets the daemon
// decide, at wiring time, which managed host backs it.
type SessionSource interface {
	ListSessions(context.Context) ([]Session, error)
	SendMessage(context.Context, string, string) error
	Close() error
}

type Session struct {
	ID     string
	Title  string
	Status string
}

type Adapter struct {
	nodeID string
	source SessionSource
	logger *slog.Logger
}

func New(nodeID string, source SessionSource, stderr io.Writer) *Adapter {
	return &Adapter{
		nodeID: nodeID, source: source,
		logger: slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
}

func (adapter *Adapter) Kind() agentbridge.AgentKind { return agentbridge.AgentCodexApp }

func (adapter *Adapter) ListEndpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	sessions, err := adapter.source.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]agentbridge.Endpoint, 0, len(sessions))
	for _, session := range sessions {
		status := agentbridge.EndpointReady
		if session.Status != "" && session.Status != "idle" {
			status = agentbridge.EndpointBusy
		}
		title := session.Title
		if title == "" {
			title = session.ID
		}
		endpoints = append(endpoints, agentbridge.Endpoint{
			ID: session.ID, Agent: agentbridge.AgentCodexApp, NativeSessionID: session.ID,
			Title: title, Status: status,
			Capabilities: []agentbridge.Capability{
				agentbridge.CapabilityReceiveText,
				agentbridge.CapabilityReplyAddress,
				agentbridge.CapabilityInteractiveSafe,
			},
			Address: agentbridge.Address{NodeID: adapter.nodeID, EndpointID: session.ID},
		})
	}
	return endpoints, nil
}

func (adapter *Adapter) Deliver(ctx context.Context, address agentbridge.Address, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	err := adapter.source.SendMessage(ctx, address.EndpointID, agentbridge.RenderIncomingText(envelope))
	if err == nil {
		return agentbridge.Delivered("")
	}
	return mapHostError(err)
}

// mapHostError keeps the Desktop owner's availability and delivery-uncertainty
// semantics intact while presenting them through the unified result codes.
func mapHostError(err error) agentbridge.DeliveryResult {
	switch {
	case desktopipc.IsDeliveryUnknown(err):
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "delivery_unknown", Detail: err.Error(),
		}
	case desktopipc.IsNotDelivered(err):
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultStartRequired, NativeErrorClass: "owner_unavailable", Detail: err.Error(),
		}
	default:
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnknown, NativeErrorClass: "host_error", Detail: fmt.Sprint(err),
		}
	}
}

func (adapter *Adapter) Health(context.Context) agentbridge.Health { return agentbridge.Ready() }

func (adapter *Adapter) Close() error { return adapter.source.Close() }
