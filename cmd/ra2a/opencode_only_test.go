package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/ocsession"
	"github.com/ceasarXuu/RA2A/internal/operator"
)

func TestOpenCodeOnlyNodesDeliverInBothDirections(t *testing.T) {
	home := isolatedOperatorHome(t)
	t.Setenv("RA2A_DISABLE_OPENCODE", "0")
	t.Setenv("RA2A_OC_SESSION_DIR", filepath.Join(home, "leases"))
	if err := operator.Save(operator.Config{NodeID: "node-a", Name: "node-a", PIN: "A1B2C3", OpenCode: "/installed/opencode"}); err != nil {
		t.Fatal(err)
	}
	createServer := func(session string) (*httptest.Server, <-chan string) {
		t.Helper()
		messages := make(chan string, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/session":
				_ = json.NewEncoder(w).Encode([]map[string]string{{"id": session, "title": session}})
			case r.Method == http.MethodPost && r.URL.Path == "/session/"+session+"/prompt_async":
				body, _ := io.ReadAll(r.Body)
				messages <- string(body)
				w.WriteHeader(http.StatusNoContent)
			case r.Method == http.MethodGet && r.URL.Path == "/event":
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {}\n\n"))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)
		return server, messages
	}
	first, firstMessages := createServer("ses_first")
	second, secondMessages := createServer("ses_second")
	for _, session := range []string{"ses_first", "ses_second"} {
		release, err := ocsession.Register(ocsession.Directory(), session)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(release)
	}
	startSource := func(context.Context, string, string, io.Writer) (sessionSource, error) {
		return nil, errors.New("Codex App Server must not start on an OpenCode-only node")
	}
	newNode := func(nodeID, url string) *agentbridge.Registry {
		t.Helper()
		t.Setenv("RA2A_OPENCODE_URL", url)
		registry, err := buildRegistry(context.Background(), nodeID, "", "", io.Discard, startSource, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = registry.Close() })
		return registry
	}
	a := newNode("node-a", first.URL)
	b := newNode("node-b", second.URL)
	for _, direction := range []struct {
		registry *agentbridge.Registry
		to       string
		from     string
		seen     <-chan string
	}{
		{b, "ra2a://node-b/ses_second", "ra2a://node-a/ses_first", secondMessages},
		{a, "ra2a://node-a/ses_first", "ra2a://node-b/ses_second", firstMessages},
	} {
		envelope := agentbridge.MessageEnvelope{ID: "msg_open_only", ProtocolVersion: agentbridge.ProtocolVersion,
			SourceAddress: direction.from, TargetAddress: direction.to, Text: "cross-node OpenCode message", CreatedAt: time.Now()}
		result := direction.registry.Deliver(context.Background(), envelope)
		if !result.Delivered() {
			t.Fatalf("%s -> %s: %+v", direction.from, direction.to, result)
		}
		select {
		case payload := <-direction.seen:
			if !strings.Contains(payload, "cross-node OpenCode message") {
				t.Fatalf("missing message: %s", payload)
			}
		case <-time.After(time.Second):
			t.Fatal("host did not receive the message")
		}
	}
	if endpoints, problems := a.Endpoints(context.Background()); len(problems) != 0 || len(endpoints) != 1 {
		t.Fatalf("OpenCode-only node A should publish one endpoint: %v %v", endpoints, problems)
	}
	if endpoints, problems := b.Endpoints(context.Background()); len(problems) != 0 || len(endpoints) != 1 {
		t.Fatalf("OpenCode-only node B should publish one endpoint: %v %v", endpoints, problems)
	}
}

func TestOpenCodeOnlyAdapterBecomesReadyWithoutDaemonRestart(t *testing.T) {
	home := isolatedOperatorHome(t)
	t.Setenv("RA2A_DISABLE_OPENCODE", "0")
	t.Setenv("RA2A_OC_SESSION_DIR", filepath.Join(home, "leases"))
	if err := operator.Save(operator.Config{NodeID: "open-node", Name: "open-node", PIN: "A1B2C3", OpenCode: "/installed/opencode"}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	t.Setenv("RA2A_OPENCODE_URL", "http://"+address)
	registry, err := buildRegistry(context.Background(), "open-node", "", "", io.Discard,
		func(context.Context, string, string, io.Writer) (sessionSource, error) {
			return nil, errors.New("must not start Codex")
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if endpoints, _ := registry.Endpoints(context.Background()); len(endpoints) != 0 {
		t.Fatalf("stopped OpenCode must not publish sessions: %v", endpoints)
	}
	listener, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session" {
			_, _ = w.Write([]byte(`[{"id":"ses_later","title":"later"}]`))
			return
		}
		if r.URL.Path == "/event" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {}\n\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.NotFound(w, r)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	release, err := ocsession.Register(ocsession.Directory(), "ses_later")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if endpoints, problems := registry.Endpoints(context.Background()); len(problems) != 0 || len(endpoints) != 1 {
		t.Fatalf("late OpenCode server must become visible without restart: %v %v", endpoints, problems)
	}
}

// Verify the actual operator path before tests can write node identity or PIN.
func isolatedOperatorHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	path, err := operator.ConfigPath()
	want := filepath.Join(home, ".config", "ra2a", "config.json")
	if err != nil || path != want {
		t.Fatalf("operator config must stay in isolated home: got %q, want %q, err %v", path, want, err)
	}
	return home
}
