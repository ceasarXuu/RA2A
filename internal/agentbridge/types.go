// Package agentbridge defines the host-agnostic adapter boundary that lets
// RA2A route messages to any supported agent host through a uniform endpoint
// and delivery model. LAN discovery and runtime message formats intentionally
// stay outside this package; adapters own all host-specific details.
package agentbridge

// AgentType identifies a supported host family, e.g. "codex-app" or
// "codex-cli". Values are opaque strings exposed to callers as metadata.
type AgentType string

// AdapterDescriptor is static metadata about one adapter of a host family.
type AdapterDescriptor struct {
	AgentType       AgentType
	DisplayName     string
	ProtocolVersion string
}

// Capability is a named ability of an endpoint, e.g. "receiveText" or
// "interactiveSafe". The set is intentionally small and open.
type Capability string

const (
	CapabilityReceiveText Capability = "receiveText"
	CapabilityInteractive Capability = "interactiveSafe"
)

// Status describes the current deliverability of an endpoint.
type Status string

const (
	StatusReady     Status = "ready"
	StatusBusy      Status = "busy"
	StatusStarting  Status = "start_required"
	StatusUnknown   Status = "unknown"
	StatusUnmanaged Status = "unmanaged" // no verified adapter ownership
)

// Endpoint is one deliverable conversation exposed by an adapter.
//
// endpointID is unique within the node; nativeSessionID is interpreted only
// by the adapter; address is the opaque, complete target that RA2A exposes to
// callers. LAN code must never inspect address internals.
type Endpoint struct {
	EndpointID      string
	AgentType       AgentType
	NativeSessionID string
	Title           string
	Status          Status
	Capabilities    map[Capability]bool
	Address         string
	// NativeStatus carries the adapter-owned native status string (e.g. "idle")
	// for transitional LAN projection. Display only; routing never reads it.
	// Removed when lannode sessions gain a typed status in Phase 2.
	NativeStatus string
}

// DeliveryResult is the host-independent outcome of one delivery attempt.
type DeliveryResult struct {
	Code   string
	Detail string
}

// Delivery result codes. Adapters map native outcomes onto these values and
// keep the original diagnostics in Detail.
const (
	Delivered       = "delivered"
	Busy            = "busy"
	NotFound        = "not_found"
	Unreachable     = "unreachable"
	Unsupported     = "unsupported"
	StartRequired   = "start_required"
	DeliveryUnknown = "unknown"
	AdapterFailed   = "adapter_failed"
)

func (result DeliveryResult) OK() bool { return result.Code == Delivered }
