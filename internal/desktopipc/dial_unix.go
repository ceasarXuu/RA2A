//go:build !windows

package desktopipc

import (
	"context"
	"net"
)

func dialPlatform(ctx context.Context, socketPath string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
}
