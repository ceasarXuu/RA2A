package sandbox

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

type nativeAdapter struct {
	calls  atomic.Int32
	result agentbridge.DeliveryResult
}

func (*nativeAdapter) Kind() agentbridge.AgentKind                                   { return agentbridge.AgentPi }
func (*nativeAdapter) ListEndpoints(context.Context) ([]agentbridge.Endpoint, error) { return nil, nil }
func (n *nativeAdapter) Deliver(context.Context, agentbridge.Address, agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	n.calls.Add(1)
	return n.result
}
func (*nativeAdapter) Health(context.Context) agentbridge.Health { return agentbridge.Ready() }
func (*nativeAdapter) Close() error                              { return nil }

func serveBudget(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "budget.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return socket
}

func TestReceptionBudgetGuardsAllSessionsAndConcurrentDuplicates(t *testing.T) {
	var mu sync.Mutex
	used := 0
	seen := map[string]*agentbridge.DeliveryResult{}
	socket := serveBudget(t, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Kind   string                     `json:"kind"`
			ID     string                     `json:"id"`
			Result agentbridge.DeliveryResult `json:"result"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if input.Kind != "pi" {
			t.Errorf("untrusted kind: %s", input.Kind)
		}
		if r.URL.Path == "/finish" {
			if input.Result.Detail != "" || input.Result.NativeErrorClass != "" {
				t.Error("native arbitrary detail copied into ledger")
			}
			seen[input.ID] = &input.Result
			w.Write([]byte(`{"ok":true}`))
			return
		}
		if previous, ok := seen[input.ID]; ok {
			json.NewEncoder(w).Encode(reservation{Duplicate: true, Result: previous})
			return
		}
		if used == 10 {
			json.NewEncoder(w).Encode(reservation{Error: "limit reached"})
			return
		}
		used++
		seen[input.ID] = nil
		json.NewEncoder(w).Encode(reservation{Allow: true})
	})
	native := &nativeAdapter{result: agentbridge.DeliveryResult{Code: agentbridge.ResultDelivered, TurnID: "native-turn", Detail: "SECRET", NativeErrorClass: "SECRET"}}
	a, err := Wrap(native, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Deliver(context.Background(), agentbridge.Address{EndpointID: "first"}, agentbridge.MessageEnvelope{ID: "one", Text: "SECRET"})
		}()
	}
	wg.Wait()
	if native.calls.Load() != 1 {
		t.Fatalf("duplicate native delivery count=%d", native.calls.Load())
	}
	for _, id := range []string{"2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		if !a.Deliver(context.Background(), agentbridge.Address{EndpointID: "other-session"}, agentbridge.MessageEnvelope{ID: id}).Delivered() {
			t.Fatal("slot unexpectedly refused")
		}
	}
	result := a.Deliver(context.Background(), agentbridge.Address{EndpointID: "resumed-session"}, agentbridge.MessageEnvelope{ID: "11"})
	if result.NativeErrorClass != "sandbox_budget_exhausted" || native.calls.Load() != 10 {
		t.Fatalf("limit bypass: %+v calls=%d", result, native.calls.Load())
	}
	duplicate := a.Deliver(context.Background(), agentbridge.Address{}, agentbridge.MessageEnvelope{ID: "one"})
	if !duplicate.Delivered() || duplicate.Detail != "" {
		t.Fatalf("previous receipt unavailable or leaked: %+v", duplicate)
	}
}

func TestBudgetFailureNeverInvokesNative(t *testing.T) {
	for _, reply := range []string{`garbage`, `{"allow":false}`, `{"allow":true,"duplicate":true}`, `{"allow":true,"error":"denied"}`} {
		t.Run(reply, func(t *testing.T) {
			socket := serveBudget(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(reply)) })
			native := &nativeAdapter{}
			a, _ := Wrap(native, socket)
			defer a.Close()
			result := a.Deliver(context.Background(), agentbridge.Address{}, agentbridge.MessageEnvelope{ID: "unique"})
			if result.Delivered() || native.calls.Load() != 0 {
				t.Fatalf("failed open: %+v", result)
			}
		})
	}
}

func TestFinishFailureRetainsUnknownAndDoesNotReplay(t *testing.T) {
	var reserved bool
	socket := serveBudget(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/finish" {
			http.Error(w, "controller down", 503)
			return
		}
		if reserved {
			w.Write([]byte(`{"duplicate":true}`))
			return
		}
		reserved = true
		w.Write([]byte(`{"allow":true}`))
	})
	native := &nativeAdapter{result: agentbridge.Delivered("turn")}
	a, _ := Wrap(native, socket)
	defer a.Close()
	for i := 0; i < 2; i++ {
		result := a.Deliver(context.Background(), agentbridge.Address{}, agentbridge.MessageEnvelope{ID: "unique"})
		if result.Code != agentbridge.ResultUnknown || !strings.Contains(result.Detail, "do not replay") {
			t.Fatalf("unknown downgraded: %+v", result)
		}
	}
	if native.calls.Load() != 1 {
		t.Fatal("unknown outcome replayed")
	}
}

func TestCanceledAndMissingIDDoNotInvokeNative(t *testing.T) {
	socket := serveBudget(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected reservation")
		w.Write([]byte(`{"allow":true}`))
	})
	native := &nativeAdapter{}
	a, _ := Wrap(native, socket)
	defer a.Close()
	a.Deliver(context.Background(), agentbridge.Address{}, agentbridge.MessageEnvelope{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.Deliver(ctx, agentbridge.Address{}, agentbridge.MessageEnvelope{ID: "canceled"})
	if native.calls.Load() != 0 {
		t.Fatal("native invoked")
	}
}
