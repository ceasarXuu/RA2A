//go:build linux

// Diagnostic only: run inside a private network namespace. No Agent or model.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ceasarXuu/RA2A/internal/lannode"
)

func main() {
	self, selfErr := os.Readlink("/proc/self/ns/net")
	host := os.Getenv("RA2A_DISCOVERY_HOST_NETNS")
	if selfErr != nil || host == "" || self == host || os.Getenv("RA2A_DISCOVERY_NETNS") != self {
		panic("diagnostic requires an explicitly selected private network namespace")
	}
	if len(os.Args) != 2 {
		panic("expected node index")
	}
	id := "sandbox-probe-" + os.Args[1]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	incoming := make(chan lannode.Message, 3)
	node, err := lannode.Start(ctx, lannode.Config{ID: id, Name: id, PIN: "T3ST42",
		Sessions: func(context.Context) ([]lannode.Session, error) {
			return []lannode.Session{{ID: "receiver", Agent: "diagnostic", Status: "ready"}}, nil
		},
		SendMessage: func(_ context.Context, m lannode.Message) error { incoming <- m; return nil },
	})
	if err != nil {
		panic(err)
	}
	defer node.Close()
	for _, index := range []string{"1", "2", "3"} {
		if _, err := node.WaitForPeer(ctx, "sandbox-probe-"+index); err != nil {
			panic(err)
		}
	}
	next := map[string]string{"1": "2", "2": "3", "3": "1"}[os.Args[1]]
	peer, ok := node.Peer("sandbox-probe-" + next)
	if !ok {
		panic("peer missing")
	}
	sessions, err := node.ListSessions(ctx, peer)
	if err != nil || len(sessions) != 1 || sessions[0].Agent != "diagnostic" {
		panic(fmt.Sprint("sessions: ", err, sessions))
	}
	m := lannode.Message{TargetSessionID: "receiver", Source: "ra2a://" + id + "/source", MessageID: id + "-once", Text: "no-model-probe"}
	if err := node.SendMessage(ctx, peer, m); err != nil {
		panic(err)
	}
	select {
	case received := <-incoming:
		if received.Text != m.Text || received.MessageID == "" {
			panic("bad receipt")
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"node": id, "peer": peer, "discovered": node.Peers(), "received": received, "pass": true})
	case <-ctx.Done():
		panic(ctx.Err())
	}
}
