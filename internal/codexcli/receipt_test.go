package codexcli

import (
	"context"
	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"testing"
	"time"
)

func TestDeliverAcknowledgesReceiptWithoutCompletion(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "start"
		if active {
			name = "steer"
		}
		t.Run(name, func(t *testing.T) {
			server := newFakeAppServer(t)
			server.addThread(testThreadID, active, true)
			server.mu.Lock()
			server.suppressDone[testThreadID] = true
			if active {
				server.activeTurns[testThreadID] = "receipt-active-turn"
			}
			server.mu.Unlock()
			adapter := newTestAdapter(t, server)
			t.Cleanup(func() { _ = adapter.Close() })
			if err := adapter.Register(testThreadID); err != nil {
				t.Fatal(err)
			}
			// Bound receipt latency independently of cold daemon discovery and connection setup.
			endpoints, err := adapter.ListEndpoints(context.Background())
			if err != nil || len(endpoints) != 1 || endpoints[0].Address.EndpointID != testThreadID {
				t.Fatalf("prepare receipt target: endpoints=%+v err=%v", endpoints, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			result := adapter.Deliver(ctx, agentbridge.Address{EndpointID: testThreadID}, testEnvelope("received before reply"))
			if !result.Delivered() || result.TurnID == "" {
				t.Fatalf("host receipt must succeed without turn/completed: %+v", result)
			}
			if ctx.Err() != nil {
				t.Fatal("receipt waited for completion until caller deadline")
			}
			method := "turn/start"
			if active {
				method = "turn/steer"
			}
			if countCalls(server.callOrder(), method) != 1 {
				t.Fatal("receipt must write exactly once")
			}
		})
	}
}

func TestDeliverRequiresUnambiguousReceipt(t *testing.T) {
	for _, tc := range []struct {
		name       string
		active     bool
		invalid    string
		disconnect bool
	}{
		{"start-missing", false, "missing", false},
		{"start-error", false, "error", false},
		{"steer-missing", true, "missing", false},
		{"steer-wrong-turn", true, "wrong-steer", false},
		{"start-lost-ack", false, "", true},
		{"steer-lost-ack", true, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newFakeAppServer(t)
			server.addThread(testThreadID, tc.active, true)
			server.mu.Lock()
			server.invalidReceipt, server.disconnectTurn = tc.invalid, tc.disconnect
			if tc.active {
				server.activeTurns[testThreadID] = "receipt-active-turn"
			}
			server.mu.Unlock()
			adapter := newTestAdapter(t, server)
			t.Cleanup(func() { _ = adapter.Close() })
			if err := adapter.Register(testThreadID); err != nil {
				t.Fatal(err)
			}
			result := adapter.Deliver(context.Background(), agentbridge.Address{EndpointID: testThreadID}, testEnvelope("write once"))
			if result.Code != agentbridge.ResultUnknown {
				t.Fatalf("ambiguous ACK must remain unknown: %+v", result)
			}
			server.mu.Lock()
			writes := len(server.inputs)
			server.mu.Unlock()
			if writes != 1 {
				t.Fatalf("uncertain input must not be replayed: %d writes", writes)
			}
		})
	}
}

func TestDeliverDoesNotResumeOrReplayIfOwnerUnloadsBeforeWrite(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "start"
		if active {
			name = "steer"
		}
		t.Run(name, func(t *testing.T) {
			server := newFakeAppServer(t)
			server.addThread(testThreadID, active, true)
			server.mu.Lock()
			server.unloadBeforeTurn = true
			server.activeTurns[testThreadID] = "active-turn"
			server.mu.Unlock()
			adapter := newTestAdapter(t, server)
			t.Cleanup(func() { _ = adapter.Close() })
			if err := adapter.Register(testThreadID); err != nil {
				t.Fatal(err)
			}
			result := adapter.Deliver(context.Background(), agentbridge.Address{EndpointID: testThreadID}, testEnvelope("do not wake"))
			if result.Delivered() {
				t.Fatalf("unloaded thread accepted input: %+v", result)
			}
			calls := server.callOrder()
			if countCalls(calls, "turn/start")+countCalls(calls, "turn/steer") != 1 || countCalls(calls, "thread/resume")+countCalls(calls, "thread/unsubscribe") != 0 {
				t.Fatalf("owner change must not resume or replay: %v", calls)
			}
			server.mu.Lock()
			writes := len(server.inputs)
			server.mu.Unlock()
			if writes != 0 {
				t.Fatalf("unloaded owner received %d writes", writes)
			}
		})
	}
}
