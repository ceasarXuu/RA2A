//go:build windows

package desktopipc

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func dialPlatform(ctx context.Context, pipePath string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipePath)
}
