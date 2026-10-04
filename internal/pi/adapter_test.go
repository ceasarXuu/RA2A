package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

const testSession = "pi.12345678-1234-1234-1234-123456789abc"

func isolatedPi(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, "pi-agent"))
	directory := filepath.Join(home, "sessions")
	t.Setenv("RA2A_PI_SESSION_DIR", directory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func liveLease(url string) lease {
	return lease{SessionID: testSession, PID: 123, Expires: time.Now().Add(5 * time.Second).UnixMilli(), URL: url,
		Token: strings.Repeat("a", 64), Status: agentbridge.EndpointReady, Title: "Pi test"}
}

func writeLease(t *testing.T, directory string, value lease) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("attachment.%d.json", value.PID)), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testEnvelope() agentbridge.MessageEnvelope {
	return agentbridge.MessageEnvelope{ID: "message-123", ProtocolVersion: agentbridge.ProtocolVersion,
		SourceAddress: "ra2a://sender/source", TargetAddress: "ra2a://local/" + testSession, Text: "hello", CreatedAt: time.Now()}
}

func deliverTest(ctx context.Context, adapter *Adapter) agentbridge.DeliveryResult {
	return adapter.Deliver(ctx, agentbridge.Address{NodeID: "local", EndpointID: testSession}, testEnvelope())
}

func TestDeliverRequiresExactReceiptAndNeverReplays(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		want       agentbridge.ResultCode
	}{
		{"exact", `{"status":"received_by_bridge","id":"message-123","sessionID":"` + testSession + `"}`, 200, agentbridge.ResultDelivered},
		{"wrong id", `{"status":"received_by_bridge","id":"other","sessionID":"` + testSession + `"}`, 200, agentbridge.ResultUnknown},
		{"wrong session", `{"status":"received_by_bridge","id":"message-123","sessionID":"pi.other"}`, 200, agentbridge.ResultUnknown},
		{"wrong status", `{"status":"queued","id":"message-123","sessionID":"` + testSession + `"}`, 200, agentbridge.ResultUnknown},
		{"no ack", "", 200, agentbridge.ResultUnknown},
		{"ambiguous ack", `{"status":"received_by_bridge","id":"message-123","sessionID":"` + testSession + `"} {"error":"contradiction"}`, 200, agentbridge.ResultUnknown},
		{"invalid JSON", "{", 200, agentbridge.ResultUnknown},
		{"HTTP error", `{"status":"received_by_bridge","id":"message-123","sessionID":"` + testSession + `"}`, 503, agentbridge.ResultUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory := isolatedPi(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/receive" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+strings.Repeat("a", 64) {
					t.Errorf("authorization = %q", got)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("content type = %q", got)
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["id"] != "message-123" || body["sessionID"] != testSession || body["text"] != agentbridge.RenderIncomingText(testEnvelope()) {
					t.Errorf("wrong payload: %#v", body)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			writeLease(t, directory, liveLease(server.URL))
			if got := deliverTest(context.Background(), New("local", directory)); got.Code != tc.want {
				t.Fatalf("result = %+v, want %s", got, tc.want)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("requests = %d; delivery must not replay", got)
			}
		})
	}
}

func TestDeliverCancellationAndLostResponseRemainUnknown(t *testing.T) {
	for _, mode := range []string{"already cancelled", "cancel after receive", "lost response"} {
		t.Run(mode, func(t *testing.T) {
			directory := isolatedPi(t)
			var calls atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "cancel after receive" {
					cancel()
					return
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer server.Close()
			writeLease(t, directory, liveLease(server.URL))
			if mode == "already cancelled" {
				cancel()
			}
			if got := deliverTest(ctx, New("local", directory)); got.Code != agentbridge.ResultUnknown {
				t.Fatalf("result = %+v", got)
			}
			wantCalls := int32(1)
			if mode == "already cancelled" {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatalf("requests = %d, want %d", calls.Load(), wantCalls)
			}
		})
	}
}

func TestInvalidLeasesAreNeitherListedNorSent(t *testing.T) {
	changes := map[string]func(*lease){
		"expired":           func(l *lease) { l.Expires = time.Now().Add(-time.Second).UnixMilli() },
		"excessive TTL":     func(l *lease) { l.Expires = time.Now().Add(time.Minute).UnixMilli() },
		"non loopback":      func(l *lease) { l.URL = "http://192.0.2.1:9999" },
		"localhost name":    func(l *lease) { l.URL = "http://localhost:9999" },
		"URL userinfo":      func(l *lease) { l.URL = "http://user@127.0.0.1:9999" },
		"URL path":          func(l *lease) { l.URL += "/unexpected" },
		"URL query":         func(l *lease) { l.URL += "?x=1" },
		"URL fragment":      func(l *lease) { l.URL += "#x" },
		"malformed URL":     func(l *lease) { l.URL = "http://[" },
		"missing token":     func(l *lease) { l.Token = "" },
		"unknown status":    func(l *lease) { l.Status = agentbridge.EndpointUnknown },
		"malformed session": func(l *lease) { l.SessionID = "pi.invalid" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			directory := isolatedPi(t)
			adapter := New("local", directory)
			var calls atomic.Int32
			adapter.client.Transport = countingTransport{calls: &calls}
			value := liveLease("http://127.0.0.1:9999")
			change(&value)
			writeLease(t, directory, value)
			endpoints, err := adapter.ListEndpoints(context.Background())
			if err != nil || len(endpoints) != 0 {
				t.Fatalf("endpoints = %+v, error = %v", endpoints, err)
			}
			if got := deliverTest(context.Background(), adapter); got.Code != agentbridge.ResultNotFound {
				t.Fatalf("result = %+v", got)
			}
			if calls.Load() != 0 {
				t.Fatalf("invalid lease caused %d requests", calls.Load())
			}
		})
	}
}

