package desktopipc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestFindThreadOwnerRequiresLiveUnambiguousEvidence(t *testing.T) {
	for _, test := range []struct {
		name      string
		response  envelope
		owner     string
		wantError bool
	}{
		{"owner", envelope{ResultType: "success", HandledByClientID: "owner-a", Result: map[string]any{"supportsUntrustedAppInput": true}}, "owner-a", false},
		{"no owner", envelope{ResultType: "error", Error: "no-client-found"}, "", false},
		{"rejected", envelope{ResultType: "error", Error: "request-version-mismatch"}, "", true},
		{"decorated no client", envelope{ResultType: "error", Error: "no-client-found: timeout"}, "", true},
		{"missing owner", envelope{ResultType: "success", Result: map[string]any{"supportsUntrustedAppInput": true}}, "", true},
		{"unsupported", envelope{ResultType: "success", HandledByClientID: "owner-a", Result: map[string]any{"supportsUntrustedAppInput": false}}, "", true},
		{"missing capability", envelope{ResultType: "success", HandledByClientID: "owner-a"}, "", true},
		{"malformed capability", envelope{ResultType: "success", HandledByClientID: "owner-a", Result: map[string]any{"supportsUntrustedAppInput": "true"}}, "", true},
		{"missing result type", envelope{HandledByClientID: "owner-a", Result: map[string]any{"supportsUntrustedAppInput": true}}, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn, peer := net.Pipe()
			defer conn.Close()
			defer peer.Close()
			client := New(conn)
			client.clientID = "probe"
			done := make(chan error, 1)
			go func() {
				request, err := readFrame(peer)
				if err != nil {
					done <- err
					return
				}
				if request.Method != "thread-owner-discovery" || request.Version != 1 || request.Params["hostId"] != "local" || request.Params["conversationId"] != "thread-a" || request.TargetClientID != "" {
					t.Errorf("unexpected discovery request: %+v", request)
				}
				response := test.response
				response.Type = "response"
				response.RequestID = request.RequestID
				done <- writeFrame(peer, response)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			owner, err := client.FindThreadOwner(ctx, "thread-a")
			if owner != test.owner || (err != nil) != test.wantError {
				t.Fatalf("owner=%q err=%v", owner, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if client.ownerID != "" {
				t.Fatal("discovery selected a writer implicitly")
			}
		})
	}
}

func TestSelectedOwnerBindsStartAndSteerOnlyWhenExplicit(t *testing.T) {
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	client := New(conn)
	client.clientID = "probe"
	done := make(chan error, 1)
	go func() {
		for _, expected := range []struct{ method, owner string }{{"thread-follower-start-turn", ""}, {"thread-follower-start-turn", "owner-a"}, {"thread-follower-steer-turn", "owner-a"}, {"thread-follower-start-turn", ""}} {
			request, err := readFrame(peer)
			if err != nil {
				done <- err
				return
			}
			if request.Method != expected.method || request.TargetClientID != expected.owner {
				t.Errorf("request=%+v expected=%+v", request, expected)
			}
			result := map[string]any{"turn": map[string]any{"id": "turn-a"}, "turnId": "turn-a"}
			if err := writeFrame(peer, envelope{Type: "response", RequestID: request.RequestID, ResultType: "success", Result: result}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.StartTurn(ctx, "thread-a", "hello", "message-a", "model-a"); err != nil {
		t.Fatal(err)
	}
	client.SelectThreadOwner("owner-a")
	if _, err := client.StartTurn(ctx, "thread-a", "hello", "message-b", "model-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.steerTurn(ctx, "thread-a", "hello", "message-c"); err != nil {
		t.Fatal(err)
	}
	client.SelectThreadOwner("")
	if _, err := client.StartTurn(ctx, "thread-a", "hello", "message-d", "model-a"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFindThreadOwnerTransportFailureIsUnknown(t *testing.T) {
	conn, peer := net.Pipe()
	defer conn.Close()
	client := New(conn)
	client.clientID = "probe"
	peer.Close()
	if owner, err := client.FindThreadOwner(context.Background(), "thread-a"); owner != "" || err == nil {
		t.Fatalf("owner=%q err=%v", owner, err)
	}
}

func TestFindThreadOwnerCancellationAndDeadlineRemainUnknown(t *testing.T) {
	t.Run("canceled awaiting response", func(t *testing.T) {
		conn, peer := net.Pipe()
		defer conn.Close()
		defer peer.Close()
		client := New(conn)
		client.clientID = "probe"
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := readFrame(peer); cancel(); done <- err }()
		if owner, err := client.FindThreadOwner(ctx, "thread-a"); owner != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("owner=%q err=%v", owner, err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("canceled before write", func(t *testing.T) {
		conn, peer := net.Pipe()
		defer conn.Close()
		defer peer.Close()
		client := New(conn)
		client.clientID = "probe"
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if owner, err := client.FindThreadOwner(ctx, "thread-a"); owner != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("owner=%q err=%v", owner, err)
		}
	})
	t.Run("response deadline", func(t *testing.T) {
		conn, peer := net.Pipe()
		defer conn.Close()
		defer peer.Close()
		client := New(conn)
		client.clientID = "probe"
		done := make(chan error, 1)
		go func() { _, err := readFrame(peer); done <- err }()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if owner, err := client.FindThreadOwner(ctx, "thread-a"); owner != "" || err == nil {
			t.Fatalf("owner=%q err=%v", owner, err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
