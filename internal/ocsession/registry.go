// Package ocsession records OpenCode sessions currently attached to the shared
// server. A session in OpenCode's global store is not necessarily executed by
// that server: only an active RA2A attachment establishes that ownership.
package ocsession

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Lease struct {
	SessionID string `json:"sessionID"`
	PID       int    `json:"pid"`
}

func Directory() string {
	if value := os.Getenv("RA2A_OC_SESSION_DIR"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ra2a", "opencode-sessions")
}

func Register(directory, sessionID string) (func(), error) {
	if directory == "" || !strings.HasPrefix(sessionID, "ses") || strings.ContainsAny(sessionID, `/\`) {
		return nil, fmt.Errorf("invalid OpenCode session lease %q", sessionID)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	pid := os.Getpid()
	path := filepath.Join(directory, fmt.Sprintf("%s.%d", sessionID, pid))
	data, err := json.Marshal(Lease{SessionID: sessionID, PID: pid})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

// Active fails closed if a lease is absent or stale. The wrapper removes its
// lease on exit; PID validation covers crashes that bypass cleanup.
func Active(directory string) map[string]bool {
	active := make(map[string]bool)
	if directory == "" {
		return active
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return active
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		var lease Lease
		if json.Unmarshal(data, &lease) != nil || lease.PID <= 0 ||
			fmt.Sprintf("%s.%d", lease.SessionID, lease.PID) != entry.Name() || !processAlive(lease.PID) {
			continue
		}
		active[lease.SessionID] = true
	}
	return active
}
