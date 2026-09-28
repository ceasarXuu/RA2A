package agentbridge

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// CallerContext is what an MCP host tells us about who is calling. Callers are
// never trusted: the resolver only accepts an identity that maps to an endpoint
// this node actually publishes.
type CallerContext struct {
	// DeclaredAddress is an opaque address the caller supplied explicitly.
	DeclaredAddress string
	// Meta is the raw MCP `_meta` object.
	Meta map[string]any
}

// CallerResolver is implemented by adapters that can recognise their own callers
// from MCP metadata. Adapters that cannot must return an error instead of
// guessing, so a wrong reply address is never invented.
type CallerResolver interface {
	ResolveCaller(context.Context, CallerContext) (Address, error)
}

// CallerHint is returned by adapters that recognise a caller namespace but need
// the caller to declare which endpoint it is.
type callerUnresolved struct{ Detail string }

func (err *callerUnresolved) Error() string { return err.Detail }

// ResolveCaller determines the caller's own endpoint. An explicitly declared
// address always wins because it is the only unambiguous signal available to a
// host-neutral caller; otherwise each adapter is asked in turn.
func (registry *Registry) ResolveCaller(ctx context.Context, caller CallerContext) (Address, error) {
	if strings.TrimSpace(caller.DeclaredAddress) != "" {
		address, err := ParseAddress(caller.DeclaredAddress)
		if err != nil {
			return Address{}, fmt.Errorf("declared caller address is invalid: %w", err)
		}
		if address.NodeID != registry.nodeID {
			return Address{}, fmt.Errorf("declared caller address belongs to node %q, not this node", address.NodeID)
		}
		if _, _, err := registry.Lookup(ctx, address); err != nil {
			return Address{}, fmt.Errorf("declared caller address is not published by this node: %w", err)
		}
		return address, nil
	}
	var reasons []string
	for _, adapter := range registry.Adapters() {
		resolver, ok := adapter.(CallerResolver)
		if !ok {
			reasons = append(reasons, fmt.Sprintf("%s: no caller metadata contract", adapter.Kind()))
			continue
		}
		address, err := resolver.ResolveCaller(ctx, caller)
		if err == nil {
			if _, _, lookupErr := registry.Lookup(ctx, address); lookupErr == nil {
				return address, nil
			}
			reasons = append(reasons, fmt.Sprintf("%s: resolved an endpoint this node does not publish", adapter.Kind()))
			continue
		}
		reasons = append(reasons, fmt.Sprintf("%s: %v", adapter.Kind(), err))
	}
	sort.Strings(reasons)
	return Address{}, fmt.Errorf("caller identity could not be resolved (%s); pass the caller's own address from list_targets as `from`",
		strings.Join(reasons, "; "))
}

// CallerHint lets an adapter explain what a caller must supply without
// inventing a default.
func CallerHint(format string, args ...any) error {
	return &callerUnresolved{Detail: fmt.Sprintf(format, args...)}
}

func IsCallerHint(err error) bool {
	var hint *callerUnresolved
	return asCallerHint(err, &hint)
}

func asCallerHint(err error, target **callerUnresolved) bool {
	for err != nil {
		if hint, ok := err.(*callerUnresolved); ok {
			*target = hint
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
