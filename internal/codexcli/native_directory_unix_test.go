//go:build linux || darwin

package codexcli

import (
	"os"
	"path/filepath"
)

func createNativeStateDirectory(path string) error    { return os.Mkdir(path, 0o700) }
func canonicalNativePath(path string) (string, error) { return filepath.EvalSymlinks(path) }
func verifyNativeProcess(int, string, string) error   { return nil }
