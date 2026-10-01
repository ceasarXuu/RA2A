package codexcli

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

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
				t.Fatalf("completion before response must confirm delivery: %+v", result)
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

func completionPayload(t *testing.T, turnID, status string) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(turnCompletedParams{ThreadID: testThreadID, Turn: turnRecord{ID: turnID, Status: status}})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func newOutcomeTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	adapter := New("node-a", Config{Stderr: io.Discard, ConfirmWindow: time.Second})
	t.Cleanup(func() { _ = adapter.Close() })
	return adapter
}

func waitForTurnWaiters(t *testing.T, adapter *Adapter, turnID string, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		adapter.mu.Lock()
		outcome := adapter.turnWaiters[turnID]
		ready := outcome != nil && outcome.waiters == count
		adapter.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("expected %d waiters for %s", count, turnID)
}

func TestTurnCompletionIsRetainedAndBroadcast(t *testing.T) {
	for _, status := range []string{"completed", "failed"} {
		t.Run(status, func(t *testing.T) {
			adapter := newOutcomeTestAdapter(t)
			adapter.onNotification("turn/completed", completionPayload(t, "early", status))
			if turn := adapter.awaitTurnOutcome(context.Background(), "early"); turn.Status != status {
				t.Fatalf("early terminal state lost: %+v", turn)
			}
			results := make(chan turnRecord, 2)
			for index := 0; index < 2; index++ {
				go func() { results <- adapter.awaitTurnOutcome(context.Background(), "shared") }()
			}
			waitForTurnWaiters(t, adapter, "shared", 2)
			adapter.onNotification("turn/completed", completionPayload(t, "shared", status))
			for index := 0; index < 2; index++ {
				if turn := <-results; turn.Status != status {
					t.Fatalf("one completion must confirm every waiter: %+v", turn)
				}
			}
			adapter.onNotification("turn/completed", completionPayload(t, "shared", "duplicate"))
			if turn := adapter.awaitTurnOutcome(context.Background(), "shared"); turn.Status != status {
				t.Fatalf("duplicate notification replaced terminal state: %+v", turn)
			}
		})
	}
}

func TestCanceledTurnWaiterDoesNotRemoveOtherWaiters(t *testing.T) {
	adapter := newOutcomeTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	canceled, remaining := make(chan turnRecord, 1), make(chan turnRecord, 1)
	go func() { canceled <- adapter.awaitTurnOutcome(ctx, "shared") }()
	go func() { remaining <- adapter.awaitTurnOutcome(context.Background(), "shared") }()
	waitForTurnWaiters(t, adapter, "shared", 2)
	cancel()
	if turn := <-canceled; turn.Status != "unconfirmed" {
		t.Fatalf("canceled wait must remain unconfirmed: %+v", turn)
	}
	waitForTurnWaiters(t, adapter, "shared", 1)
	adapter.onNotification("turn/completed", completionPayload(t, "shared", "completed"))
	if turn := <-remaining; turn.Status != "completed" {
		t.Fatalf("canceling one wait removed its peer: %+v", turn)
	}
}

func TestCloseReleasesTurnWaiters(t *testing.T) {
	adapter := newOutcomeTestAdapter(t)
	results := make(chan turnRecord, 2)
	for index := 0; index < 2; index++ {
		go func() { results <- adapter.awaitTurnOutcome(context.Background(), "shared") }()
	}
	waitForTurnWaiters(t, adapter, "shared", 2)
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		select {
		case turn := <-results:
			if turn.Status != "unconfirmed" {
				t.Fatalf("closing adapter must not confirm a turn: %+v", turn)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Close left a turn waiter blocked")
		}
	}
	adapter.onNotification("turn/completed", completionPayload(t, "late", "completed"))
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.turnWaiters) != 0 {
		t.Fatal("closed adapter retained late notifications")
	}
}

func TestTerminalOutcomesExpire(t *testing.T) {
	adapter := newOutcomeTestAdapter(t)
	adapter.onNotification("turn/completed", completionPayload(t, "old", "completed"))
	adapter.mu.Lock()
	adapter.turnWaiters["old"].completedAt = time.Now().Add(-adapter.config.CallTimeout - adapter.config.ConfirmWindow - time.Second)
	adapter.mu.Unlock()
	adapter.onNotification("turn/completed", completionPayload(t, "new", "completed"))
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if _, retained := adapter.turnWaiters["old"]; retained {
		t.Fatal("expired completed turns must be pruned")
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
