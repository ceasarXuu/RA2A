package codexhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ownerRecord is the small persistent lease for the App Server process started
// by RA2A. It lets a later daemon instance reclaim a process left behind by an
// abnormal exit without ever attaching to an arbitrary existing socket.
type ownerRecord struct {
	PID        int    `json:"pid"`
	SocketPath string `json:"socketPath"`
	CodexPath  string `json:"codexPath,omitempty"`
}

func prepareConfig(config Config) (Config, error) {
	baseSocketPath := config.SocketPath
	if config.OwnerPath == "" {
		config.OwnerPath = ownerRecordPath(baseSocketPath)
	}
	if err := cleanupOwnerRecord(config.OwnerPath); err != nil {
		return Config{}, fmt.Errorf("cleanup stale managed Codex host: %w", err)
	}
	config.SocketPath = uniqueSocketPath(baseSocketPath, os.Getpid())
	return config, nil
}

func uniqueSocketPath(base string, pid int) string {
	return fmt.Sprintf("%s.ra2a-%d.sock", base, pid)
}

func ownerRecordPath(socketPath string) string {
	return socketPath + ".ra2a-owner.json"
}

func writeOwnerRecord(path string, record ownerRecord) error {
	if record.PID <= 0 || record.SocketPath == "" {
		return errors.New("invalid managed Codex host owner record")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temporary := path + ".new"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func readOwnerRecord(path string) (ownerRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ownerRecord{}, err
	}
	var record ownerRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ownerRecord{}, fmt.Errorf("decode owner record: %w", err)
	}
	if record.PID <= 0 || record.SocketPath == "" {
		return ownerRecord{}, errors.New("invalid managed Codex host owner record")
	}
	return record, nil
}

func cleanupOwnerRecord(path string) error {
	record, err := readOwnerRecord(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		// A corrupt lease must not prevent recovery. The socket is never reused;
		// remove only the record and let the next launch allocate a new one.
		return removeOwnerArtifacts(path, "")
	}
	if process, findErr := os.FindProcess(record.PID); findErr == nil {
		if err := killManagedPID(record.PID); err != nil {
			_ = process.Kill()
		}
	}
	return removeOwnerArtifacts(path, record.SocketPath)
}

func clearOwnerRecord(path string, pid int, socketPath string) error {
	if path == "" {
		return nil
	}
	record, err := readOwnerRecord(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return removeOwnerArtifacts(path, "")
	}
	if pid > 0 && record.PID != pid {
		return nil
	}
	if socketPath != "" && record.SocketPath != socketPath {
		return nil
	}
	return removeOwnerArtifacts(path, record.SocketPath)
}

func removeOwnerArtifacts(ownerPath, socketPath string) error {
	if socketPath != "" {
		if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(ownerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// CleanupManaged reclaims the process recorded for the supplied base socket.
// It is used by `ra2a stop`/`ra2a exit` when the daemon itself is not alive.
func CleanupManaged(baseSocketPath string) error {
	if baseSocketPath == "" {
		return nil
	}
	return cleanupOwnerRecord(ownerRecordPath(baseSocketPath))
}

// DefaultSocketPath returns the stable base path used to derive RA2A's
// per-launch App Server socket and owner lease.
func DefaultSocketPath() string {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".codex", "app-server-control", "app-server-control.sock")
		}
		codexHome = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(codexHome, "app-server-control", "app-server-control.sock")
}
