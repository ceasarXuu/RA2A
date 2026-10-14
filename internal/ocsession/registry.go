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
	"time"
)

type Lease struct {
	SessionID string `json:"sessionID"`
	PID       int    `json:"pid"`
	// Expires is set by the client-local TUI plugin, in Unix milliseconds.
	// Legacy fixed-session wrappers omit it.
	Expires int64 `json:"expires,omitempty"`
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
		if json.Unmarshal(data, &lease) != nil || !validLease(lease, entry.Name()) || !processAlive(lease.PID) {
			continue
		}
		active[lease.SessionID] = true
	}
	return active
}

// FocusPath is private to one wrapper process; a TUI switch replaces this file
// atomically instead of briefly publishing both the old and new sessions.
func FocusPath(directory string) string {
	return filepath.Join(directory, fmt.Sprintf("attachment.%d", os.Getpid()))
}

func Current(directory string) string {
	path := FocusPath(directory)
	data, err := os.ReadFile(path)
	var lease Lease
	if err != nil || json.Unmarshal(data, &lease) != nil || !validLease(lease, filepath.Base(path)) {
		return ""
	}
	return lease.SessionID
}

func validLease(lease Lease, name string) bool {
	if lease.PID <= 0 || !strings.HasPrefix(lease.SessionID, "ses") || strings.ContainsAny(lease.SessionID, `/\\`) {
		return false
	}
	if name == fmt.Sprintf("attachment.%d", lease.PID) {
		return lease.Expires > time.Now().UnixMilli()
	}
	return lease.Expires == 0 && name == fmt.Sprintf("%s.%d", lease.SessionID, lease.PID)
}
