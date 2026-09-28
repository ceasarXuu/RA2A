package agentbridge

import (
	"fmt"
	"strings"
)

// RenderIncomingText is the single place that turns a delivered envelope into
// the text an agent sees. Keeping one renderer means every adapter shows the
// same provenance header, and the envelope never carries host-specific fields.
func RenderIncomingText(envelope MessageEnvelope) string {
	var prompt strings.Builder
	prompt.WriteString("[RA2A message]\n")
	if envelope.SourceAddress != "" {
		fmt.Fprintf(&prompt, "from: %s\n", envelope.SourceAddress)
	}
	if envelope.ID != "" {
		fmt.Fprintf(&prompt, "message-id: %s\n", envelope.ID)
	}
	prompt.WriteString("\n")
	prompt.WriteString(envelope.Text)
	return prompt.String()
}
