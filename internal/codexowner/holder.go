// Package codexowner reads native writer ownership without acquiring its lock.
package codexowner

import "errors"

var ErrChanged = errors.New("writer identity changed during observation")
var ErrUnsupported = errors.New("native writer ownership query is unsupported")

// Holder is an observation, not a lease or permission to write. PID alone is
// insufficient: callers must invalidate it on process or lock-file replacement.
type Holder struct {
	PID        int
	StartTicks uint64
	Executable string
	Device     uint64
	Inode      uint64
}
