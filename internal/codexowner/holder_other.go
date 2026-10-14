//go:build !linux

package codexowner

import "context"

// An unavailable platform reader must never be interpreted as a sleeping thread.
func ReadWriter(ctx context.Context, _ string) (*Holder, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrUnsupported
}