type countingTransport struct{ calls *atomic.Int32 }

func (c countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return nil, fmt.Errorf("unexpected network request")
}

func TestDuplicateSessionOwnershipRejected(t *testing.T) {
	directory := isolatedPi(t)
	value := liveLease("http://127.0.0.1:9999")
	writeLease(t, directory, value)
	value.PID++
	writeLease(t, directory, value)
	adapter := New("local", directory)
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil || len(endpoints) != 0 {
		t.Fatalf("ambiguous session listed: %+v, %v", endpoints, err)
	}
	if got := deliverTest(context.Background(), adapter); got.Code != agentbridge.ResultNotFound {
		t.Fatalf("result = %+v", got)
	}
}

func TestRedirectDoesNotForwardTokenOrDelivery(t *testing.T) {
	directory := isolatedPi(t)
	var forwarded, initial atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		initial.Add(1)
		http.Redirect(w, r, target.URL+"/receive", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	writeLease(t, directory, liveLease(server.URL))
	if got := deliverTest(context.Background(), New("local", directory)); got.Code != agentbridge.ResultUnknown {
		t.Fatalf("result = %+v", got)
	}
	if initial.Load() != 1 || forwarded.Load() != 0 {
		t.Fatalf("initial = %d, forwarded = %d", initial.Load(), forwarded.Load())
	}
}

func TestPiEndpointsRouteThroughRegistry(t *testing.T) {
	directory := isolatedPi(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "received_by_bridge", "id": "message-123", "sessionID": testSession})
	}))
	defer server.Close()
	value := liveLease(server.URL)
	value.Status = agentbridge.EndpointBusy
	writeLease(t, directory, value)
	adapter := New("local", directory)
	registry := agentbridge.NewRegistry("local")
	if err := registry.Register(adapter); err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	endpoints, problems := registry.Endpoints(context.Background())
	if len(problems) != 0 || len(endpoints) != 1 {
		t.Fatalf("endpoints = %+v, problems = %v", endpoints, problems)
	}
	endpoint := endpoints[0]
	if endpoint.Agent != agentbridge.AgentPi || endpoint.NativeSessionID != strings.TrimPrefix(testSession, "pi.") || endpoint.Title != value.Title || endpoint.Status != agentbridge.EndpointBusy || !endpoint.Has(agentbridge.CapabilityReceiveText) || !endpoint.Has(agentbridge.CapabilityReplyAddress) {
		t.Fatalf("wrong endpoint: %+v", endpoint)
	}
	_, owner, err := registry.Lookup(context.Background(), endpoint.Address)
	if err != nil || owner != adapter {
		t.Fatalf("owner = %v, error = %v", owner, err)
	}
	if got := registry.Deliver(context.Background(), testEnvelope()); got.Code != agentbridge.ResultDelivered {
		t.Fatalf("result = %+v", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("requests = %d", calls.Load())
	}
}

func TestDirectoryUsesIsolatedEnvironment(t *testing.T) {
	directory := isolatedPi(t)
	if got := Directory(); got != directory {
		t.Fatalf("Directory = %q, want %q", got, directory)
	}
	t.Setenv("RA2A_PI_SESSION_DIR", "")
	if got, want := Directory(), filepath.Join(os.Getenv("HOME"), ".config", "ra2a", "pi-sessions"); got != want {
		t.Fatalf("Directory = %q, want %q", got, want)
	}
}
