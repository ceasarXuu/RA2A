package agentbridge

import (
	"context"
	"errors"
)

// ErrClosed is returned by adapters or the registry after Close.
var ErrClosed = errors.New("agentbridge: closed")

// AgentAdapter abstracts one agent host family. Implementations are the only
// place that understands host protocols, writers, UI state, and native errors;
// the registry and callers must never branch on agent types.
type AgentAdapter interface {
	Descriptor() AdapterDescriptor
	ListEndpoints(context.Context) ([]Endpoint, error)
	Deliver(context.Context, Endpoint, string) (DeliveryResult, error)
	Health(context.Context) error
	Close() error
}
