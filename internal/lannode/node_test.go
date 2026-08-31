package lannode

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	coapdtls "github.com/plgd-dev/go-coap/v3/dtls"
	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/net/blockwise"
	"github.com/plgd-dev/go-coap/v3/options"
)

func TestNodeDiscoversAndCallsItself(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	received := make(chan Message, 1)
	node, err := Start(ctx, Config{
		ID:   "local-test-node",
		Name: "Local Test Node",
		PIN:  "A2B3C4",
		Sessions: func(context.Context) ([]Session, error) {
			return []Session{{ID: "thread-1", Title: "Local thread", Status: "idle"}}, nil
		},
		SendMessage: func(_ context.Context, message Message) error {
			received <- message
			return nil
		},
	})
	if err != nil {
		t.Fatalf("start node: %v", err)
	}
	defer node.Close()

	peer, err := node.WaitForPeer(ctx, "local-test-node")
	if err != nil {
		t.Fatalf("discover self: %v", err)
	}
	if peer.ID != "local-test-node" {
		t.Fatalf("peer ID = %q", peer.ID)
	}

	sessions, err := node.ListSessions(ctx, peer)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "thread-1" {
		t.Fatalf("sessions = %#v", sessions)
	}

	message := Message{TargetSessionID: "thread-1", Text: "hello", Source: "ra2a://sender/thread-2"}
	if err := node.SendMessage(ctx, peer, message); err != nil {
		t.Fatalf("send message: %v", err)
	}
	if got := <-received; got != message {
		t.Fatalf("received message = %#v", got)
	}
}

func TestSessionResponseRemainsCachedAcrossSlowBlockDownload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var calls atomic.Int32
	node, err := Start(ctx, Config{
		ID: "block-cache-test", Name: "Block Cache Test", PIN: "A2B3C4",
		Sessions: func(context.Context) ([]Session, error) {
			calls.Add(1)
			return []Session{{ID: "thread-1", Title: strings.Repeat("x", 4096)}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	peer, err := node.WaitForPeer(ctx, "block-cache-test")
	if err != nil {
		t.Fatal(err)
	}

	key := []byte("A2B3C4")
	dtlsOptions := coapdtls.NewDTLSClientOptions(
		dtls.WithPSK(func([]byte) ([]byte, error) { return key, nil }),
		dtls.WithPSKIdentityHint([]byte("ra2a-client")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	client, err := coapdtls.Dial(peer.Address, dtlsOptions,
		options.WithBlockwise(false, blockwise.SZX1024, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	first, err := client.Get(ctx, "/v1/sessions")
	if err != nil {
		t.Fatal(err)
	}
	token := append(message.Token(nil), first.Token()...)
	client.ReleaseMessage(first)

	time.Sleep(4 * time.Second)
	block, err := blockwise.EncodeBlockOption(blockwise.SZX1024, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	request, err := client.NewGetRequest(ctx, "/v1/sessions")
	if err != nil {
		t.Fatal(err)
	}
	request.SetToken(token)
	request.SetOptionUint32(message.Block2, block)
	second, err := client.Do(request)
	client.ReleaseMessage(request)
	if err != nil {
		t.Fatal(err)
	}
	client.ReleaseMessage(second)
	if got := calls.Load(); got != 1 {
		t.Fatalf("session enumeration calls = %d, want cached response", got)
	}
}

func TestSendMessageReturnsSessionBusyPrecondition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	node, err := Start(ctx, Config{
		ID: "busy-test-node", Name: "Busy Test Node", PIN: "A2B3C4",
		SendMessage: func(context.Context, Message) error { return errors.New("SESSION_BUSY") },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	peer, err := node.WaitForPeer(ctx, "busy-test-node")
	if err != nil {
		t.Fatal(err)
	}
	err = node.SendMessage(ctx, peer, Message{TargetSessionID: "thread", Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "PreconditionFailed") || !strings.Contains(err.Error(), "SESSION_BUSY") {
		t.Fatalf("error = %v", err)
	}
}

func TestPeersReturnsSortedSnapshot(t *testing.T) {
	node := &Node{peers: map[string]Peer{
		"z-node": {ID: "z-node", Name: "Z", Address: "127.0.0.1:2"},
		"a-node": {ID: "a-node", Name: "A", Address: "127.0.0.1:1"},
	}}
	got := node.Peers()
	want := []Peer{
		{ID: "a-node", Name: "A", Address: "127.0.0.1:1"},
		{ID: "z-node", Name: "Z", Address: "127.0.0.1:2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Peers() = %#v, want %#v", got, want)
	}
	got[0].Name = "changed"
	if node.peers["a-node"].Name != "A" {
		t.Fatal("Peers returned mutable internal state")
	}
}
