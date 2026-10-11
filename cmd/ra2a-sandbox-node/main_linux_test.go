package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/control"
)

func validTestEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("RA2A_SANDBOX_HOST_NETNS", "net:[different-host]")
	t.Setenv("HOME", "/home/sandbox")
	t.Setenv("CODEX_HOME", "/home/sandbox/.codex")
	t.Setenv("RA2A_PI_SESSION_DIR", "/home/sandbox/.config/ra2a/pi-sessions")
	t.Setenv("RA2A_OC_SESSION_DIR", "/home/sandbox/.config/ra2a/opencode-sessions")
	t.Setenv("RA2A_SANDBOX_BUDGET_SOCKET", "/run/budget.sock")
}

type timedBackend struct {
	send func(context.Context, control.SendRequest) error
}

func (b timedBackend) ListTargets(context.Context) ([]control.Target, error) { return nil, nil }
func (b timedBackend) Send(ctx context.Context, request control.SendRequest) error {
	return b.send(ctx, request)
}

func TestTimedSendHandlerKeepsExactIDAndDoesNotRetry(t *testing.T) {
	calls := 0
	backend := timedBackend{send: func(ctx context.Context, request control.SendRequest) error {
		calls++
		if request.MessageID != "exact-id" || request.Text != "marker" || request.To != "ra2a://remote/receiver" || request.From != "ra2a://source/sender" {
			t.Errorf("modified request: %+v", request)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("missing bounded deadline")
		}
		<-ctx.Done()
		return fmt.Errorf("%w: SECRET native diagnostics", control.ErrDeliveryUnknown)
	}}
	request := httptest.NewRequest(http.MethodPost, "/sandbox/send-timeout", strings.NewReader(`{"messageId":"exact-id","text":"marker","to":"ra2a://remote/receiver","from":"ra2a://source/sender"}`))
	response := httptest.NewRecorder()
	timedSend(backend)(response, request)
	if response.Code != 200 || calls != 1 || strings.Contains(response.Body.String(), "SECRET") || !strings.Contains(response.Body.String(), `"error":"DELIVERY_UNKNOWN"`) {
		t.Fatalf("unsafe timeout response: calls=%d status=%d body=%s", calls, response.Code, response.Body.String())
	}
}

func TestTimedSendRejectsMissingIDAndSeparatesPrewriteFailure(t *testing.T) {
	calls := 0
	backend := timedBackend{send: func(context.Context, control.SendRequest) error { calls++; return errors.New("SECRET before write") }}
	for _, body := range []string{`{}`, `invalid`} {
		response := httptest.NewRecorder()
		timedSend(backend)(response, httptest.NewRequest("POST", "/sandbox/send-timeout", strings.NewReader(body)))
		if response.Code != 400 || calls != 0 {
			t.Fatal("invalid request executed")
		}
	}
	response := httptest.NewRecorder()
	timedSend(backend)(response, httptest.NewRequest("POST", "/sandbox/send-timeout", strings.NewReader(`{"messageId":"id"}`)))
	if calls != 1 || strings.Contains(response.Body.String(), "SECRET") || !strings.Contains(response.Body.String(), "NON_UNKNOWN_FAILURE") {
		t.Fatalf("incorrect error class: %s", response.Body.String())
	}
}

func TestTransportACKHoldIsCancellable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	awaitTransportACK(ctx, 5*time.Second)
	if time.Since(start) > time.Second {
		t.Fatal("canceled ACK wait retained worker")
	}
}

func TestIsolationRejectsHostNamespaceAndNonprivatePaths(t *testing.T) {
	for _, scenario := range []struct{ name, key, value string }{
		{"missing-host-identity", "RA2A_SANDBOX_HOST_NETNS", ""},
		{"formal-home", "HOME", "/home/real-user"},
		{"prefix-lookalike", "HOME", "/home/sandbox-unsafe"},
		{"relative-codex-home", "CODEX_HOME", ".codex"},
		{"escaped-codex-home", "CODEX_HOME", "/home/sandbox/../../tmp"},
		{"formal-leases", "RA2A_PI_SESSION_DIR", "/tmp/pi"},
		{"formal-oc-leases", "RA2A_OC_SESSION_DIR", "/tmp/oc"},
		{"arbitrary-budget-path", "RA2A_SANDBOX_BUDGET_SOCKET", "/tmp/budget.sock"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			validTestEnvironment(t)
			t.Setenv(scenario.key, scenario.value)
			if validateIsolation() == nil {
				t.Fatal("unsafe worker setup accepted")
			}
		})
	}
	t.Run("same-namespace", func(t *testing.T) {
		validTestEnvironment(t)
		current, err := os.Readlink("/proc/self/ns/net")
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("RA2A_SANDBOX_HOST_NETNS", current)
		if validateIsolation() == nil {
			t.Fatal("worker accepted host network")
		}
	})
}

func TestIsolationAcceptsDeclaredPrivateEnvironment(t *testing.T) {
	validTestEnvironment(t)
	if err := validateIsolation(); err != nil {
		t.Fatal(err)
	}
}
