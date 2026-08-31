package lannode

import (
	"context"
	"testing"
	"time"
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
