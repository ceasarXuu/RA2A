package operator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOpenCodeConfigFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readMCPEntry(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode: %v", err)
	}
	servers, _ := config["mcp"].(map[string]any)
	if servers == nil {
		return nil
	}
	entry, _ := servers["ra2a"].(map[string]any)
	return entry
}

const fixtureConfig = `{
  "permission": {"websearch": "allow"},
  "mcp": {"heroui-react": {"type": "local", "command": ["npx", "-y", "@heroui/react-mcp@latest"], "enabled": true}},
  "provider": {"ollama": {"name": "Ollama"}}
}`

func TestRegisterOpenCodeMCPCreatesEntryAndKeepsEverythingElse(t *testing.T) {
	path := writeOpenCodeConfigFixture(t, fixtureConfig)
	t.Setenv("OPENCODE_CONFIG", path)
	if err := RegisterOpenCodeMCP("/home/u/.local/bin/ra2a"); err != nil {
		t.Fatalf("register: %v", err)
	}
	entry := readMCPEntry(t, path)
	if entry == nil {
		t.Fatal("the ra2a entry must exist")
	}
	command, _ := entry["command"].([]any)
	if len(command) != 2 || command[0] != "/home/u/.local/bin/ra2a" || command[1] != "mcp" {
		t.Fatalf("unexpected command: %v", command)
	}
	if entry["type"] != "local" || entry["enabled"] != true {
		t.Fatalf("unexpected entry: %v", entry)
	}
	raw, _ := os.ReadFile(path)
	var config map[string]any
	_ = json.Unmarshal(raw, &config)
	if config["provider"] == nil || config["permission"] == nil {
		t.Fatalf("unrelated keys must survive: %s", raw)
	}
	servers := config["mcp"].(map[string]any)
	if _, ok := servers["heroui-react"]; !ok {
		t.Fatalf("the operator's other MCP server must survive: %s", raw)
	}
	if _, err := os.Stat(path + ".ra2a-backup"); err != nil {
		t.Fatalf("the first modification must leave a backup: %v", err)
	}
}

func TestRegisterOpenCodeMCPIsIdempotent(t *testing.T) {
	path := writeOpenCodeConfigFixture(t, fixtureConfig)
	t.Setenv("OPENCODE_CONFIG", path)
	executable := "/home/u/.local/bin/ra2a"
	if err := RegisterOpenCodeMCP(executable); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	for i := 0; i < 3; i++ {
		if err := RegisterOpenCodeMCP(executable); err != nil {
			t.Fatalf("repeat register %d: %v", i, err)
		}
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("repeat registration must not rewrite the file:\n%s\n---\n%s", first, second)
	}
}

// An entry the operator wrote by hand is not RA2A's to redirect.
func TestRegisterOpenCodeMCPRefusesToOverwriteAForeignEntry(t *testing.T) {
	foreign := `{"mcp": {"ra2a": {"type": "local", "command": ["/somewhere/else", "mcp"], "enabled": true}}}`
	path := writeOpenCodeConfigFixture(t, foreign)
	t.Setenv("OPENCODE_CONFIG", path)
	err := RegisterOpenCodeMCP("/home/u/.local/bin/ra2a")
	if err == nil {
		t.Fatal("a foreign ra2a entry must not be overwritten")
	}
	if !strings.Contains(err.Error(), "did not install") {
		t.Fatalf("the refusal must explain itself, got %v", err)
	}
	if entry := readMCPEntry(t, path); entry == nil {
		t.Fatal("the foreign entry must be left intact")
	}
}

func TestRegisterOpenCodeMCPCreatesTheFileWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "opencode.json")
	t.Setenv("OPENCODE_CONFIG", path)
	if err := RegisterOpenCodeMCP("/home/u/.local/bin/ra2a"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if entry := readMCPEntry(t, path); entry == nil {
		t.Fatal("the entry must be created in a fresh config")
	}
}

func TestUnregisterOpenCodeMCPRemovesOnlyOurEntry(t *testing.T) {
	path := writeOpenCodeConfigFixture(t, fixtureConfig)
	t.Setenv("OPENCODE_CONFIG", path)
	executable := "/home/u/.local/bin/ra2a"
	if err := RegisterOpenCodeMCP(executable); err != nil {
		t.Fatal(err)
	}
	// Point the entry at this test binary so unregister recognises it as ours.
	raw, _ := os.ReadFile(path)
	var config map[string]any
	_ = json.Unmarshal(raw, &config)
	servers := config["mcp"].(map[string]any)
	servers["ra2a"] = openCodeLocalServer{Type: "local",
		Command: []string{executablePathGuess(), "mcp"}, Enabled: true}
	_ = writeOpenCodeConfig(path, config)

	if err := UnregisterOpenCodeMCP(); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	entry := readMCPEntry(t, path)
	if entry != nil {
		t.Fatalf("our entry must be removed, got %v", entry)
	}
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), "heroui-react") {
		t.Fatalf("other MCP servers must survive unregister: %s", raw)
	}
}

func TestUnregisterIsSafeWhenNothingIsInstalled(t *testing.T) {
	path := writeOpenCodeConfigFixture(t, fixtureConfig)
	t.Setenv("OPENCODE_CONFIG", path)
	if err := UnregisterOpenCodeMCP(); err != nil {
		t.Fatalf("unregister with no entry must be a no-op, got %v", err)
	}
	missing := filepath.Join(t.TempDir(), "absent.json")
	t.Setenv("OPENCODE_CONFIG", missing)
	if err := UnregisterOpenCodeMCP(); err != nil {
		t.Fatalf("unregister with no file must be a no-op, got %v", err)
	}
}

func TestRegisterRejectsAnEmptyExecutable(t *testing.T) {
	path := writeOpenCodeConfigFixture(t, fixtureConfig)
	t.Setenv("OPENCODE_CONFIG", path)
	if err := RegisterOpenCodeMCP(""); err == nil {
		t.Fatal("an empty executable must be rejected")
	}
}
