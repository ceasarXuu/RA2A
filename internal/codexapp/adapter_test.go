package codexapp_test

import (
	"context"
	"io"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/codexapp"
)

type ownershipSource struct {
	sessions []codexapp.Session
	targets  []string
}

func (source *ownershipSource) ListSessions(context.Context) ([]codexapp.Session, error) {
	return source.sessions, nil
}

func (source *ownershipSource) SendMessage(_ context.Context, target, _ string) error {
	source.targets = append(source.targets, target)
	return nil
}

func (*ownershipSource) Close() error { return nil }

type ownershipCLI struct {
	endpoints []agentbridge.Endpoint
	targets   []string
}

func (*ownershipCLI) Kind() agentbridge.AgentKind { return agentbridge.AgentCodexCLI }

func (cli *ownershipCLI) ListEndpoints(context.Context) ([]agentbridge.Endpoint, error) {
	return cli.endpoints, nil
}

func (cli *ownershipCLI) Deliver(_ context.Context, address agentbridge.Address, _ agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	cli.targets = append(cli.targets, address.EndpointID)
	return agentbridge.Delivered("cli-turn")
}

func (*ownershipCLI) Health(context.Context) agentbridge.Health { return agentbridge.Ready() }
func (*ownershipCLI) Close() error                              { return nil }

func TestExplicitCLIOwnershipIsNotShadowedByAppHistory(t *testing.T) {
	ctx := context.Background()
	threadID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	address := agentbridge.Address{NodeID: "node-a", EndpointID: threadID}
	source := &ownershipSource{sessions: []codexapp.Session{
		{ID: threadID, Title: "CLI in shared history", Status: "idle"},
		{ID: "desktop-thread", Title: "Desktop", Status: "idle"},
	}}
	app := codexapp.New("node-a", source, io.Discard, threadID)
	cli := &ownershipCLI{endpoints: []agentbridge.Endpoint{{
		ID: threadID, NativeSessionID: threadID, Status: agentbridge.EndpointReady,
		Capabilities: []agentbridge.Capability{agentbridge.CapabilityReceiveText}, Address: address,
	}}}
	registry := agentbridge.NewRegistry("node-a")
	for _, adapter := range []agentbridge.Adapter{app, cli} {
		if err := registry.Register(adapter); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = registry.Close() })

	t.Run("loaded CLI owns lookup and delivery", func(t *testing.T) {
		endpoints, problems := registry.Endpoints(ctx)
		if len(problems) != 0 || len(endpoints) != 2 {
			t.Fatalf("expected two unambiguous endpoints: endpoints=%+v problems=%v", endpoints, problems)
		}
		endpoint, adapter, err := registry.Lookup(ctx, address)
		if err != nil || endpoint.Agent != agentbridge.AgentCodexCLI || adapter != cli {
			t.Fatalf("CLI ownership lost: endpoint=%+v adapter=%v err=%v", endpoint, adapter, err)
		}
		result := registry.Deliver(ctx, agentbridge.MessageEnvelope{
			ID: "msg-cli", ProtocolVersion: agentbridge.ProtocolVersion,
			TargetAddress: address.String(), Text: "hello CLI",
		})
		if !result.Delivered() || len(cli.targets) != 1 || cli.targets[0] != threadID || len(source.targets) != 0 {
			t.Fatalf("CLI delivery used wrong writer: result=%+v cli=%v app=%v", result, cli.targets, source.targets)
		}
	})

	t.Run("unloaded CLI never falls back to App", func(t *testing.T) {
		cli.endpoints = nil
		endpoints, problems := registry.Endpoints(ctx)
		if len(problems) != 0 || len(endpoints) != 1 || endpoints[0].ID != "desktop-thread" {
			t.Fatalf("unloaded CLI became App: endpoints=%+v problems=%v", endpoints, problems)
		}
		if _, _, err := registry.Lookup(ctx, address); err == nil {
			t.Fatal("unloaded CLI must not have a routable App endpoint")
		}
		result := registry.Deliver(ctx, agentbridge.MessageEnvelope{
			ID: "msg-unloaded", ProtocolVersion: agentbridge.ProtocolVersion,
			TargetAddress: address.String(), Text: "do not send to Desktop",
		})
		if result.Delivered() || len(source.targets) != 0 || len(cli.targets) != 1 {
			t.Fatalf("unloaded CLI reached a writer: result=%+v cli=%v app=%v", result, cli.targets, source.targets)
		}
	})

	t.Run("unregistered Desktop still receives", func(t *testing.T) {
		result := registry.Deliver(ctx, agentbridge.MessageEnvelope{
			ID: "msg-desktop", ProtocolVersion: agentbridge.ProtocolVersion,
			TargetAddress: "ra2a://node-a/desktop-thread", Text: "hello Desktop",
		})
		if !result.Delivered() || len(source.targets) != 1 || source.targets[0] != "desktop-thread" {
			t.Fatalf("Desktop delivery changed: result=%+v app=%v", result, source.targets)
		}
	})
}
