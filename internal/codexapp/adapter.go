// Package codexapp implements the codex-app agentbridge adapter: it owns the
// managed Codex App Server host (session discovery and thread-model
// resolution) and delivers messages through Codex Desktop IPC, which is the
// verified writer owner of App sessions. LAN/control data models
// intentionally stay outside this package.
package codexapp

import (
	"context"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/appserverprobe"
	"github.com/ceasarXuu/RA2A/internal/codexhost"
)

// AgentTypeCodexApp is the agentbridge identity of this adapter.
const AgentTypeCodexApp agentbridge.AgentType = "codex-app"

// sessionHost is the slice of codexhost.Host this adapter needs; it is an
// interface so adapter tests never start a managed process.
type sessionHost interface {
	ListThreadSummaries(context.Context) ([]appserverprobe.ThreadSummary, error)
	ResolveThreadModel(context.Context, string) (string, error)
	Close() error
}

// Adapter implements agentbridge.AgentAdapter for the Codex App / Desktop
// host family. Deliver goes through Desktop IPC (desktop-first policy); the
// managed host is used for discovery and model resolution only and is never a
// write fallback, matching the verified v0.0.10-v0.0.14 semantics.
type Adapter struct {
	host           sessionHost
	desktopEnabled bool
}

// New eagerly starts the managed Codex App Server host (same lifecycle as the
// historical daemon session source) and returns a ready adapter.
// desktopEnabled=false disables Desktop IPC delivery: Deliver then reports
// start_required without dialing, for future headless installs.
func New(ctx context.Context, hostConfig codexhost.Config, desktopEnabled bool) (*Adapter, error) {
	host, err := codexhost.Start(ctx, hostConfig)
	if err != nil {
		return nil, err
	}
	return newAdapter(host, desktopEnabled), nil
}

// newAdapter builds an adapter over an injected host; used by New and tests.
func newAdapter(host sessionHost, desktopEnabled bool) *Adapter {
	return &Adapter{host: host, desktopEnabled: desktopEnabled}
}

// Descriptor returns the static identity of this adapter.
func (adapter *Adapter) Descriptor() agentbridge.AdapterDescriptor {
	return agentbridge.AdapterDescriptor{
		AgentType:   AgentTypeCodexApp,
		DisplayName: "Codex App",
	}
}

// ListEndpoints projects every discovered App thread onto the uniform
// endpoint model. The endpoint identity is transitional: endpointID,
// nativeSessionID and address all equal the native thread ID until Phase 2
// introduces typed endpoint addresses.
func (adapter *Adapter) ListEndpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	threads, err := adapter.host.ListThreadSummaries(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]agentbridge.Endpoint, 0, len(threads))
	for _, thread := range threads {
		endpoints = append(endpoints, endpointForThread(thread))
	}
	return endpoints, nil
}

// Deliver sends one text message to the endpoint's native thread through the
// Codex Desktop owner.
func (adapter *Adapter) Deliver(ctx context.Context, endpoint agentbridge.Endpoint, text string) (agentbridge.DeliveryResult, error) {
	if err := ctx.Err(); err != nil {
		return agentbridge.DeliveryResult{}, err
	}
	return adapter.deliverDesktop(ctx, endpoint.NativeSessionID, text)
}

// Health reports whether the adapter can serve discovery; best effort through
// the managed host.
func (adapter *Adapter) Health(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := adapter.host.ListThreadSummaries(ctx)
	return err
}

// Close shuts down the managed Codex App Server host.
func (adapter *Adapter) Close() error {
	if adapter.host == nil {
		return nil
	}
	return adapter.host.Close()
}

// endpointForThread is the pure thread-to-endpoint projection.
func endpointForThread(thread appserverprobe.ThreadSummary) agentbridge.Endpoint {
	status := agentbridge.StatusBusy
	switch thread.Status {
	case "idle":
		status = agentbridge.StatusReady
	case "error", "unknown", "":
		status = agentbridge.StatusUnknown
	}
	return agentbridge.Endpoint{
		EndpointID:      thread.ID,
		AgentType:       AgentTypeCodexApp,
		NativeSessionID: thread.ID, // transitional: endpoint identity == native thread ID
		Title:           thread.Title,
		Status:          status,
		Capabilities:    map[agentbridge.Capability]bool{agentbridge.CapabilityReceiveText: true},
		Address:         thread.ID, // transitional identity, see ListEndpoints
		NativeStatus:    thread.Status,
	}
}

var _ agentbridge.AgentAdapter = (*Adapter)(nil)
