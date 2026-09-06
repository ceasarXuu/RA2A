package codexapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/desktopipc"
)

func recordedSender(err error) (desktopSender, *int) {
	calls := 0
	sender := func(context.Context, string, string, desktopipc.StartModelResolver) error {
		calls++
		return err
	}
	return sender, &calls
}

func TestDeliverWithDesktopPreferenceDisabledReportsStartRequired(t *testing.T) {
	sender, calls := recordedSender(nil)
	result, err := deliverWithDesktopPreference(context.Background(), "t", "hello", false, sender)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != agentbridge.StartRequired || !strings.Contains(result.Detail, "start Codex Desktop") {
		t.Fatalf("result = %#v", result)
	}
	if *calls != 0 {
		t.Fatalf("desktop sender called %d times while disabled", *calls)
	}
}

func TestDeliverWithDesktopPreferenceDelivered(t *testing.T) {
	sender, calls := recordedSender(nil)
	result, err := deliverWithDesktopPreference(context.Background(), "t", "hello", true, sender)
	if err != nil || result.Code != agentbridge.Delivered {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if *calls != 1 {
		t.Fatalf("desktop sender called %d times, want 1", *calls)
	}
}

func TestDeliverWithDesktopPreferenceMapsNotDeliveredToStartRequired(t *testing.T) {
	notDelivered := &desktopipc.NotDeliveredError{Cause: errors.New("no Desktop owner")}
	sender, calls := recordedSender(notDelivered)
	result, err := deliverWithDesktopPreference(context.Background(), "t", "hello", true, sender)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != agentbridge.StartRequired || !strings.Contains(result.Detail, "start Codex Desktop") {
		t.Fatalf("result = %#v", result)
	}
	if *calls != 1 {
		t.Fatalf("desktop sender called %d times, want 1 (no managed fallback)", *calls)
	}
}

func TestDeliverWithDesktopPreferenceDoesNotDuplicateUnknownDelivery(t *testing.T) {
	unknown := &desktopipc.DeliveryUnknownError{Cause: context.DeadlineExceeded}
	sender, calls := recordedSender(unknown)
	result, err := deliverWithDesktopPreference(context.Background(), "t", "hello", true, sender)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != agentbridge.DeliveryUnknown {
		t.Fatalf("code = %q, want unknown", result.Code)
	}
	if *calls != 1 {
		t.Fatalf("desktop sender called %d times, want 1", *calls)
	}
}

func TestDeliverWithDesktopPreferenceSurfacesUnclassifiedError(t *testing.T) {
	want := errors.New("unexpected Desktop failure")
	sender, calls := recordedSender(want)
	result, err := deliverWithDesktopPreference(context.Background(), "t", "hello", true, sender)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != agentbridge.AdapterFailed {
		t.Fatalf("code = %q, want adapter_failed", result.Code)
	}
	if *calls != 1 {
		t.Fatalf("desktop sender called %d times, want 1", *calls)
	}
}

func TestDeliverWithDesktopPreferenceSurfacesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sender, calls := recordedSender(nil)
	result, err := deliverWithDesktopPreference(ctx, "t", "hello", true, sender)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if result.Code != "" {
		t.Fatalf("result = %#v, want empty on cancellation", result)
	}
	if *calls != 0 {
		t.Fatalf("desktop sender called %d times on cancelled context", *calls)
	}
}

func TestClassifyDesktopResult(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil is delivered", want: agentbridge.Delivered},
		{name: "not delivered maps to start required", err: &desktopipc.NotDeliveredError{Cause: errors.New("no socket")}, want: agentbridge.StartRequired},
		{name: "unknown delivery maps to unknown", err: &desktopipc.DeliveryUnknownError{Cause: errors.New("timeout")}, want: agentbridge.DeliveryUnknown},
		{name: "unclassified error maps to adapter failed", err: errors.New("boom"), want: agentbridge.AdapterFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := classifyDesktopResult(context.Background(), test.err)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if result.Code != test.want {
				t.Fatalf("code = %q, want %q (detail %q)", result.Code, test.want, result.Detail)
			}
		})
	}
}

func TestClassifyDesktopResultSurfacesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := classifyDesktopResult(ctx, context.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if result.Code != "" {
		t.Fatalf("result must stay empty on cancellation, got %#v", result)
	}
}
