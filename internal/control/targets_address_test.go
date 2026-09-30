package control

import (
	"testing"

	"github.com/ceasarXuu/RA2A/internal/lannode"
)

func TestTargetListingSuppliesOpaqueAddressesForMixedVersionPeers(t *testing.T) {
	targets := sortedTargets(map[string]Target{
		"older": {ID: "older", Sessions: []lannode.Session{{ID: "ses_old"}}},
		"newer": {ID: "newer", Sessions: []lannode.Session{{ID: "ses_new", Address: "ra2a://newer/ses_new"}}},
	})
	if targets[0].Sessions[0].Address != "ra2a://newer/ses_new" ||
		targets[1].Sessions[0].Address != "ra2a://older/ses_old" {
		t.Fatalf("agents must not have to construct their own addresses: %+v", targets)
	}
}
