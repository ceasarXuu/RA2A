//go:build linux

package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/codexstdio"
)

func TestNativeAppAdapterWriterReceiptAndSleepingRejection(t *testing.T) {
	binary := os.Getenv("RA2A_TEST_CODEX_BIN")
	if binary == "" {
		t.Skip("set absolute native binary to opt into private mock-provider test")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("absolute binary required")
	}
	binary, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	count := new(atomic.Int32)
	held, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer fixture-only" {
			http.Error(w, "fixture only", 403)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		if count.Add(1) == 1 {
			close(held)
			<-release
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "fixture-response"}},
			{"type": "response.output_item.done", "item": map[string]any{"id": "fixture-msg", "type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "FIXTURE_REPLY"}}}},
			{"type": "response.completed", "response": map[string]any{"id": "fixture-response", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			payload, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], payload)
		}
	}))
	t.Cleanup(mock.Close)
	config := fmt.Sprintf("model=\"mock-model\"\nmodel_provider=\"fixture\"\nmodel_reasoning_effort=\"none\"\ncli_auth_credentials_store=\"file\"\nthread_unload_delay_secs=1\n[features]\nplugins=false\n[model_providers.fixture]\nname=\"Fixture\"\nbase_url=%q\nwire_api=\"responses\"\nenv_key=\"RA2A_MOCK_KEY\"\nrequires_openai_auth=false\n", mock.URL+"/v1")
	if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server")
	cmd.Dir = home
	cmd.Stderr = io.Discard
	cmd.Env = []string{"PATH=/usr/bin:/bin", "NO_PROXY=127.0.0.1", "RA2A_MOCK_KEY=fixture-only", "CODEX_NO_UPDATE=1"}
	for _, key := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "CODEX_HOME", "TEMP", "TMP", "TMPDIR"} {
		cmd.Env = append(cmd.Env, key+"="+home)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	appIn, writer := io.Pipe()
	reader, appOut := io.Pipe()
	bridge := codexstdio.New(appIn, appOut, out, in)
	done := make(chan error, 1)
	go func() { done <- bridge.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		bridge.Close()
		wait := cmd.Wait()
		t.Logf("owned native child reaped pid=%d exit=%d err=%v", cmd.Process.Pid, cmd.ProcessState.ExitCode(), wait)
		<-done
	})
	frames := make(chan map[string]json.RawMessage, 256)
	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 16<<20)
		for scanner.Scan() {
			var f map[string]json.RawMessage
			if json.Unmarshal(scanner.Bytes(), &f) == nil {
				frames <- f
			}
		}
		close(frames)
	}()
	sequence := 0
	call := func(method string, params any) json.RawMessage {
		sequence++
		_ = json.NewEncoder(writer).Encode(map[string]any{"id": sequence, "method": method, "params": params})
		for {
			select {
			case f := <-frames:
				var id int
				_ = json.Unmarshal(f["id"], &id)
				if id == sequence {
					if len(f["error"]) > 0 {
						t.Fatalf("owner request %s rejected", method)
					}
					return f["result"]
				}
			case <-ctx.Done():
				t.Fatal("owner RPC timeout")
			}
		}
	}
	call("initialize", map[string]any{"clientInfo": map[string]any{"name": "codex-backend", "version": "0"}, "capabilities": map[string]any{"experimentalApi": true}})
	_ = json.NewEncoder(writer).Encode(map[string]any{"method": "initialized"})
	var created struct{ Thread struct{ ID string } }
	if json.Unmarshal(call("thread/start", map[string]any{"cwd": home, "approvalPolicy": "never", "sandbox": "danger-full-access"}), &created) != nil || created.Thread.ID == "" {
		t.Fatal("fixture thread absent")
	}
	h := AppHost{Bridge: bridge, PID: cmd.Process.Pid, Start: AppProcessStart(cmd.Process.Pid), Home: home, Executable: binary, NodeID: "fixture"}
	if endpoints := h.endpoints(ctx); len(endpoints) != 1 || endpoints[0].ID != created.Thread.ID {
		t.Fatalf("native App endpoints %v", endpoints)
	}
	result := h.deliver(ctx, created.Thread.ID, "NATIVE_APP_FIXTURE_ONCE")
	if result.Code != agentbridge.ResultDelivered || result.TurnID == "" {
		t.Fatalf("native receipt %v", result)
	}
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("held mock not reached")
	}
	followup := h.deliver(ctx, created.Thread.ID, "NATIVE_APP_FOLLOWUP")
	if followup.Code != agentbridge.ResultDelivered || followup.TurnID != result.TurnID {
		t.Fatalf("native busy steer %v", followup)
	}
	close(release)
	for {
		select {
		case f := <-frames:
			if string(f["method"]) == `"turn/completed"` {
				goto completed
			}
		case <-ctx.Done():
			t.Fatal("native completion timeout")
		}
	}
completed:
	if count.Load() < 1 {
		t.Fatalf("mock calls %d", count.Load())
	}
	// The original client remains the owner and may continue without reconnecting.
	call("turn/start", map[string]any{"threadId": created.Thread.ID, "input": []map[string]any{{"type": "text", "text": "OWNER_CONTINUE", "textElements": []any{}}}})
	for {
		select {
		case f := <-frames:
			if string(f["method"]) == `"turn/completed"` {
				goto ownerCompleted
			}
		case <-ctx.Done():
			t.Fatal("owner continuation timeout")
		}
	}
ownerCompleted:
	beforeSleep := count.Load()
	call("thread/unsubscribe", map[string]any{"threadId": created.Thread.ID})
	deadline := time.Now().Add(5 * time.Second)
	for len(h.endpoints(ctx)) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if len(h.endpoints(ctx)) != 0 {
		t.Fatal("native owner did not unload")
	}
	if h.deliver(ctx, created.Thread.ID, "MUST_NOT_WAKE").Code != agentbridge.ResultNotFound || count.Load() != beforeSleep {
		t.Fatal("sleeping native received input")
	}
	if _, err = os.Stat(filepath.Join(home, "thread-writer-locks", created.Thread.ID+".lock")); !os.IsNotExist(err) {
		t.Fatal("sleeping writer recreated")
	}
}
