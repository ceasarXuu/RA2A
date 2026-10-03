//go:build linux || darwin || windows

package codexcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

// Opt in with an absolute native binary. Every daemon command uses a new home
// and a loopback mock provider; no user credentials or global config are copied.
func TestNativeCLIIsolatedDelivery(t *testing.T) {
	binary := os.Getenv("RA2A_TEST_CODEX_BIN")
	if binary == "" {
		t.Skip("set RA2A_TEST_CODEX_BIN to opt into isolated native app-server validation")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("RA2A_TEST_CODEX_BIN must be absolute")
	}
	// Use short temporary paths; Windows must use its writable user temp root.
	tempRoot, prefix := "/tmp", "ra2a-native-"
	if runtime.GOOS == "windows" {
		tempRoot, prefix = os.TempDir(), "r2-"
	}
	home, err := os.MkdirTemp(tempRoot, prefix)
	if err != nil {
		t.Fatal(err)
	}
	safeToRemove := true
	t.Cleanup(func() {
		if safeToRemove {
			_ = os.RemoveAll(home)
		} else {
			t.Logf("preserving isolated native home because stop/ownership was not verified: %s", home)
		}
	})
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	home = canonicalHome
	if problem := socketPathProblem(controlSocketPath(home)); problem != "" {
		t.Fatal(problem) // Never probe/start a daemon or relocate an overlong path.
	}
	isolatedEnv := map[string]string{
		"CODEX_HOME": home, "HOME": home, "USERPROFILE": home,
		"LOCALAPPDATA": filepath.Join(home, "local"), "APPDATA": filepath.Join(home, "roaming"),
		"TMPDIR": home, "TEMP": home, "TMP": home,
	}
	for key, value := range isolatedEnv {
		t.Setenv(key, value)
	}
	mock := newNativeMock(t)
	config := fmt.Sprintf("model = \"mock-model\"\nmodel_provider = \"ra2a-mock\"\nmodel_reasoning_effort = \"none\"\ncli_auth_credentials_store = \"file\"\n[model_providers.ra2a-mock]\nname = \"RA2A Mock\"\nbase_url = %q\nwire_api = \"responses\"\nenv_key = \"RA2A_MOCK_KEY\"\nrequires_openai_auth = false\n", mock.URL+"/v1")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createNativeStateDirectory(filepath.Join(home, "app-server-daemon")); err != nil {
		t.Fatal(err)
	}
	settings := `{"remoteControlEnabled":false,"shutdownGraceSeconds":10,"updater":{"autoUpdateEnabled":false,"updateIntervalMinutes":1440}}`
	if err := os.WriteFile(filepath.Join(home, "app-server-daemon/settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	// Do not inherit agent routing, proxy injection, or account secrets.
	env := []string{"CODEX_NO_UPDATE=1", "RA2A_MOCK_KEY=fixture-only", "NO_PROXY=127.0.0.1,localhost,::1"}
	for key, value := range isolatedEnv {
		env = append(env, key+"="+value)
	}
	systemKeys := []string{"PATH", "USER", "LOGNAME", "LANG"}
	if runtime.GOOS == "windows" {
		systemKeys = []string{"PATH", "SystemRoot", "ComSpec"}
	}
	for _, key := range systemKeys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	socketPath := controlSocketPath(home)
	pidPath := filepath.Join(home, "app-server-daemon", "daemon.pid")
	ownedPID := 0
	assertPID := func(pid int) {
		t.Helper()
		data, err := os.ReadFile(pidPath)
		var record struct {
			PID              int    `json:"pid"`
			ProcessStartTime string `json:"processStartTime"`
		}
		if err != nil || json.Unmarshal(data, &record) != nil || pid <= 0 || record.PID != pid || record.ProcessStartTime == "" {
			t.Fatalf("refuse lifecycle operation without matching temporary-home PID record: expected=%d record=%+v err=%v", pid, record, err)
		}
	}
	// rust-v0.160.0 app-server-daemon/lib.rs:295-317 derives every resource
	// from CODEX_HOME; backend/mod.rs has only Pid. pid.rs:147-225 validates
	// process identity before stopping the PID from that home's record.
	lifecycle := func(action string) {
		t.Helper()
		if action == "start" {
			for _, path := range []string{socketPath, pidPath,
				filepath.Join(home, "app-server-daemon", "daemon-updater.pid"),
				filepath.Join(home, "app-server-daemon", "app-server.pid"),
				filepath.Join(home, "app-server-daemon", "app-server-updater.pid")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("refuse native start with existing temporary resource %s: %v", path, err)
				}
			}
		}
		if action == "stop" {
			assertPID(ownedPID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "app-server", "daemon", action)
		cmd.Env = env
		var diagnostics bytes.Buffer
		cmd.Stderr = &diagnostics
		if action == "start" {
			safeToRemove = false // Keep failed starts too; they may have launched a child.
		}
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("isolated daemon %s: %v: %s%s", action, err, diagnostics.String(), output)
		}
		if action == "start" {
			var started struct {
				Status           string `json:"status"`
				Backend          string `json:"backend"`
				PID              int    `json:"pid"`
				SocketPath       string `json:"socketPath"`
				ManagedCodexPath string `json:"managedCodexPath"`
			}
			if err := json.Unmarshal(output, &started); err != nil {
				t.Fatalf("native start output: %v: %s", err, output)
			}
			managed, err := filepath.EvalSymlinks(started.ManagedCodexPath)
			if err != nil || started.Status != "started" || started.Backend != "pid" || started.SocketPath != socketPath ||
				!strings.HasPrefix(managed, filepath.Join(home, "packages")+string(os.PathSeparator)) {
				t.Fatalf("native start resources escaped temporary home: %+v, managed=%s err=%v", started, managed, err)
			}
			assertPID(started.PID)
			ownedPID = started.PID
		} else if action == "stop" {
			if _, err := os.Lstat(pidPath); !os.IsNotExist(err) {
				t.Fatalf("native stop did not remove the owned PID record: %v", err)
			}
			ownedPID = 0
			safeToRemove = true
		}
		t.Logf("isolated daemon %s: %s%s", action, diagnostics.String(), strings.TrimSpace(string(output)))
	}
	state, err := detectDaemon(context.Background(), binary, home)
	if err != nil || state.Running || state.SocketPath != socketPath {
		t.Fatalf("new home must have no running daemon: %+v, %v", state, err)
	}
	lifecycle("start")
	// Register stop only after start proved socket, package, and PID ownership.
	t.Cleanup(func() {
		if ownedPID != 0 {
			lifecycle("stop")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	adapter := New("native-fixture", Config{CodexPath: binary, CodexHome: home, Stderr: io.Discard})
	t.Cleanup(func() { _ = adapter.Close() })
	var observer *rpcConn
	completed := make(chan turnRecord, 64)
	notify := func(method string, params json.RawMessage) {
		if method == "turn/completed" {
			var done turnCompletedParams
			if json.Unmarshal(params, &done) == nil {
				completed <- done.Turn
			}
		}
	}
	openObserver := func(threadID string) *appServer {
		t.Helper()
		state, err := detectDaemon(ctx, binary, home)
		if err != nil || !state.Running || state.SocketPath != socketPath {
			t.Fatalf("isolated daemon version: %+v, %v", state, err)
		}
		observer, err = dialRPC(ctx, state.SocketPath, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		init, err := observer.initialize(ctx, "ra2a_native_fixture", "0.0.0", true)
		if err != nil || normalizeCodexHome(init.CodexHome) != normalizeCodexHome(home) {
			t.Fatalf("observer attached to wrong home: %+v, %v", init, err)
		}
		observer.setNotificationHandler(notify)
		server := &appServer{conn: observer}
		if threadID != "" {
			if _, err := server.threadResume(ctx, threadID); err != nil {
				t.Fatal(err)
			}
		}
		return server
	}
	t.Cleanup(func() {
		if observer != nil {
			_ = observer.Close()
		}
	})
	owner := openObserver("")
	awaitOwnerTurn := func(turnID string) {
		t.Helper()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case done := <-completed:
				if done.ID == turnID {
					if done.Status != "completed" {
						t.Fatalf("owner turn: %+v", done)
					}
					return
				}
			case <-timer.C:
				t.Fatalf("owner turn %s did not complete", turnID)
			}
		}
	}
	var created threadResponse
	err = observer.call(ctx, "thread/start", map[string]any{"cwd": home, "model": "mock-model", "approvalPolicy": "never", "sandbox": "read-only"}, &created)
	if err != nil {
		t.Fatal(err)
	}
	threadID := created.Thread.ID
	// Fresh threads have no rollout for a second client's resume until seeded.
	seed, err := owner.turnStart(ctx, threadID, "native-owner-seed")
	if err != nil {
		t.Fatal(err)
	}
	awaitOwnerTurn(seed.ID)
	if err := adapter.Register(threadID); err != nil {
		t.Fatal(err)
	}
	deliver := func(marker string) agentbridge.DeliveryResult {
		return adapter.Deliver(ctx, agentbridge.Address{NodeID: "native-fixture", EndpointID: threadID}, agentbridge.MessageEnvelope{
			ID: marker, ProtocolVersion: agentbridge.ProtocolVersion, SourceAddress: "ra2a://fixture-sender/source", Text: marker,
		})
	}
	for round := 1; round <= 22; round++ {
		endpoints, err := adapter.ListEndpoints(ctx)
		if err != nil || len(endpoints) != 1 || !endpoints[0].Has(agentbridge.CapabilityInteractiveSafe) {
			t.Fatalf("round %d endpoint: %+v, %v", round, endpoints, err)
		}
		marker := fmt.Sprintf("native-round-%02d", round)
		result := deliver(marker)
		if !result.Delivered() {
			t.Fatalf("round %d receipt: %+v", round, result)
		}
		// Execution is checked separately by the owning client's observer.
		awaitOwnerTurn(result.TurnID)
		if !mock.contains(marker, "ra2a://fixture-sender/source") {
			t.Fatalf("round %d lost envelope provenance at native provider boundary", round)
		}
	}
	// The independent owning client can still start its own turn afterwards.
	manual, err := owner.turnStart(ctx, threadID, "native-owner-continue")
	if err != nil {
		t.Fatal(err)
	}
	awaitOwnerTurn(manual.ID)
	mock.hold.Store(true)
	active, err := owner.turnStart(ctx, threadID, "native-active-seed")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mock.started:
	case <-time.After(10 * time.Second):
		t.Fatal("mock provider did not hold active turn")
	}
	// The mock remains blocked until AFTER receipt has returned.
	ackCtx, ackCancel := context.WithTimeout(ctx, 2*time.Second)
	result := adapter.Deliver(ackCtx, agentbridge.Address{EndpointID: threadID}, agentbridge.MessageEnvelope{
		ID: "native-active-followup", SourceAddress: "ra2a://fixture-sender/source", Text: "native-active-followup",
	})
	ackCancel()
	if !result.Delivered() || result.TurnID != active.ID {
		t.Fatalf("receipt must return while native execution is still held: %+v, active=%s", result, active.ID)
	}
	mock.releaseOnce.Do(func() { close(mock.release) })
	awaitOwnerTurn(active.ID)
	if !mock.contains("native-active-followup", "ra2a://fixture-sender/source") {
		t.Fatal("active followup did not reach the native provider")
	}
	_ = observer.Close()
	lifecycle("stop")
	lifecycle("start")
	_ = openObserver(threadID)
	result = deliver("native-after-restart")
	if !result.Delivered() {
		t.Fatalf("native daemon restart recovery: %+v", result)
	}
	awaitOwnerTurn(result.TurnID)
	if !mock.contains("native-after-restart", "ra2a://fixture-sender/source") {
		t.Fatal("delivery after restart did not reach the native provider")
	}
	testNativeLANRecovery(t, ctx, adapter, &appServer{conn: observer}, home, threadID, awaitOwnerTurn)
	if _, err := os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("fixture must remain unauthenticated: %v", err)
	}
	t.Logf("native app-server %s: 22 receipt rounds, independent execution/owner continue, receipt while active model held and daemon restart passed; home=%s", adapter.serverVersion, home)
}

type nativeMock struct {
	*httptest.Server
	mu          sync.Mutex
	requests    []string
	hold        atomic.Bool
	started     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func newNativeMock(t *testing.T) *nativeMock {
	mock := &nativeMock{started: make(chan struct{}), release: make(chan struct{})}
	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected mock request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unsupported fixture request", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-only" {
			t.Error("mock provider must only use the fixture credential")
		}
		body, _ := io.ReadAll(r.Body)
		var request struct{ Model string }
		if err := json.Unmarshal(body, &request); err != nil || request.Model != "mock-model" {
			t.Errorf("native turn must retain the fixture model: model=%q, err=%v", request.Model, err)
		}
		mock.mu.Lock()
		mock.requests = append(mock.requests, string(body))
		n := len(mock.requests)
		mock.mu.Unlock()
		if mock.hold.Swap(false) {
			close(mock.started)
			<-mock.release
		}
		w.Header().Set("Content-Type", "text/event-stream")
		id := fmt.Sprintf("response_%d", n)
		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": id}},
			{"type": "response.output_item.done", "item": map[string]any{"id": id + "_msg", "type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "RA2A-NATIVE-REPLY"}}}},
			{"type": "response.completed", "response": map[string]any{"id": id, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		}
		for _, event := range events {
			payload, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], payload)
		}
	}))
	t.Cleanup(mock.Close)
	t.Cleanup(func() { mock.releaseOnce.Do(func() { close(mock.release) }) })
	return mock
}

func (mock *nativeMock) contains(parts ...string) bool {
	mock.mu.Lock()
	defer mock.mu.Unlock()
	for _, body := range mock.requests {
		matches := true
		for _, part := range parts {
			matches = matches && strings.Contains(body, part)
		}
		if matches {
			return true
		}
	}
	return false
}
