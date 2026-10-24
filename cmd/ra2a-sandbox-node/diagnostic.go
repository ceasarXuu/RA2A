//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

// diagnosticAdapter is a no-model receiver used by the macOS matrix dry run.
// It records only the message id, target and first marker token; it never
// invokes a client or a provider.
type diagnosticAdapter struct {
	kind       agentbridge.AgentKind
	endpointID string
	mu         sync.Mutex
	file       *os.File
}

func newDiagnosticAdapter(_ string, kind agentbridge.AgentKind, path string) (*diagnosticAdapter, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("diagnostic adapter requires a known Agent kind")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("diagnostic output must be an absolute path")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	return &diagnosticAdapter{kind: kind, endpointID: "diagnostic-" + string(kind), file: file}, nil
}

func (adapter *diagnosticAdapter) Kind() agentbridge.AgentKind { return adapter.kind }

func (adapter *diagnosticAdapter) ListEndpoints(context.Context) ([]agentbridge.Endpoint, error) {
	return []agentbridge.Endpoint{{
		ID:           adapter.endpointID,
		Agent:        adapter.kind,
		Title:        "diagnostic no-model receiver",
		Status:       agentbridge.EndpointReady,
		Capabilities: []agentbridge.Capability{agentbridge.CapabilityReceiveText, agentbridge.CapabilityReplyAddress},
		Address:      agentbridge.Address{EndpointID: adapter.endpointID},
	}}, nil
}

func (adapter *diagnosticAdapter) Deliver(_ context.Context, target agentbridge.Address, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	if envelope.ID == "" || envelope.Text == "" {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnsupported}
	}
	marker := ""
	if fields := strings.Fields(envelope.Text); len(fields) > 0 {
		marker = fields[0]
	}
	record := map[string]string{"messageId": envelope.ID, "target": target.String(), "marker": marker}
	raw, err := json.Marshal(record)
	if err != nil {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnknown}
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if _, err := adapter.file.Write(append(raw, '\n')); err != nil {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnknown}
	}
	return agentbridge.Delivered("diagnostic-" + envelope.ID)
}

func (*diagnosticAdapter) Health(context.Context) agentbridge.Health { return agentbridge.Ready() }

func (adapter *diagnosticAdapter) Close() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.file == nil {
		return nil
	}
	err := adapter.file.Close()
	adapter.file = nil
	return err
}
