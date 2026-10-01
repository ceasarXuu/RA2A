package codexcli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

func TestDirectInputRequiresExplicitHostCapability(t *testing.T) {
	for _, active := range []bool{false, true} {
		mode := "idle"
		if active {
			mode = "active"
		}
		for _, field := range []string{"true", "false", "missing", "null"} {
			t.Run(mode+"/"+field, func(t *testing.T) {
				server := newFakeAppServer(t)
				server.addThread(testThreadID, active, true)
				server.mu.Lock()
				server.directInputFields[testThreadID] = capabilityField(field)
				server.activeTurns[testThreadID] = "33333333-3333-4333-8333-333333333333"
				server.mu.Unlock()
				adapter := newTestAdapter(t, server)
				t.Cleanup(func() { _ = adapter.Close() })
				if err := adapter.Register(testThreadID); err != nil {
					t.Fatal(err)
				}
				endpoints, err := adapter.ListEndpoints(context.Background())
				if err != nil {
					t.Fatalf("list endpoints: %v", err)
				}
				if field == "true" {
					status := agentbridge.EndpointReady
					if active {
						status = agentbridge.EndpointBusy
					}
					if len(endpoints) != 1 || endpoints[0].Status != status ||
						!endpoints[0].Has(agentbridge.CapabilityReceiveText) ||
						!endpoints[0].Has(agentbridge.CapabilityInteractiveSafe) {
						t.Errorf("explicit true must publish a usable endpoint, got %+v", endpoints)
					}
				} else if len(endpoints) != 0 {
					t.Errorf("%s must not publish a receivable endpoint, got %+v", field, endpoints)
				}
				result := adapter.Deliver(context.Background(),
					agentbridge.Address{NodeID: "node-a", EndpointID: testThreadID}, testEnvelope("hi"))
				calls := server.callOrder()
				starts, steers := countCalls(calls, "turn/start"), countCalls(calls, "turn/steer")
				if field == "true" {
					if !result.Delivered() || starts+steers != 1 || (active && steers != 1) || (!active && starts != 1) {
						t.Errorf("explicit true must keep start/steer delivery: result=%+v, calls=%v", result, calls)
					}
				} else if result.Code != agentbridge.ResultUnsupported || result.NativeErrorClass != "capability_rejected" || starts+steers != 0 {
					t.Errorf("%s must reject with zero turn writes: result=%+v, starts=%d, steers=%d", field, result, starts, steers)
				}
				if server.unsubscribeCount() != 1 {
					t.Errorf("delivery must release its subscription, got %v", calls)
				}
			})
		}
	}
}

func TestDirectInputGateRechecksPreviouslyPublishedEndpoint(t *testing.T) {
	for _, field := range []string{"false", "missing", "null"} {
		t.Run(field, func(t *testing.T) {
			server := newFakeAppServer(t)
			server.addThread(testThreadID, false, true)
			adapter := newTestAdapter(t, server)
			t.Cleanup(func() { _ = adapter.Close() })
			if err := adapter.Register(testThreadID); err != nil {
				t.Fatal(err)
			}
			endpoints, err := adapter.ListEndpoints(context.Background())
			if err != nil || len(endpoints) != 1 {
				t.Fatalf("initial endpoint: %+v, %v", endpoints, err)
			}
			server.mu.Lock()
			server.directInputFields[testThreadID] = capabilityField(field)
			server.mu.Unlock()
			result := adapter.Deliver(context.Background(), endpoints[0].Address, testEnvelope("hi"))
			calls := server.callOrder()
			if result.Code != agentbridge.ResultUnsupported || countCalls(calls, "turn/start")+countCalls(calls, "turn/steer") != 0 {
				t.Fatalf("stale listing must not authorize a write: result=%+v, calls=%v", result, calls)
			}
		})
	}
}

func capabilityField(field string) json.RawMessage {
	if field == "missing" {
		return nil
	}
	return json.RawMessage(field)
}
