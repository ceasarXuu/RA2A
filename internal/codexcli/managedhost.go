package codexcli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// CheckManagedHost verifies that the RA2A-managed app-server behind socketPath
// can actually serve a TUI that the wrapper is about to point at it.
//
// Injection used to be gated on socket liveness alone. A managed host runs with
// the RA2A service environment, which can differ from the caller's shell
// environment (proxy settings above all); a session captured into such a host
// cannot reach the account backend even though every local check passes. The
// gate therefore requires the host to report the caller's Codex home and to
// complete an authenticated account read, which exercises credentials and
// backend reachability in one round trip.
//
// The probe connects with adapterClientName for the same reason the adapter
// does: initialize makes that name the app-server's process-wide default thread
// originator (first writer wins), and sharing the identity keeps the side
// effect single-sourced.
func CheckManagedHost(ctx context.Context, socketPath, codexHome string) error {
	if socketPath == "" {
		return errors.New("managed app-server socket is empty")
	}
	handshake := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < handshake {
			handshake = remaining
		}
	}
	conn, err := dialRPC(ctx, socketPath, handshake)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	result, err := conn.initialize(ctx, adapterClientName, "0.0.0", false)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	hostHome := normalizeCodexHome(result.CodexHome)
	callerHome := normalizeCodexHome(codexHome)
	if hostHome != "" && callerHome != "" && hostHome != callerHome {
		return fmt.Errorf("host codex home %s differs from caller home %s", hostHome, callerHome)
	}
	if err := conn.call(ctx, "account/rateLimits/read", map[string]any{}, nil); err != nil {
		return fmt.Errorf("read account: %w", err)
	}
	return nil
}

func normalizeCodexHome(home string) string {
	if home == "" {
		return ""
	}
	cleaned := filepath.Clean(home)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil && resolved != "" {
		return resolved
	}
	return cleaned
}
