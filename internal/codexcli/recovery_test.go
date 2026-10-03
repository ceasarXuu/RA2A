package codexcli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

func TestDeliverPreservesEnvelopeWhenCompletionPrecedesResponse(t *testing.T) {
	for _, active := range []bool{false, true} {
		mode := "start"
		if active {
			mode = "steer"
		}
		t.Run(mode, func(t *testing.T) {
			server := newFakeAppServer(t)
			server.addThread(testThreadID, active, true)
			server.mu.Lock()
			server.completeBeforeAck = true
			if active {
				server.activeTurns[testThreadID] = "shared-active-turn"
			}
			server.mu.Unlock()
			adapter := newTestAdapter(t, server)
			t.Cleanup(func() { _ = adapter.Close() })
			if err := adapter.Register(testThreadID); err != nil {
				t.Fatal(err)
			}
			envelope := testEnvelope("reply to this")
			envelope.SourceAddress = "ra2a://sender/aaaaaaaa-bbbb-4ccc-8ddd-111111111111"
			envelope.ID = "message-1"
			result := adapter.Deliver(context.Background(), agentbridge.Address{EndpointID: testThreadID}, envelope)
			if !result.Delivered() {
				t.Fatalf("receipt must survive an earlier completion notification: %+v", result)
			}
			server.mu.Lock()
			inputs := append([]json.RawMessage(nil), server.inputs...)
			server.mu.Unlock()
			if len(inputs) != 1 {
				t.Fatalf("expected one host write, got %d", len(inputs))
			}
			var input struct {
				Input []struct{ Text string } `json:"input"`
			}
			if err := json.Unmarshal(inputs[0], &input); err != nil {
				t.Fatal(err)
			}
			if len(input.Input) != 1 || input.Input[0].Text != agentbridge.RenderIncomingText(envelope) {
				t.Fatalf("host input lost envelope provenance: %s", inputs[0])
			}
		})
	}
}

func TestAdapterReconnectsWithoutReplayingUncertainWrite(t *testing.T) {
	server := newFakeAppServer(t)
	server.addThread(testThreadID, false, true)
	adapter := newTestAdapter(t, server)
	t.Cleanup(func() { _ = adapter.Close() })
	if err := adapter.Register(testThreadID); err != nil {
		t.Fatal(err)
	}
	if endpoints, err := adapter.ListEndpoints(context.Background()); err != nil || len(endpoints) != 1 {
		t.Fatalf("initial connection: endpoints=%+v err=%v", endpoints, err)
	}
	old := adapter.conn
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if endpoints, err := adapter.ListEndpoints(context.Background()); err != nil || len(endpoints) != 1 {
		t.Fatalf("healthy backend must restore endpoints: endpoints=%+v err=%v", endpoints, err)
	}
	if adapter.conn == old {
		t.Fatal("adapter reused the closed connection")
	}
	server.mu.Lock()
	server.disconnectTurn = true
	server.mu.Unlock()
	result := adapter.Deliver(context.Background(), agentbridge.Address{EndpointID: testThreadID}, testEnvelope("write once"))
	if result.Code != agentbridge.ResultUnknown {
		t.Fatalf("disconnect after host input must remain unknown: %+v", result)
	}
	if endpoints, err := adapter.ListEndpoints(context.Background()); err != nil || len(endpoints) != 1 {
		t.Fatalf("next operation must reconnect: endpoints=%+v err=%v", endpoints, err)
	}
	if count := countCalls(server.callOrder(), "turn/start"); count != 1 {
		t.Fatalf("uncertain write was replayed %d times", count)
	}
}
