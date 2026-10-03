//go:build linux || darwin

package codexcli

import "os"

func createNativeStateDirectory(path string) error { return os.Mkdir(path, 0o700) }
