//go:build linux || darwin || windows

package codexcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/codexstdio"
)

// This is an isolated stdio transport experiment, not a Desktop integration test.
// The parent client owns initialization, creation, subscriptions and continuation.
func TestNativeStdioBridgeReceiptAndOwnerContinuation(t *testing.T) {
	binary := os.Getenv("RA2A_TEST_CODEX_BIN")
	if binary == "" {
		t.Skip("set absolute RA2A_TEST_CODEX_BIN to opt into isolated stdio validation")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("RA2A_TEST_CODEX_BIN must be absolute")
	}
	home := t.TempDir()
	mock := newNativeMock(t)
	config := fmt.Sprintf("model = \"mock-model\"\nmodel_provider = \"ra2a-mock\"\nmodel_reasoning_effort = \"none\"\ncli_auth_credentials_store = \"file\"\nthread_unload_delay_secs = 1\n[features]\nplugins = false\n[model_providers.ra2a-mock]\nname = \"RA2A Mock\"\nbase_url = %q\nwire_api = \"responses\"\nenv_key = \"RA2A_MOCK_KEY\"\nrequires_openai_auth = false\n", mock.URL+"/v1")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"CODEX_NO_UPDATE=1", "RA2A_MOCK_KEY=fixture-only", "NO_PROXY=127.0.0.1,localhost,::1"}
	for _, key := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "CODEX_HOME", "TMPDIR", "TEMP", "TMP"} {
		env = append(env, key+"="+home)
	}
	for _, key := range []string{"PATH", "SystemRoot", "ComSpec", "LANG"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server")
	cmd.Env = env
	cmd.Dir = home
	stderr := &stdioFixtureDiagnostics{}
	cmd.Stderr = stderr
	nativeInput, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	nativeOutput, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	appInput, appWriter := io.Pipe()
	appReader, appOutput := io.Pipe()
	bridge := codexstdio.New(appInput, appOutput, nativeOutput, nativeInput)
	bridgeDone := make(chan error, 1)
	go func() { bridgeDone <- bridge.Run(ctx) }()
	client := newStdioFixtureClient(appReader, appWriter)
	t.Cleanup(func() {
		_ = appWriter.Close()
		_ = nativeInput.Close()
		cancel()
		_ = appReader.Close()
		_ = appInput.Close()
		_ = appOutput.Close()
		_ = nativeOutput.Close()
		select {
		case <-bridgeDone:
		case <-time.After(5 * time.Second):
			t.Error("stdio bridge pumps did not stop")
		}
		waitErr := cmd.Wait() // Direct child only; cancellation may produce nonzero exit.
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() && waitErr == nil {
			t.Errorf("owned native child exit unconfirmed: %v", waitErr)
		} else if cmd.ProcessState != nil {
			t.Logf("owned native child reaped: pid=%d exitCode=%d state=%s waitErr=%v", cmd.Process.Pid, cmd.ProcessState.ExitCode(), cmd.ProcessState, waitErr)
		}
		<-client.done
	})
	init := client.call(t, ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "codex-backend", "version": "0.0.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	var initialized struct {
		CodexHome string `json:"codexHome"`
	}
	if json.Unmarshal(init, &initialized) != nil || !sameStdioFixtureDirectory(initialized.CodexHome, home) {
		t.Fatalf("native home escaped fixture: %s; stderr=%s", init, stderr.String())
	}
	client.write(t, map[string]any{"method": "initialized", "params": map[string]any{}})
	started := client.call(t, ctx, "thread/start", map[string]any{"cwd": home, "approvalPolicy": "never", "sandbox": "danger-full-access"})
	var created struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(started, &created) != nil || !validThreadID(created.Thread.ID) {
		t.Fatalf("invalid owner thread: %s", started)
	}
	thread := created.Thread.ID
	lock := filepath.Join(home, "thread-writer-locks", thread+".lock")
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("owner creation must hold fixture writer: %v", err)
	}
	if _, err := bridge.Call(ctx, "thread/resume", map[string]any{"threadId": thread}); err == nil {
		t.Fatal("bridge must forbid resume")
	}
	marker := "RA2A_STDIO_BRIDGE_NATIVE_RECEIPT_001"
	receipt, err := bridge.Call(ctx, "turn/start", map[string]any{"threadId": thread, "input": userInput(marker)})
	if err != nil {
		t.Fatalf("bridge receipt: %v; stderr=%s", err, stderr.String())
	}
	var accepted struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(receipt, &accepted) != nil || accepted.Turn.ID == "" {
		t.Fatalf("bridge response lacked native turn receipt: %s", receipt)
	}
	client.awaitTurn(t, ctx, thread, marker)
	if !mock.contains(marker) {
		t.Fatal("mock provider did not receive bridge input")
	}
	ownerMarker := "RA2A_STDIO_NATIVE_OWNER_CONTINUE_001"
	client.call(t, ctx, "turn/start", map[string]any{"threadId": thread, "input": userInput(ownerMarker)})
	client.awaitTurn(t, ctx, thread, ownerMarker)
	if !mock.contains(ownerMarker) {
		t.Fatal("original owner could not independently continue")
	}
	if _, err := bridge.Call(ctx, "turn/start", map[string]any{"threadId": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "input": userInput("MUST-NOT-LOAD")}); err == nil {
		t.Fatal("missing thread must reject without resume")
	}
	client.call(t, ctx, "thread/unsubscribe", map[string]any{"threadId": thread})
	deadline := time.Now().Add(10 * time.Second)
	unloaded := false
	for time.Now().Before(deadline) {
		raw, err := bridge.Call(ctx, "thread/loaded/list", map[string]any{})
		var loaded struct {
			Data []string `json:"data"`
		}
		if err != nil || json.Unmarshal(raw, &loaded) != nil {
			t.Fatalf("loaded probe: %v %s", err, raw)
		}
		found := false
		for _, id := range loaded.Data {
			found = found || id == thread
		}
		if !found {
			unloaded = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !unloaded {
		t.Fatal("native unsubscribe did not unload within isolated configured delay")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("unloaded fixture writer remained: %v", err)
	}
	if _, err := bridge.Call(ctx, "turn/start", map[string]any{"threadId": thread, "input": userInput("MUST-NOT-WAKE")}); err == nil {
		t.Fatal("bridge awakened sleeping native owner")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("rejected sleeping input created writer: %v", err)
	}
	if mock.contains("MUST-NOT-WAKE") || mock.contains("MUST-NOT-LOAD") {
		t.Fatal("rejected input reached mock provider")
	}
	t.Logf("native stdio receipt, owner continuation, missing/sleeping rejection verified; pid=%d", cmd.Process.Pid)
}

// Native may canonicalize /tmp to /private/tmp; identity must still be exact.
func sameStdioFixtureDirectory(actual, expected string) bool {
	if !filepath.IsAbs(actual) || !filepath.IsAbs(expected) {
		return false
	}
	a, aErr := os.Stat(actual)
	b, bErr := os.Stat(expected)
	return aErr == nil && bErr == nil && a.IsDir() && b.IsDir() && os.SameFile(a, b)
}

func TestStdioFixtureDirectoryIdentity(t *testing.T) {
	home, other := t.TempDir(), t.TempDir()
	if !sameStdioFixtureDirectory(home, home) || sameStdioFixtureDirectory(other, home) || sameStdioFixtureDirectory(filepath.Join(home, "missing"), home) || sameStdioFixtureDirectory(".", home) {
		t.Fatal("directory identity gate accepted a different/missing/relative home")
	}
	alias := filepath.Join(other, "alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Logf("directory alias unavailable on this host: %v", err)
		return
	}
	if !sameStdioFixtureDirectory(alias, home) {
		t.Fatal("same-directory alias rejected")
	}
}

type stdioFixtureDiagnostics struct {
	mu   sync.Mutex
	text strings.Builder
}

func (d *stdioFixtureDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.text.Write(p)
}
func (d *stdioFixtureDiagnostics) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.text.String()
}

