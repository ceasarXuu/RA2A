package pi

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

func ExtensionPath() (string, error) {
	directory := os.Getenv("PI_CODING_AGENT_DIR")
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		directory = filepath.Join(home, ".pi", "agent")
	}
	return filepath.Join(directory, "extensions", "ra2a.mjs"), nil
}

func Install() (string, error) {
	target, err := ExtensionPath()
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	previous, err := os.ReadFile(target)
	if err == nil && !bytes.HasPrefix(previous, []byte("// RA2A Pi bridge:")) {
		return "", fmt.Errorf("refuse to replace unowned Pi extension %s", target)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if bytes.Equal(previous, Extension) {
		return target, nil
	}
	temporary, err := os.CreateTemp(directory, ".ra2a-*.mjs.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(temporary.Name())
	_, err = temporary.Write(Extension)
	closeErr := temporary.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = os.Rename(temporary.Name(), target); err != nil {
		return "", err
	}
	return target, nil
}

// Uninstall only removes the dedicated product-owned extension.
func Uninstall() error {
	target, err := ExtensionPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte("// RA2A Pi bridge:")) {
		return fmt.Errorf("refuse to remove unowned Pi extension %s", target)
	}
	return os.Remove(target)
}
