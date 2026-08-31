package lannode

import (
	"context"
	"testing"
	"time"
)

func TestNodeDiscoversAndCallsItself(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	node, err := Start(ctx, Config{
		ID:   "local-test-node",
		Name: "Local Test Node",
		PIN:  "A2B3C4",
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
	if len(sessions) != 0 {
		t.Fatalf("session count = %d, want 0", len(sessions))
	}
}
