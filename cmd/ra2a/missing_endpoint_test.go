package main

import (
	"context"
	"errors"
	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/control"
	"github.com/ceasarXuu/RA2A/internal/lannode"
	"testing"
	"time"
)

func TestMissingLANEndpointIsNotDeliveryUnknown(t *testing.T) {
	err := deliverOverLAN(context.Background(), "local", agentbridge.NewRegistry("local"), nil,
		lannode.Message{TargetSessionID: "missing", Text: "probe"})
	if !errors.Is(err, control.ErrTargetNotFound) || errors.Is(err, control.ErrDeliveryUnknown) {
		t.Fatalf("missing endpoint classification: %v", err)
	}
}

func TestMissingEndpointClassificationSurvivesDTLSCoAP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	registry := agentbridge.NewRegistry("fixture")
	node, err := lannode.Start(ctx, lannode.Config{
		ID: "missing-endpoint-fixture", Name: "Missing endpoint fixture", PIN: "123456",
		SendMessage: func(ctx context.Context, msg lannode.Message) error {
			return deliverOverLAN(ctx, "fixture", registry, nil, msg)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	peer, err := node.WaitForPeer(ctx, "missing-endpoint-fixture")
	if err != nil {
		t.Fatal(err)
	}
	err = node.SendMessage(ctx, peer, lannode.Message{TargetSessionID: "missing", Text: "probe"})
	if !errors.Is(err, lannode.ErrEndpointNotFound) {
		t.Fatalf("remote missing verdict: %v", err)
	}
}
