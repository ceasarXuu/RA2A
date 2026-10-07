package control

import (
	"context"
	"errors"
	"fmt"
	"github.com/ceasarXuu/RA2A/internal/lannode"
	"testing"
)

func TestCoordinatorPreservesRemoteMissingEndpointAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name         string
		remote, want error
	}{
		{"missing", fmt.Errorf("%w: missing", lannode.ErrEndpointNotFound), ErrTargetNotFound},
		{"written_unknown", fmt.Errorf("%w: lost acknowledgement", ErrDeliveryUnknown), ErrDeliveryUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lan := &fakeLAN{peers: []lannode.Peer{{ID: "remote"}}, sendErr: tc.remote}
			err := NewCoordinator("local", lan).Send(context.Background(), SendRequest{To: "ra2a://remote/missing", Text: "probe", SourceSessionID: "sender"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("classification %v, want %v", err, tc.want)
			}
			if len(lan.sentPeers) != 1 {
				t.Fatalf("must not replay: %d sends", len(lan.sentPeers))
			}
		})
	}
}
