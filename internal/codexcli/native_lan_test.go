//go:build linux || darwin

package codexcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/lannode"
)

// Start publishes short-lived test mDNS advertisements and binds a random
// all-interface port. Every request uses the explicit loopback self peer;
// discovery, production identities, and production nodes are not used.
func testNativeLANRecovery(t *testing.T, ctx context.Context, adapter *Adapter, owner *appServer, home, first string, awaitTurn func(string)) {
	t.Helper()
	var created threadResponse
	if err := owner.conn.call(ctx, "thread/start", map[string]any{
		"cwd": home, "model": "mock-model", "approvalPolicy": "never", "sandbox": "read-only",
	}, &created); err != nil {
		t.Fatal(err)
	}
	second := created.Thread.ID
	seed, err := owner.turnStart(ctx, second, "native-lan-second-seed")
	if err != nil {
		t.Fatal(err)
	}
	awaitTurn(seed.ID)
	if err := adapter.Register(second); err != nil {
		t.Fatal(err)
	}
	endpoints, err := adapter.ListEndpoints(ctx)
	if err != nil || len(endpoints) != 2 {
		t.Fatalf("two registered native endpoints: %+v, %v", endpoints, err)
	}
	for _, endpoint := range endpoints {
		if !endpoint.Has(agentbridge.CapabilityInteractiveSafe) {
			t.Fatalf("native endpoint cannot receive input: %+v", endpoint)
		}
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	prefix := "native-lan-" + hex.EncodeToString(random[:8])
	pin := hex.EncodeToString(random[9:12])
	start := func(id string, deliver func(context.Context, lannode.Message) error) *lannode.Node {
		t.Helper()
		node, err := lannode.Start(ctx, lannode.Config{ID: id, Name: id, PIN: pin, SendMessage: deliver})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(node.Close)
		return node
	}
	senderID, receiverID := prefix+"-sender", prefix+"-receiver"
	sender := start(senderID, nil)
	var deliveries atomic.Int32
	receipts := make(chan agentbridge.DeliveryResult, 3)
	withheld := make(chan agentbridge.DeliveryResult, 1)
	resumeResponse := make(chan struct{})
	const uncertainMarker = "native-lan-ack-withheld"
	accept := func(requestCtx context.Context, message lannode.Message) error {
		deliveries.Add(1)
		result := adapter.Deliver(requestCtx, agentbridge.Address{EndpointID: message.TargetSessionID}, agentbridge.MessageEnvelope{
			ID: message.MessageID, ProtocolVersion: agentbridge.ProtocolVersion, SourceAddress: message.Source, Text: message.Text,
		})
		if !result.Delivered() {
			return fmt.Errorf("native receipt: %+v", result)
		}
		if message.Text == uncertainMarker {
			withheld <- result
			<-resumeResponse
			return nil
		}
		receipts <- result
		return nil
	}
	receiver := start(receiverID, accept)
	peer := func(node *lannode.Node) lannode.Peer {
		t.Helper()
		peer, ok := node.Peer(receiverID)
		if !ok || !strings.HasPrefix(peer.Address, "127.0.0.1:") {
			t.Fatalf("explicit test loopback peer: %+v, %v", peer, ok)
		}
		return peer
	}
	message := func(target, source, marker string) lannode.Message {
		return lannode.Message{TargetSessionID: target, Source: "ra2a://" + senderID + "/" + source, MessageID: marker, Text: marker}
	}
	send := func(peer lannode.Peer, target, source, marker string) {
		t.Helper()
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := sender.SendMessage(requestCtx, peer, message(target, source, marker)); err != nil {
			t.Fatalf("native LAN receipt for %s: %v", marker, err)
		}
		select {
		case receipt := <-receipts:
			awaitTurn(receipt.TurnID)
		case <-requestCtx.Done():
			t.Fatal("LAN success without native receipt")
		}
	}
	assertInputs := func(marker string, firstWant, secondWant int) {
		t.Helper()
		for thread, want := range map[string]int{first: firstWant, second: secondWant} {
			if got := nativeUserInputCount(t, ctx, owner, thread, marker); got != want {
				t.Fatalf("thread %s marker %s input count=%d, want %d", thread, marker, got, want)
			}
		}
	}
	livePeer := peer(receiver)
	send(livePeer, first, second, "native-lan-first-only")
	assertInputs("native-lan-first-only", 1, 0)
	send(livePeer, second, first, "native-lan-second-only")
	assertInputs("native-lan-second-only", 0, 1)
	receiver.Close()
	requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	err = sender.SendMessage(requestCtx, livePeer, message(first, second, "native-lan-offline"))
	cancel()
	if !errors.Is(err, lannode.ErrPeerUnreachable) {
		t.Fatalf("closed test node must fail before delivery: %v", err)
	}
	if got := deliveries.Load(); got != 2 {
		t.Fatalf("offline attempt reached native adapter: calls=%d, want 2", got)
	}
	assertInputs("native-lan-offline", 0, 0)
	receiver = start(receiverID, accept)
	send(peer(receiver), first, second, "native-lan-after-recovery")
	assertInputs("native-lan-after-recovery", 1, 0)
	assertInputs("native-lan-offline", 0, 0)
	assertInputs("native-lan-first-only", 1, 0)
	assertInputs("native-lan-second-only", 0, 1)
	if got := deliveries.Load(); got != 3 {
		t.Fatalf("native adapter calls=%d, want three distinct accepted messages", got)
	}
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(resumeResponse) }) }
	t.Cleanup(resume) // Unblock the test handler before closing any test node.
	uncertainCtx, uncertainCancel := context.WithCancel(ctx)
	defer uncertainCancel()
	sendError := make(chan error, 1)
	recoveredPeer := peer(receiver)
	go func() {
		sendError <- sender.SendMessage(uncertainCtx, recoveredPeer, message(first, second, uncertainMarker))
	}()
	var accepted agentbridge.DeliveryResult
	select {
	case accepted = <-withheld:
	case <-time.After(5 * time.Second):
		t.Fatal("native input was not acknowledged before withholding CoAP response")
	}
	// Cancel only after the native ACK, so cold probes/handshakes cannot make
	// this look like a prewrite failure. control.Coordinator.Send maps this
	// wrapped context.Canceled (not ErrPeerUnreachable) to ErrDeliveryUnknown.
	uncertainCancel()
	select {
	case err := <-sendError:
		if !errors.Is(err, context.Canceled) || errors.Is(err, lannode.ErrPeerUnreachable) {
			t.Fatalf("missing CoAP ACK must preserve postwrite cancellation: %v", err)
		}
		t.Logf("withheld CoAP ACK raw send error (control maps to DELIVERY_UNKNOWN): %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("sender did not return after cancellation")
	}
	awaitTurn(accepted.TurnID)
	assertInputs(uncertainMarker, 1, 0)
	if got := deliveries.Load(); got != 4 {
		t.Fatalf("uncertain send callbacks=%d, want one additional callback", got)
	}
	resume()
	send(recoveredPeer, first, second, "native-lan-after-unknown")
	assertInputs("native-lan-after-unknown", 1, 0)
	assertInputs(uncertainMarker, 1, 0)
	if got := deliveries.Load(); got != 5 {
		t.Fatalf("recovery replayed uncertain input: callbacks=%d, want 5", got)
	}
	t.Log("real native CLI + DTLS/CoAP: two-thread target isolation, closed-node recovery, withheld ACK cancellation and no replay passed")
}

func nativeUserInputCount(t *testing.T, ctx context.Context, owner *appServer, threadID, marker string) int {
	t.Helper()
	var response struct {
		Thread struct {
			Turns []struct {
				Items []struct {
					Type    string `json:"type"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"items"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if err := owner.conn.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &response); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, turn := range response.Thread.Turns {
		for _, item := range turn.Items {
			if item.Type == "userMessage" {
				for _, content := range item.Content {
					if content.Type == "text" && strings.Contains(content.Text, marker) {
						count++
					}
				}
			}
		}
	}
	return count
}
