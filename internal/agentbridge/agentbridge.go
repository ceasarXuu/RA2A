// Package agentbridge defines the host-agnostic contract between RA2A and the
// agents it connects. The router, LAN layer and MCP layer only ever see these
// types: they never learn a concrete host type, so adding an agent means adding
// one adapter instead of editing routing code.
package agentbridge

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type AgentKind string

const (
	AgentCodexApp AgentKind = "codex-app"
	AgentCodexCLI AgentKind = "codex-cli"
	AgentOpenCode AgentKind = "opencode"
)

func (kind AgentKind) Valid() bool {
	switch kind {
	case AgentCodexApp, AgentCodexCLI, AgentOpenCode:
		return true
	}
	return false
}

type Capability string

const (
	CapabilityReceiveText      Capability = "receiveText"
	CapabilityReplyAddress     Capability = "replyAddress"
	CapabilityInteractiveSafe  Capability = "interactiveSafe"
	CapabilitySteerActiveTurn  Capability = "steerActiveTurn"
	CapabilityTerminalRequired Capability = "terminalRequired"
)

var allCapabilities = map[Capability]bool{
	CapabilityReceiveText:      true,
	CapabilityReplyAddress:     true,
	CapabilityInteractiveSafe:  true,
	CapabilitySteerActiveTurn:  true,
	CapabilityTerminalRequired: true,
}

func (capability Capability) Valid() bool { return allCapabilities[capability] }

type EndpointStatus string

const (
	EndpointReady   EndpointStatus = "ready"
	EndpointBusy    EndpointStatus = "busy"
	EndpointUnknown EndpointStatus = "unknown"
)

// Endpoint is one addressable conversation owned by exactly one adapter.
// NativeSessionID is interpreted only by the owning adapter and is never
// exposed to LAN or MCP callers; Address is the only handle callers may use.
type Endpoint struct {
	ID              string         `json:"id"`
	Agent           AgentKind      `json:"agent"`
	NativeSessionID string         `json:"-"`
	Title           string         `json:"title"`
	Status          EndpointStatus `json:"status"`
	Capabilities    []Capability   `json:"capabilities"`
	Address         Address        `json:"address"`
}

func (endpoint Endpoint) Has(capability Capability) bool {
	for _, candidate := range endpoint.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

// Validate rejects endpoints that would break the router invariants: an
// endpoint must carry a known agent kind, an addressable status, a non-empty
// capability set and an address that round-trips.
func (endpoint Endpoint) Validate() error {
	if endpoint.ID == "" {
		return errors.New("endpoint id is required")
	}
	if !endpoint.Agent.Valid() {
		return fmt.Errorf("unknown agent kind %q", endpoint.Agent)
	}
	if endpoint.Status != EndpointReady && endpoint.Status != EndpointBusy {
		return fmt.Errorf("endpoint %s has status %q", endpoint.ID, endpoint.Status)
	}
	if len(endpoint.Capabilities) == 0 {
		return fmt.Errorf("endpoint %s has no capabilities", endpoint.ID)
	}
	for _, capability := range endpoint.Capabilities {
		if !capability.Valid() {
			return fmt.Errorf("endpoint %s has unknown capability %q", endpoint.ID, capability)
		}
	}
	if endpoint.Address.EndpointID != endpoint.ID {
		return fmt.Errorf("endpoint %s address %q does not match its id", endpoint.ID, endpoint.Address.String())
	}
	return nil
}

// Address is the opaque target handle handed to callers. Callers must treat it
// as a value returned by ListTargets and must not construct or parse its parts.
type Address struct {
	NodeID     string `json:"node"`
	EndpointID string `json:"endpoint"`
}

func (address Address) String() string {
	return "ra2a://" + address.NodeID + "/" + address.EndpointID
}

func (address Address) Valid() bool {
	return address.NodeID != "" && address.EndpointID != "" &&
		!strings.ContainsAny(address.NodeID+address.EndpointID, "/?#")
}

const ProtocolVersion = 1

type MessageEnvelope struct {
	ID              string    `json:"messageId"`
	ProtocolVersion int       `json:"protocolVersion"`
	SourceAddress   string    `json:"source"`
	TargetAddress   string    `json:"target"`
	Text            string    `json:"text"`
	CreatedAt       time.Time `json:"createdAt"`
}

func (envelope MessageEnvelope) Validate() error {
	if envelope.Text == "" {
		return errors.New("message text is required")
	}
	if envelope.ID == "" {
		return errors.New("message id is required")
	}
	if envelope.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported envelope protocol version %d", envelope.ProtocolVersion)
	}
	return nil
}

type ResultCode string

const (
	ResultDelivered     ResultCode = "delivered"
	ResultBusy          ResultCode = "busy"
	ResultNotFound      ResultCode = "not_found"
	ResultUnreachable   ResultCode = "unreachable"
	ResultUnsupported   ResultCode = "unsupported"
	ResultStartRequired ResultCode = "start_required"
	ResultUnknown       ResultCode = "unknown"
)

func (code ResultCode) Valid() bool {
	switch code {
	case ResultDelivered, ResultBusy, ResultNotFound, ResultUnreachable,
		ResultUnsupported, ResultStartRequired, ResultUnknown:
		return true
	}
	return false
}

// DeliveryResult carries the unified verdict plus the adapter's own diagnostic
// detail. NativeErrorClass keeps host failures classifiable without leaking
// host-specific error text into the cross-agent protocol.
type DeliveryResult struct {
	Code             ResultCode `json:"code"`
	TurnID           string     `json:"turnId,omitempty"`
	Detail           string     `json:"detail,omitempty"`
	NativeErrorClass string     `json:"nativeErrorClass,omitempty"`
}

func Delivered(turnID string) DeliveryResult {
	return DeliveryResult{Code: ResultDelivered, TurnID: turnID}
}

func (result DeliveryResult) Delivered() bool { return result.Code == ResultDelivered }

type Health struct {
	Ready  bool       `json:"ready"`
	Code   ResultCode `json:"code"`
	Detail string     `json:"detail,omitempty"`
}

func Ready() Health { return Health{Ready: true, Code: ResultDelivered} }

func Unhealthy(code ResultCode, detail string) Health { return Health{Code: code, Detail: detail} }

func (health Health) Validate() error {
	if health.Ready {
		return nil
	}
	if !health.Code.Valid() || health.Code == ResultDelivered {
		return fmt.Errorf("unhealthy adapter must report a failure code, got %q", health.Code)
	}
	return nil
}