type stdioFixtureClient struct {
	writer io.Writer
	inbox  chan map[string]json.RawMessage
	done   chan struct{}
	seq    int
	seen   []map[string]json.RawMessage
}

func newStdioFixtureClient(reader io.Reader, writer io.Writer) *stdioFixtureClient {
	c := &stdioFixtureClient{writer: writer, inbox: make(chan map[string]json.RawMessage, 256), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer close(c.inbox)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		for scanner.Scan() {
			var frame map[string]json.RawMessage
			if json.Unmarshal(scanner.Bytes(), &frame) == nil {
				c.inbox <- frame
			}
		}
	}()
	return c
}
func (c *stdioFixtureClient) write(t *testing.T, frame any) {
	t.Helper()
	if err := json.NewEncoder(c.writer).Encode(frame); err != nil {
		t.Fatal(err)
	}
}
func (c *stdioFixtureClient) next(t *testing.T, ctx context.Context) map[string]json.RawMessage {
	t.Helper()
	select {
	case frame, ok := <-c.inbox:
		if !ok {
			t.Fatal("original App output closed")
		}
		c.seen = append(c.seen, frame)
		return frame
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil
	}
}
func (c *stdioFixtureClient) call(t *testing.T, ctx context.Context, method string, params any) json.RawMessage {
	t.Helper()
	c.seq++
	id := c.seq
	c.write(t, map[string]any{"id": id, "method": method, "params": params})
	for {
		frame := c.next(t, ctx)
		var got int
		_ = json.Unmarshal(frame["id"], &got)
		if got == id {
			if e := frame["error"]; len(e) != 0 {
				t.Fatalf("owner %s: %s", method, e)
			}
			return frame["result"]
		}
	}
}
func (c *stdioFixtureClient) awaitTurn(t *testing.T, ctx context.Context, thread, marker string) {
	t.Helper()
	for {
		started, user, completed := false, false, false
		for _, f := range c.seen {
			var method string
			_ = json.Unmarshal(f["method"], &method)
			raw := string(f["params"])
			if !strings.Contains(raw, thread) {
				continue
			}
			started = started || method == "turn/started"
			user = user || (method == "item/started" && strings.Contains(raw, "userMessage") && strings.Contains(raw, marker))
			completed = completed || method == "turn/completed" && strings.Contains(raw, marker)
		}
		// turn/completed carries a turn ID, not necessarily its earlier input.
		if started && user {
			for _, f := range c.seen {
				if string(f["method"]) == `"turn/completed"` && strings.Contains(string(f["params"]), thread) {
					completed = true
				}
			}
		}
		if started && user && completed {
			c.seen = nil
			return
		}
		c.next(t, ctx)
	}
}
