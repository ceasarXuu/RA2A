package operator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// OpenCodeConfigPath resolves the OpenCode user config. OPENCODE_CONFIG wins so
// tests and non-default installs can point somewhere else.
func OpenCodeConfigPath() (string, error) {
	if override := os.Getenv("OPENCODE_CONFIG"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json"), nil
}

const openCodeMCPServerName = "ra2a"

type openCodeLocalServer struct {
	Type    string   `json:"type"`
	Command []string `json:"command"`
	Enabled bool     `json:"enabled"`
}

// RegisterOpenCodeMCP makes RA2A's MCP server available to OpenCode agents.
//
// It is written in product code rather than left to the operator because the
// requirement is that an OpenCode agent can send without any setup step. The edit
// is deliberately narrow: only the `mcp.ra2a` entry is touched, every other key
// is preserved, and the first modification backs the file up once so a
// hand-maintained config can always be restored.
func RegisterOpenCodeMCP(executable string) error {
	if executable == "" {
		return errors.New("opencode mcp registration needs the ra2a executable path")
	}
	path, err := OpenCodeConfigPath()
	if err != nil {
		return err
	}
	config := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &config); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		backupConfigOnce(path, raw)
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
	default:
		return err
	}

	servers, _ := config["mcp"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	// An entry the operator wrote by hand is not ours to overwrite; pointing it
	// elsewhere would silently redirect an agent's traffic.
	if existing, found := servers[openCodeMCPServerName]; found {
		if server, ok := existing.(map[string]any); ok {
			if enabled, _ := server["enabled"].(bool); enabled {
				if command, ok := server["command"].([]any); ok && sameCommand(command, executable) {
					return nil
				}
			}
		}
		return fmt.Errorf(
			"%s already defines an mcp server named %q that RA2A did not install; remove it or rename it before retrying",
			path, openCodeMCPServerName)
	}
	servers[openCodeMCPServerName] = openCodeLocalServer{
		Type: "local", Command: []string{executable, "mcp"}, Enabled: true,
	}
	config["mcp"] = servers
	return writeOpenCodeConfig(path, config)
}

// UnregisterOpenCodeMCP removes the entry RA2A installed. It is a no-op when
// the file or the entry is absent, so uninstall stays safe to run twice.
func UnregisterOpenCodeMCP() error {
	path, err := OpenCodeConfigPath()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	config := map[string]any{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	servers, _ := config["mcp"].(map[string]any)
	if servers == nil {
		return nil
	}
	entry, found := servers[openCodeMCPServerName]
	if !found {
		return nil
	}
	if server, ok := entry.(map[string]any); ok {
		if enabled, _ := server["enabled"].(bool); enabled {
			if command, ok := server["command"].([]any); ok && sameCommand(command, executablePathGuess()) {
				delete(servers, openCodeMCPServerName)
			} else {
				return fmt.Errorf("mcp server %q in %s was not installed by RA2A; leaving it alone", openCodeMCPServerName, path)
			}
		}
	}
	if len(servers) == 0 {
		delete(config, "mcp")
	} else {
		config["mcp"] = servers
	}
	return writeOpenCodeConfig(path, config)
}

func writeOpenCodeConfig(path string, config map[string]any) error {
	// Stable key order keeps the diff small and reviewable.
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".ra2a-new"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func backupConfigOnce(path string, raw []byte) {
	backup := path + ".ra2a-backup"
	if _, err := os.Stat(backup); err == nil {
		return
	}
	_ = os.WriteFile(backup, raw, 0o600)
}

func sameCommand(command []any, executable string) bool {
	if len(command) < 2 {
		return false
	}
	first, _ := command[0].(string)
	second, _ := command[1].(string)
	return second == "mcp" && (executable == "" || first == executable)
}

func executablePathGuess() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return executable
}
