package codexapp

import (
	"context"
	"errors"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/appserverprobe"
)

type fakeHost struct {
	threads      []appserverprobe.ThreadSummary
	listErr      error
	resolveErr   error
	resolveCalls []string
	closed       bool
}

func (host *fakeHost) ListThreadSummaries(context.Context) ([]appserverprobe.ThreadSummary, error) {
	if host.listErr != nil {
		return nil, host.listErr
	}
	return append([]appserverprobe.ThreadSummary(nil), host.threads...), nil
}

func (host *fakeHost) ResolveThreadModel(_ context.Context, threadID string) (string, error) {
	host.resolveCalls = append(host.resolveCalls, threadID)
	if host.resolveErr != nil {
		return "", host.resolveErr
	}
	return "gpt-test", nil
}

func (host *fakeHost) Close() error {
	host.closed = true
	return nil
}

func TestDescriptorIdentifiesCodexApp(t *testing.T) {
	adapter := newAdapter(&fakeHost{}, true)
	descriptor := adapter.Descriptor()
	if descriptor.AgentType != "codex-app" || descriptor.DisplayName == "" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func TestEndpointForThreadUsesTransitionalIdentity(t *testing.T) {
	endpoint := endpointForThread(appserverprobe.ThreadSummary{
		ID: "01abc", Title: "Fix bug", Status: "idle",
	})
	if endpoint.EndpointID != "01abc" || endpoint.NativeSessionID != "01abc" || endpoint.Address != "01abc" {
		t.Fatalf("transitional identity not equal: %#v", endpoint)
	}
	if endpoint.AgentType != AgentTypeCodexApp {
		t.Fatalf("agent type = %q", endpoint.AgentType)
	}
	if endpoint.Status != agentbridge.StatusReady {
		t.Fatalf("status = %q, want ready", endpoint.Status)
	}
	if endpoint.NativeStatus != "idle" {
		t.Fatalf("native status = %q", endpoint.NativeStatus)
	}
	if !endpoint.Capabilities[agentbridge.CapabilityReceiveText] {
		t.Fatal("receiveText capability missing")
	}
}

func TestEndpointForThreadMapsBusyAndUnknown(t *testing.T) {
	busy := endpointForThread(appserverprobe.ThreadSummary{ID: "1", Status: "working"})
	if busy.Status != agentbridge.StatusBusy || busy.NativeStatus != "working" {
		t.Fatalf("busy endpoint = %#v", busy)
	}
	unknown := endpointForThread(appserverprobe.ThreadSummary{ID: "2", Status: "error"})
	if unknown.Status != agentbridge.StatusUnknown {
		t.Fatalf("error endpoint = %#v", unknown)
	}
}

func TestListEndpointsProjectsHostThreads(t *testing.T) {
	host := &fakeHost{threads: []appserverprobe.ThreadSummary{
		{ID: "b", Title: "Two", Status: "idle"},
		{ID: "a", Title: "One", Status: "working"},
	}}
	adapter := newAdapter(host, true)
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %d", len(endpoints))
	}
	if endpoints[0].EndpointID != "b" || endpoints[1].EndpointID != "a" {
		t.Fatalf("projection order = %#v", endpoints)
	}
}

func TestListEndpointsPropagatesHostError(t *testing.T) {
	host := &fakeHost{listErr: errors.New("host down")}
	adapter := newAdapter(host, true)
	if _, err := adapter.ListEndpoints(context.Background()); err == nil {
		t.Fatal("expected list error")
	}
}

func TestHealthChecksHost(t *testing.T) {
	adapter := newAdapter(&fakeHost{}, true)
	if err := adapter.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
	broken := newAdapter(&fakeHost{listErr: errors.New("host down")}, true)
	if err := broken.Health(context.Background()); err == nil {
		t.Fatal("expected health failure")
	}
}

func TestCloseDelegatesToHost(t *testing.T) {
	host := &fakeHost{}
	adapter := newAdapter(host, true)
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if !host.closed {
		t.Fatal("host was not closed")
	}
}
