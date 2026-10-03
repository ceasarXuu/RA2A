package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

//go:embed focus.mjs
var focusPlugin string

// prepareFocus adds a client-local plugin without changing the user's config.
// An explicit override is copied beside its source, preserving relative paths.
func prepareFocus(directory string) (configPath string, release func(), err error) {
	directory, err = filepath.Abs(directory)
	if err != nil {
		return "", nil, err
	}
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return "", nil, err
	}
	temporary, err := os.MkdirTemp(directory, "focus-")
	if err != nil {
		return "", nil, err
	}
	leasePath := ocsession.FocusPath(directory)
	cleanup := func() {
		_ = os.Remove(leasePath)
		_ = os.Remove(leasePath + ".next")
		if configPath != "" {
			_ = os.Remove(configPath)
		}
		_ = os.RemoveAll(temporary)
	}
	release = cleanup
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	pathJSON, _ := json.Marshal(leasePath)
	plugin := strings.ReplaceAll(focusPlugin, "__RA2A_LEASE__", string(pathJSON))
	plugin = strings.ReplaceAll(plugin, "__RA2A_PID__", strconv.Itoa(os.Getpid()))
	pluginPath := filepath.Join(temporary, "focus.mjs")
	if err = os.WriteFile(pluginPath, []byte(plugin), 0o600); err != nil {
		return "", nil, err
	}
	config := map[string]json.RawMessage{}
	configDirectory := temporary
	if original := os.Getenv("OPENCODE_TUI_CONFIG"); original != "" {
		data, readErr := os.ReadFile(original)
		if readErr != nil {
			return "", nil, fmt.Errorf("read TUI override: %w", readErr)
		}
		if err = json.Unmarshal(jsonConfig(data), &config); err != nil {
			return "", nil, fmt.Errorf("parse TUI override: %w", err)
		}
		if config == nil {
			return "", nil, fmt.Errorf("TUI override must be an object")
		}
		configDirectory = filepath.Dir(original)
	}
	var plugins []json.RawMessage
	if raw := config["plugin"]; len(raw) != 0 {
		if err = json.Unmarshal(raw, &plugins); err != nil {
			return "", nil, fmt.Errorf("parse TUI plugins: %w", err)
		}
	}
	urlPath := filepath.ToSlash(pluginPath)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	pluginURL := (&url.URL{Scheme: "file", Path: urlPath}).String()
	entry, _ := json.Marshal(pluginURL)
	plugins = append(plugins, entry)
	config["plugin"], _ = json.Marshal(plugins)
	data, err := json.Marshal(config)
	if err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp(configDirectory, "ra2a-tui-*.json")
	if err != nil {
		return "", nil, err
	}
	configPath = file.Name()
	_, err = file.Write(data)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return configPath, release, err
}

// jsonConfig removes JSONC comments and trailing commas, preserving strings and
// offsets. Native tui.json supports both; re-encoding retains all user fields.
func jsonConfig(data []byte) []byte {
	data = append([]byte(nil), data...)
	quoted, escaped := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c != '/' || i+1 >= len(data) {
			continue
		}
		if data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				data[i] = ' '
				i++
			}
		} else if data[i+1] == '*' {
			data[i], data[i+1] = ' ', ' '
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				data[i] = ' '
				i++
			}
			if i+1 >= len(data) {
				return []byte{0}
			} // reject an unterminated comment
			if i+1 < len(data) {
				data[i], data[i+1] = ' ', ' '
				i++
			}
		}
	}
	quoted, escaped = false, false
	for i, c := range data {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(data) && strings.ContainsRune(" \t\r\n", rune(data[j])) {
				j++
			}
			if j < len(data) && (data[j] == ']' || data[j] == '}') {
				data[i] = ' '
			}
		}
	}
	return data
}
