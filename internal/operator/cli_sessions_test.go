package operator

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

func seedConfig(t *testing.T) {
	t.Helper()
	if err := Save(Config{NodeID: "node-a", Name: "node-a", PIN: "A1B2C3", Codex: "codex"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func TestAdoptAndReleaseCLISession(t *testing.T) {
	withTempHome(t)
	seedConfig(t)
	threadID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

	config, err := AdoptCLISession(threadID)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if len(config.CLISessions) != 1 || config.CLISessions[0] != threadID {
		t.Fatalf("adopt must record the thread, got %+v", config.CLISessions)
	}
	if _, err := AdoptCLISession(threadID); err != nil {
		t.Fatalf("repeat adopt must stay idempotent: %v", err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(reloaded.CLISessions) != 1 {
		t.Fatalf("repeat adopt must not duplicate, got %+v", reloaded.CLISessions)
	}

	if _, err := ReleaseCLISession(threadID); err != nil {
		t.Fatalf("release: %v", err)
	}
	reloaded, err = Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(reloaded.CLISessions) != 0 {
		t.Fatalf("release must drop the thread, got %+v", reloaded.CLISessions)
	}
}

func TestAdoptSortsAndPreservesLegacyConfig(t *testing.T) {
	withTempHome(t)
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"nodeId":"node-a","name":"node-a","pin":"A1B2C3","codex":"codex"}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptCLISession("bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"); err != nil {
		t.Fatalf("adopt on legacy config: %v", err)
	}
	config, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if config.Codex != "codex" || config.PIN != "A1B2C3" {
		t.Fatalf("legacy fields must be preserved, got %+v", config)
	}
	if _, err := AdoptCLISession("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"); err != nil {
		t.Fatal(err)
	}
	config, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.CLISessions) != 2 ||
		config.CLISessions[0] != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" {
		t.Fatalf("adopted sessions must be sorted for stable output, got %+v", config.CLISessions)
	}
}
