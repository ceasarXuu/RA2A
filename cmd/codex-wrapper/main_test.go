package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClassifyPlainPromptInjects(t *testing.T) {
	plan := classify([]string{"[RA2A] help me"})
	if !plan.tuiMode || !plan.injectRemote || plan.explicitRemote {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestClassifyBareCodexInjects(t *testing.T) {
	plan := classify(nil)
	if !plan.tuiMode || !plan.injectRemote {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestClassifyExecPassesThrough(t *testing.T) {
	for _, args := range [][]string{
		{"exec", "fix the bug"},
		{"e", "fix the bug"},
		{"app-server", "--listen", "unix:///tmp/x.sock"},
		{"mcp"},
		{"login"},
		{"agents"},
		{"queue", "--thread", "t1", "--message", "m"},
		{"help"},
		{"version"},
	} {
		plan := classify(args)
		if plan.injectRemote || plan.tuiMode {
			t.Fatalf("args %v: plan = %#v", args, plan)
		}
	}
}

func TestClassifyConfigValueDoesNotBecomeSubcommand(t *testing.T) {
	plan := classify([]string{"-c", "model=o4-mini", "please explain"})
	if !plan.tuiMode || !plan.injectRemote {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestClassifyExplicitRemoteNeverInjects(t *testing.T) {
	for _, args := range [][]string{
		{"--remote", "ws://127.0.0.1:4500", "hi"},
		{"--remote=ws://127.0.0.1:4500", "hi"},
		{"--remote-auth-token-env", "TOKEN", "hi"},
		{"hi", "--remote", "ws://127.0.0.1:4500"},
	} {
		plan := classify(args)
		if plan.injectRemote || !plan.explicitRemote {
			t.Fatalf("args %v: plan = %#v", args, plan)
		}
	}
}

func TestClassifyFlagOnlyInvocationInjects(t *testing.T) {
	for _, args := range [][]string{
		{"--yolo"},
		{"-m", "gpt-6-sol"},
		{"--model", "gpt-6-sol", "--yolo"},
	} {
		plan := classify(args)
		if !plan.tuiMode || !plan.injectRemote {
			t.Fatalf("args %v: plan = %#v", args, plan)
		}
	}
}

func TestClassifyHelpAndVersionPassThrough(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--version"}, {"-V"}, {"--yolo", "--help"}} {
		plan := classify(args)
		if plan.injectRemote || plan.tuiMode {
			t.Fatalf("args %v: plan = %#v", args, plan)
		}
	}
}

func TestOfficialDaemonKeepsNativeTUI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fake CLI fixture")
	}
	native := fakeNativeCodex(t)
	t.Setenv("CODEX_TEST_DAEMON_STATUS", "running")
	args := []string{"--yolo"}
	if got := managedArgs(native, "/tmp/managed.sock", args); len(got) != 1 || got[0] != "--yolo" {
		t.Fatalf("native shared daemon must keep TUI untouched: %v", got)
	}
}

func TestManagedFallbackRequiresUsableHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fake CLI fixture")
	}
	native := fakeNativeCodex(t)
	t.Setenv("CODEX_TEST_DAEMON_STATUS", "stopped")
	args := []string{"--yolo"}

	t.Run("usable host injects remote", func(t *testing.T) {
		home := newShortDir(t)
		t.Setenv("CODEX_HOME", home)
		host := startFakeManagedHost(t, home, "")
		got := managedArgs(native, host.socketPath, args)
		want := []string{"--remote", "unix://" + host.socketPath, "--yolo"}
		if len(got) != len(want) {
			t.Fatalf("managed fallback args = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("managed fallback args = %v, want %v", got, want)
			}
		}
	})

	t.Run("host without account access keeps native", func(t *testing.T) {
		home := newShortDir(t)
		t.Setenv("CODEX_HOME", home)
		// The observed incident: the managed host inherits the RA2A service
		// environment and cannot reach the account backend.
		host := startFakeManagedHost(t, home, "failed to fetch codex rate limits")
		if got := managedArgs(native, host.socketPath, args); len(got) != 1 || got[0] != "--yolo" {
			t.Fatalf("unusable host must keep the native TUI: %v", got)
		}
	})

	t.Run("host with foreign codex home keeps native", func(t *testing.T) {
		t.Setenv("CODEX_HOME", newShortDir(t))
		host := startFakeManagedHost(t, newShortDir(t), "")
		if got := managedArgs(native, host.socketPath, args); len(got) != 1 || got[0] != "--yolo" {
			t.Fatalf("host with another codex home must keep the native TUI: %v", got)
		}
	})

	t.Run("unreachable host keeps native", func(t *testing.T) {
		t.Setenv("CODEX_HOME", newShortDir(t))
		host := startFakeManagedHost(t, codexHome(), "")
		_ = host.listener.Close()
		if got := managedArgs(native, host.socketPath, args); len(got) != 1 || got[0] != "--yolo" {
			t.Fatalf("unreachable host must keep the native TUI: %v", got)
		}
	})
}

func fakeNativeCodex(t *testing.T) string {
	t.Helper()
	native := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(native, []byte("#!/bin/sh\nprintf '{\"status\":\"%s\"}\\n' \"$CODEX_TEST_DAEMON_STATUS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return native
}

// fakeManagedHost answers the two calls the managed-host gate performs:
// initialize and account/rateLimits/read.
type fakeManagedHost struct {
	listener   net.Listener
	socketPath string
	codexHome  string
	accountErr string
}

func startFakeManagedHost(t *testing.T, codexHome, accountErr string) *fakeManagedHost {
	t.Helper()
	socketPath := filepath.Join(newShortDir(t), "managed.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	host := &fakeManagedHost{listener: listener, socketPath: socketPath, codexHome: codexHome, accountErr: accountErr}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		host.serve(conn)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return host
}

func (host *fakeManagedHost) serve(conn *websocket.Conn) {
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var message struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(payload, &message); err != nil || message.ID == nil {
			continue
		}
		switch message.Method {
		case "initialize":
			host.reply(conn, map[string]any{"id": *message.ID, "jsonrpc": "2.0", "result": map[string]any{
				"userAgent": "ra2a_codex_cli/0.159.2 (Ubuntu; x86_64)", "codexHome": host.codexHome,
				"platformFamily": "unix", "platformOs": "linux",
			}})
		case "account/rateLimits/read":
			if host.accountErr != "" {
				host.reply(conn, map[string]any{"id": *message.ID, "jsonrpc": "2.0",
					"error": map[string]any{"code": -32603, "message": host.accountErr}})
				continue
			}
			host.reply(conn, map[string]any{"id": *message.ID, "jsonrpc": "2.0", "result": map[string]any{"rateLimits": map[string]any{}}})
		}
	}
}

func (host *fakeManagedHost) reply(conn *websocket.Conn, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, payload)
}

func TestReadySocketAcceptsOnlyLiveManagedServer(t *testing.T) {
	home := newShortDir(t)
	t.Setenv("CODEX_HOME", home)
	controlDir := filepath.Join(home, "app-server-control")
	if err := os.MkdirAll(controlDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lease := filepath.Join(controlDir, "app-server-control.sock.ra2a-owner.json")

	t.Run("missing lease", func(t *testing.T) {
		if got := readySocket(); got != "" {
			t.Fatalf("readySocket = %q with no lease", got)
		}
	})

	listener, err := net.Listen("unix", filepath.Join(controlDir, "live.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	record := ownerRecord{PID: os.Getpid(), SocketPath: listener.Addr().String()}
	writeRecord(t, lease, record)
	t.Run("live socket", func(t *testing.T) {
		if got := readySocket(); got != record.SocketPath {
			t.Fatalf("readySocket = %q, want %q", got, record.SocketPath)
		}
	})

	t.Run("stale socket file", func(t *testing.T) {
		dead := filepath.Join(controlDir, "dead.sock")
		if err := os.WriteFile(dead, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		writeRecord(t, lease, ownerRecord{PID: os.Getpid(), SocketPath: dead})
		// A regular file is not a socket: must not be considered ready.
		if got := readySocket(); got != "" {
			t.Fatalf("readySocket = %q for a plain file", got)
		}
	})

	t.Run("no listener behind socket path", func(t *testing.T) {
		path := filepath.Join(controlDir, "unconnected.sock")
		// Create a socket file via Listen then close it to leave a stale path.
		probe, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		address := probe.Addr().String()
		_ = probe.Close()
		writeRecord(t, lease, ownerRecord{PID: os.Getpid(), SocketPath: address})
		if got := readySocket(); got != "" {
			t.Fatalf("readySocket = %q for an unconnected socket", got)
		}
	})
}

func TestReadySocketResolvesRelocatedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket fixture")
	}
	home := newShortDir(t)
	t.Setenv("CODEX_HOME", home)
	controlDir := filepath.Join(home, "app-server-control")
	if err := os.MkdirAll(controlDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lease := filepath.Join(controlDir, "app-server-control.sock.ra2a-owner.json")

	// Codex 0.159+ binds the real socket outside CODEX_HOME (AF_UNIX path
	// limit workaround) and leaves a symlink behind in the control directory.
	realDir := newShortDir(t)
	realSocket := filepath.Join(realDir, "relocated.sock")
	listener, err := net.Listen("unix", realSocket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	link := filepath.Join(controlDir, "app-server-control.sock.ra2a-1.sock")
	if err := os.Symlink(realSocket, link); err != nil {
		t.Fatal(err)
	}

	t.Run("live symlink resolves to the real socket", func(t *testing.T) {
		writeRecord(t, lease, ownerRecord{PID: os.Getpid(), SocketPath: link})
		if got := readySocket(); got != realSocket {
			t.Fatalf("readySocket = %q, want resolved %q", got, realSocket)
		}
	})

	t.Run("dangling symlink rejected", func(t *testing.T) {
		dangling := filepath.Join(controlDir, "dangling.sock")
		if err := os.Symlink(filepath.Join(realDir, "gone.sock"), dangling); err != nil {
			t.Fatal(err)
		}
		writeRecord(t, lease, ownerRecord{PID: os.Getpid(), SocketPath: dangling})
		if got := readySocket(); got != "" {
			t.Fatalf("readySocket = %q for a dangling symlink", got)
		}
	})

	t.Run("symlink to a regular file rejected", func(t *testing.T) {
		file := filepath.Join(realDir, "plain")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		linkToFile := filepath.Join(controlDir, "file-link.sock")
		if err := os.Symlink(file, linkToFile); err != nil {
			t.Fatal(err)
		}
		writeRecord(t, lease, ownerRecord{PID: os.Getpid(), SocketPath: linkToFile})
		if got := readySocket(); got != "" {
			t.Fatalf("readySocket = %q for a symlink to a plain file", got)
		}
	})
}

func TestRealCodexSkipsWrapperItself(t *testing.T) {
	wrapper := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_WRAPPER_REAL_BIN", "")
	t.Setenv("PATH", "")
	real, err := realCodex(wrapper)
	if err == nil {
		t.Fatalf("expected error resolving only the wrapper itself, got %q", real)
	}
}

func TestRealCodexPrefersSiblingBinary(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "codex")
	real := filepath.Join(dir, "codex.bin")
	for _, path := range []string{wrapper, real} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", "")
	got, err := realCodex(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if got != real {
		t.Fatalf("realCodex = %q, want %q", got, real)
	}
}

func TestRealCodexFallsBackToPathExcludingWrapper(t *testing.T) {
	dir := t.TempDir()
	name := "codex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	wrapper := filepath.Join(dir, name)
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	realDir := t.TempDir()
	real := filepath.Join(realDir, name)
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(dir, "bin")+string(os.PathListSeparator)+realDir)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := realCodex(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if got != real {
		t.Fatalf("realCodex = %q, want %q", got, real)
	}
}

func TestRealCodexPrefersOfficialStandaloneManagedPath(t *testing.T) {
	home := newShortDir(t)
	t.Setenv("CODEX_HOME", home)
	standalone := filepath.Join(home, "packages", "standalone", "current", "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(standalone), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(standalone, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	got, err := realCodex(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if got != standalone {
		t.Fatalf("realCodex = %q, want official standalone %q", got, standalone)
	}
}

func TestReadySocketTimeoutDoesNotHang(t *testing.T) {
	home := newShortDir(t)
	t.Setenv("CODEX_HOME", home)
	controlDir := filepath.Join(home, "app-server-control")
	_ = os.MkdirAll(controlDir, 0o700)
	path := filepath.Join(controlDir, "slow.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// Accept connections but never respond: the wrapper only dials, which
	// succeeds immediately; the guard is that we never open a real frame.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	writeRecord(t, filepath.Join(controlDir, "app-server-control.sock.ra2a-owner.json"),
		ownerRecord{PID: os.Getpid(), SocketPath: path})
	start := time.Now()
	if got := readySocket(); got == "" {
		t.Fatal("live accepting socket was not considered ready")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("readySocket took %v", elapsed)
	}
}

func writeRecord(t *testing.T, path string, record ownerRecord) {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// newShortDir returns a temp dir under the platform temp root with a short
// name so unix socket paths (104-byte sun_path limit on macOS) stay legal.
func newShortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
