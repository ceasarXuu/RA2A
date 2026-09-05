package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestClassifyHelpAndVersionPassThrough(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		plan := classify(args)
		if plan.injectRemote || plan.tuiMode {
			t.Fatalf("args %v: plan = %#v", args, plan)
		}
	}
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
