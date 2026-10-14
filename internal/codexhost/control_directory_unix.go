//go:build !windows

package codexhost

import "os"

func prepareControlDirectory(path string) error {
	return os.MkdirAll(path, 0o700)
}
