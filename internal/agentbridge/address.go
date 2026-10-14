package agentbridge

import (
	"fmt"
	"strings"
)

func parseEnvelopeTarget(target string) (Address, error) {
	if !strings.HasPrefix(target, "ra2a://") {
		return Address{}, fmt.Errorf("target %q is not an ra2a address", target)
	}
	rest := strings.TrimPrefix(target, "ra2a://")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Address{}, fmt.Errorf("target %q must be ra2a://node/endpoint", target)
	}
	address := Address{NodeID: parts[0], EndpointID: parts[1]}
	if !address.Valid() {
		return Address{}, fmt.Errorf("target %q is malformed", target)
	}
	return address, nil
}

// ParseAddress is provided for the daemon control plane, which receives opaque
// addresses from MCP callers. It performs no interpretation beyond splitting the
// two opaque segments.
func ParseAddress(target string) (Address, error) { return parseEnvelopeTarget(target) }
