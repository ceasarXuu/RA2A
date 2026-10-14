package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

func TestUnattachedSessionIsNotPublished(t *testing.T) {
	fake := newFakeOpenCode(t)
	// The fixture has two sessions, but only one belongs to an attached TUI.
	entries, err := os.ReadDir(ocsession.Directory())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "ses_2.") {
			if err := os.Remove(filepath.Join(ocsession.Directory(), entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	adapter := New("node", Config{BaseURL: fake.server.URL}, nil)
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil || len(endpoints) != 1 || endpoints[0].ID != "ses_1" {
		t.Fatalf("only the attached session can be published: %+v, %v", endpoints, err)
	}
	envelope := agentbridge.MessageEnvelope{
		ID: "msg_ownership", ProtocolVersion: agentbridge.ProtocolVersion,
		SourceAddress: "ra2a://elsewhere/ses_sender", TargetAddress: "ra2a://node/ses_2",
		Text: "hello", CreatedAt: time.Now(),
	}
	result := adapter.Deliver(context.Background(), agentbridge.Address{NodeID: "node", EndpointID: "ses_2"}, envelope)
	if result.Code != agentbridge.ResultNotFound {
		t.Fatalf("unattached session must not acknowledge a queued message: %+v", result)
	}
}
