package ocsession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFocusConfigPreservesExplicitJSONCOverride(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "tui.jsonc")
	content := `{
	  // Existing theme, bindings, relative plugins and options remain intact.
	  "theme": "user-theme",
	  "keybinds": {"session_list": "ctrl+r",},
	  "plugin": ["./user-plugin.mjs", ["pkg://example", {"url": "http://x/*literal*/",}],],
	  "attention": {"sounds": {"done": "./done.wav"}},
	}`
	if err := os.WriteFile(original, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_TUI_CONFIG", original)
	leases := filepath.Join(dir, "leases with space")
	configPath, release, err := PrepareFocus(leases)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if filepath.Dir(configPath) != filepath.Dir(original) {
		t.Fatal("relative config paths changed base directory")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	var plugins []json.RawMessage
	if err := json.Unmarshal(config["plugin"], &plugins); err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 3 || string(config["theme"]) != `"user-theme"` ||
		!strings.Contains(string(plugins[1]), "http://x/*literal*/") ||
		!strings.Contains(string(config["attention"]), "./done.wav") {
		t.Fatalf("lost user configuration: %s", raw)
	}
	var pluginURL string
	_ = json.Unmarshal(plugins[2], &pluginURL)
	if !strings.HasPrefix(pluginURL, "file:///") || !strings.Contains(pluginURL, "leases%20with%20space") {
		t.Fatalf("bad file URL: %s", pluginURL)
	}
	unchanged, _ := os.ReadFile(original)
	if string(unchanged) != content {
		t.Fatal("formal override changed")
	}
	release()
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("temporary config remains: %v", err)
	}
	unchanged, _ = os.ReadFile(original)
	if string(unchanged) != content {
		t.Fatal("cleanup changed original")
	}
}

func TestFocusConfigFailureKeepsOriginalAndRemovesOnlyTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(original, []byte(`{"plugin":`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_TUI_CONFIG", original)
	leases := filepath.Join(dir, "leases")
	_, _, err := PrepareFocus(leases)
	if err == nil {
		t.Fatal("invalid override silently replaced")
	}
	entries, _ := os.ReadDir(leases)
	if len(entries) != 0 {
		t.Fatal("failed preparation left temporary plugin/config")
	}
	raw, _ := os.ReadFile(original)
	if string(raw) != `{"plugin":` {
		t.Fatal("failed preparation changed original")
	}
}
