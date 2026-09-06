package codexapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/desktopipc"
)

// desktopSender mirrors the historical desktopMessageSender closure shape so
// the desktop-first policy stays testable without a real host or socket.
type desktopSender func(ctx context.Context, threadID, prompt string, resolve desktopipc.StartModelResolver) error

// deliverDesktop implements the desktop-first delivery policy of the
// v0.0.10-v0.0.14 App path: the Codex Desktop process owns the session
// writer, so delivery goes through Desktop IPC and never falls back to the
// managed host. When Desktop integration is disabled, delivery reports
// start_required without dialing.
func (adapter *Adapter) deliverDesktop(ctx context.Context, threadID, prompt string) (agentbridge.DeliveryResult, error) {
	return deliverWithDesktopPreference(ctx, threadID, prompt, adapter.desktopEnabled, adapter.sendDesktop)
}

// sendDesktop binds the real Desktop IPC sender with the managed host's
// thread-model resolver.
func (adapter *Adapter) sendDesktop(ctx context.Context, threadID, prompt string, _ desktopipc.StartModelResolver) error {
	return sendDesktopMessage(ctx, threadID, prompt, adapter.host.ResolveThreadModel)
}

// deliverWithDesktopPreference enforces the desktop-first policy: delivery is
// only attempted through the Desktop owner, never through a managed-host
// write fallback.
func deliverWithDesktopPreference(ctx context.Context, threadID, prompt string, enabled bool, desktop desktopSender) (agentbridge.DeliveryResult, error) {
	if !enabled {
		return agentbridge.DeliveryResult{
			Code:   agentbridge.StartRequired,
			Detail: "start Codex Desktop and retry",
		}, nil
	}
	if err := ctx.Err(); err != nil {
		return agentbridge.DeliveryResult{}, err
	}
	err := desktop(ctx, threadID, prompt, nil)
	return classifyDesktopResult(ctx, err)
}

// sendDesktopMessage dials the Codex Desktop IPC socket once per message and
// asks the Desktop owner to steer or start the turn. Thread model resolution
// is lazy: it only runs when the Desktop reports no active turn.
func sendDesktopMessage(ctx context.Context, threadID, prompt string, resolveModel desktopipc.StartModelResolver) error {
	connection, _, err := desktopipc.DialContext(ctx, "")
	if err != nil {
		return &desktopipc.NotDeliveredError{Cause: err}
	}
	defer connection.Close()
	client := desktopipc.New(connection)
	if err := client.Initialize(ctx); err != nil {
		return &desktopipc.NotDeliveredError{Cause: err}
	}
	_, err = client.SendMessage(ctx, threadID, prompt, desktopipc.NewMessageID(), resolveModel)
	return err
}

// classifyDesktopResult maps Desktop IPC outcomes onto agentbridge delivery
// codes. Context cancellation is surfaced as an error so it is never
// mislabeled as a delivery verdict.
func classifyDesktopResult(ctx context.Context, err error) (agentbridge.DeliveryResult, error) {
	switch {
	case err == nil:
		return agentbridge.DeliveryResult{Code: agentbridge.Delivered}, nil
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return agentbridge.DeliveryResult{}, ctx.Err()
	}
	var notDelivered *desktopipc.NotDeliveredError
	if errors.As(err, &notDelivered) {
		return agentbridge.DeliveryResult{
			Code:   agentbridge.StartRequired,
			Detail: fmt.Sprintf("start Codex Desktop and retry: %v", notDelivered),
		}, nil
	}
	var unknown *desktopipc.DeliveryUnknownError
	if errors.As(err, &unknown) {
		return agentbridge.DeliveryResult{Code: agentbridge.DeliveryUnknown, Detail: unknown.Error()}, nil
	}
	return agentbridge.DeliveryResult{Code: agentbridge.AdapterFailed, Detail: err.Error()}, nil
}
