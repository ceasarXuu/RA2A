package ocsession

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLeaseOnlyPublishesLiveAttachedSession(t *testing.T) {
	directory := t.TempDir()
	if Active(directory)["ses_attached"] {
		t.Fatal("unattached session must not be published")
	}
	release, err := Register(directory, "ses_attached")
	if err != nil {
		t.Fatal(err)
	}
	if !Active(directory)["ses_attached"] {
		t.Fatal("attached session must be published")
	}
	release()
	if Active(directory)["ses_attached"] {
		t.Fatal("exited TUI session must not be published")
	}
	if err := os.WriteFile(filepath.Join(directory, "ses_stale.999999"), []byte(`{"sessionID":"ses_stale","pid":999999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if Active(directory)["ses_stale"] {
		t.Fatal("dead owner process must not publish a session")
	}
}
