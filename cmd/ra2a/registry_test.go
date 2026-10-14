package main

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/lannode"
)

func TestBuildRegistryReservesRegisteredCLIThreadsFromAppHistory(t *testing.T) {
	t.Setenv("RA2A_DISABLE_OPENCODE", "true")
	ctx := context.Background()
	threadID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	// An unavailable native executable makes the CLI adapter publish no loaded
	// endpoints; explicit ownership must still exclude its thread from App history.
	registry, err := buildRegistry(ctx, "node-a", filepath.Join(t.TempDir(), "missing-codex"), "unused.sock", io.Discard,
		fakeSourceFactory([]lannode.Session{
			{ID: threadID, Title: "CLI history", Status: "idle"},
			{ID: "desktop-thread", Title: "Desktop", Status: "idle"},
		}), []string{threadID, "invalid-cli-thread"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	endpoints, _ := registry.Endpoints(ctx)
	if len(endpoints) != 1 || endpoints[0].ID != "desktop-thread" || endpoints[0].Agent != agentbridge.AgentCodexApp {
		t.Fatalf("registered unloaded CLI must be excluded: %+v", endpoints)
	}
	if _, _, err := registry.Lookup(ctx, agentbridge.Address{NodeID: "node-a", EndpointID: threadID}); err == nil {
		t.Fatal("registered CLI must not fall back to App while unavailable")
	}
}

func TestBuildRegistryKeepsAppThreadsWithoutAcceptedCLIRegistration(t *testing.T) {
	t.Setenv("RA2A_DISABLE_OPENCODE", "true")
	ctx := context.Background()
	for _, registered := range [][]string{nil, {"desktop-thread"}} {
		registry, err := buildRegistry(ctx, "node-a", "unused-codex", "unused.sock", io.Discard,
			fakeSourceFactory([]lannode.Session{{ID: "desktop-thread", Status: "idle"}}), registered)
		if err != nil {
			t.Fatal(err)
		}
		endpoints, problems := registry.Endpoints(ctx)
		if len(problems) != 0 || len(endpoints) != 1 || endpoints[0].Agent != agentbridge.AgentCodexApp {
			t.Fatalf("unregistered or invalid registration changed Desktop behavior: endpoints=%+v problems=%v", endpoints, problems)
		}
		if err := registry.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
