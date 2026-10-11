// ra2a-sandbox-node is an explicit Linux test worker, not an installer or daemon.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/codexcli"
	"github.com/ceasarXuu/RA2A/internal/control"
	"github.com/ceasarXuu/RA2A/internal/lannode"
	"github.com/ceasarXuu/RA2A/internal/opencode"
	"github.com/ceasarXuu/RA2A/internal/pi"
	"github.com/ceasarXuu/RA2A/internal/sandbox"
	"golang.org/x/sys/unix"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func validateIsolation() error {
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil || os.Getenv("RA2A_SANDBOX_HOST_NETNS") == "" || current == os.Getenv("RA2A_SANDBOX_HOST_NETNS") {
		return fmt.Errorf("sandbox requires a private network namespace")
	}
	for _, name := range []string{"HOME", "CODEX_HOME", "RA2A_PI_SESSION_DIR", "RA2A_OC_SESSION_DIR"} {
		value := filepath.Clean(os.Getenv(name))
		if value != "/home/sandbox" && !strings.HasPrefix(value, "/home/sandbox/") {
			return fmt.Errorf("%s must be under private /home/sandbox", name)
		}
	}
	if os.Getenv("RA2A_SANDBOX_BUDGET_SOCKET") != "/run/budget.sock" {
		return fmt.Errorf("sandbox budget socket must be exclusively mounted at /run/budget.sock")
	}
	return nil
}

func run() error {
	// Same-PID-namespace lease observation remains possible; accessing this
	// worker's private mount namespace via /proc/PID/root or fd is prohibited.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	if err := validateIsolation(); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	id := os.Getenv("RA2A_SANDBOX_NODE_ID")
	kind := agentbridge.AgentKind(os.Getenv("RA2A_SANDBOX_KIND"))
	var adapter agentbridge.Adapter
	var cli *codexcli.Adapter
	switch kind {
	case agentbridge.AgentCodexCLI:
		if !filepath.IsAbs(os.Getenv("RA2A_SANDBOX_CODEX_BIN")) {
			return fmt.Errorf("sandbox requires an explicit absolute native Codex binary")
		}
		cli = codexcli.New(id, codexcli.Config{CodexPath: os.Getenv("RA2A_SANDBOX_CODEX_BIN"), CodexHome: os.Getenv("CODEX_HOME"), Stderr: os.Stderr})
		adapter = cli
	case agentbridge.AgentOpenCode:
		oc := opencode.New(id, opencode.Config{BaseURL: "http://127.0.0.1:4099"}, os.Stderr)
		oc.Watch(ctx)
		adapter = oc
	case agentbridge.AgentPi:
		adapter = pi.New(id, pi.Directory())
	default:
		return fmt.Errorf("sandbox Agent kind %q unsupported; no substitute endpoint", kind)
	}
	guarded, err := sandbox.Wrap(adapter, os.Getenv("RA2A_SANDBOX_BUDGET_SOCKET"))
	if err != nil {
		return err
	}
	registry := agentbridge.NewRegistry(id)
	if err := registry.Register(guarded); err != nil {
		return err
	}
	defer registry.Close()
	view := registryView{registry}
	var withheld sync.Map
	node, err := lannode.Start(ctx, lannode.Config{ID: id, Name: id, PIN: os.Getenv("RA2A_SANDBOX_PIN"), Sessions: view.sessions,
		SendMessage: func(ctx context.Context, message lannode.Message) error {
			result := registry.Deliver(ctx, agentbridge.MessageEnvelope{ID: message.MessageID, ProtocolVersion: agentbridge.ProtocolVersion,
				SourceAddress: message.Source, TargetAddress: "ra2a://" + id + "/" + message.TargetSessionID, Text: message.Text, CreatedAt: time.Now().UTC()})
			if !result.Delivered() {
				return fmt.Errorf("%w: %s: %s", control.ErrDeliveryUnknown, result.Code, result.Detail)
			}
			if consumeWithheld(&withheld, message.MessageID, message.Text) {
				awaitTransportACK(ctx, 5*time.Second)
			}
			return nil
		}})
	if err != nil {
		return err
	}
	defer node.Close()
	coordinator := control.NewAdapterCoordinator(id, node, view)
	mux := http.NewServeMux()
	mux.Handle("/", control.NewHandler(coordinator, nil))
	mux.HandleFunc("/sandbox/withhold", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		var request struct {
			ID string `json:"id"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request) != nil || request.ID == "" {
			http.Error(w, "exact message ID required", 400)
			return
		}
		withheld.Store(request.ID, true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"armed":true}`))
	})
	mux.HandleFunc("/sandbox/send-timeout", timedSend(coordinator))
	mux.HandleFunc("/sandbox/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || cli == nil {
			http.Error(w, "CLI registration only", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
			http.Error(w, "invalid registration", 400)
			return
		}
		// Register publishes only existing loaded threads. It does not resume or
		// create a thread; the real client must have acquired its writer already.
		if err := cli.Register(request.ID); err != nil {
			http.Error(w, "invalid thread ID", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"registered":true}`))
	})
	server := &http.Server{Addr: "127.0.0.1:47321", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func consumeWithheld(armed *sync.Map, id, text string) bool {
	if _, ok := armed.LoadAndDelete(id); ok {
		return true
	}
	if fields := strings.Fields(text); len(fields) > 0 && strings.HasPrefix(fields[0], "R2S_") {
		_, ok := armed.LoadAndDelete(fields[0])
		return ok
	}
	return false
}

// awaitTransportACK runs only after the adapter has confirmed native receipt.
// It simulates lost transport acknowledgement, never canceled native input.
func awaitTransportACK(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func timedSend(backend control.Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		var request control.SendRequest
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&request) != nil || request.MessageID == "" {
			http.Error(w, "full SendRequest with messageId required", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		result := map[string]string{"status": "accepted"}
		if err := backend.Send(ctx, request); err != nil {
			result["status"] = "error"
			result["error"] = "NON_UNKNOWN_FAILURE"
			if errors.Is(err, control.ErrDeliveryUnknown) {
				result["error"] = "DELIVERY_UNKNOWN"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}

type registryView struct{ *agentbridge.Registry }

func (v registryView) Endpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	endpoints, problems := v.Registry.Endpoints(ctx)
	return endpoints, errors.Join(problems...)
}

func (v registryView) sessions(ctx context.Context) ([]lannode.Session, error) {
	endpoints, err := v.Endpoints(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]lannode.Session, 0, len(endpoints))
	for _, endpoint := range endpoints {
		capabilities := make([]string, 0, len(endpoint.Capabilities))
		for _, capability := range endpoint.Capabilities {
			capabilities = append(capabilities, string(capability))
		}
		result = append(result, lannode.Session{ID: endpoint.ID, Address: endpoint.Address.String(), Title: endpoint.Title, Status: string(endpoint.Status), Agent: string(endpoint.Agent), Capabilities: capabilities})
	}
	return result, nil
}
