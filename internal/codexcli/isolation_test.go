package codexcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

func TestAdapterUsesConfiguredHomeWithoutDefaultFallback(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(fmt.Sprintf("running=%v", running), func(t *testing.T) {
			defaultHome, isolatedHome := t.TempDir(), t.TempDir()
			t.Setenv("CODEX_HOME", defaultHome)
			ordinary, isolated := newFakeAppServer(t), newFakeAppServer(t)
			ordinary.addThread(testThreadID, false, true)
			isolated.addThread(testThreadID, false, true)
			binary := writeHomeAwareCodex(t, isolatedHome, isolated.socketPath, ordinary.socketPath, running)
			adapter := New("isolated-node", Config{CodexPath: binary, CodexHome: isolatedHome, Stderr: io.Discard})
			t.Cleanup(func() { _ = adapter.Close() })
			if err := adapter.Register(testThreadID); err != nil {
				t.Fatal(err)
			}
			health := adapter.Health(context.Background())
			endpoints, err := adapter.ListEndpoints(context.Background())
			result := adapter.Deliver(context.Background(), agentbridge.Address{EndpointID: testThreadID}, testEnvelope("isolated"))
			if running {
				if !health.Ready || err != nil || len(endpoints) != 1 || !result.Delivered() {
					t.Errorf("configured daemon must serve adapter: health=%+v endpoints=%+v err=%v result=%+v", health, endpoints, err, result)
				}
				if countCalls(isolated.callOrder(), "turn/start") != 1 {
					t.Errorf("configured home must receive the write, got %v", isolated.callOrder())
				}
			} else if health.Ready || health.Code != agentbridge.ResultStartRequired || err == nil || result.Code != agentbridge.ResultStartRequired {
				t.Errorf("absent configured daemon must not fall back: health=%+v err=%v result=%+v", health, err, result)
			}
			if calls := ordinary.callOrder(); len(calls) != 0 {
				t.Errorf("default daemon must receive no RPCs from explicit home: %v", calls)
			}
			if os.Getenv("CODEX_HOME") != defaultHome {
				t.Fatal("adapter must not mutate the process environment")
			}
			state, err := DetectDaemon(context.Background(), binary)
			if err != nil || !state.Running || state.SocketPath != ordinary.socketPath {
				t.Errorf("public default probe must keep inherited home: %+v, %v", state, err)
			}
		})
	}
}

func TestDaemonProbeUsesConfiguredHomeWhenSocketOmitted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	binary := writeHomeAwareCodex(t, home, "", "/wrong-default-socket", true)
	state, err := detectDaemon(context.Background(), binary, home)
	if err != nil || !state.Running || state.SocketPath != controlSocketPath(home) {
		t.Fatalf("socket fallback must follow configured home: %+v, %v", state, err)
	}
}

func writeHomeAwareCodex(t *testing.T, home, scopedSocket, defaultSocket string, running bool) string {
	t.Helper()
	version := func(socket string) string {
		payload, err := json.Marshal(daemonVersionOutput{
			Status: "running", SocketPath: socket, CLIVersion: "0.158.0", AppServerVersion: "0.158.0",
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(payload)
	}
	path := filepath.Join(t.TempDir(), "codex")
	scopedOutput := "exit 1"
	if running {
		scopedOutput = "cat <<'JSON'\n" + version(scopedSocket) + "\nJSON"
	}
	quotedHome := "'" + strings.ReplaceAll(home, "'", "'\"'\"'") + "'"
	payload := "#!/bin/sh\nif [ \"$CODEX_HOME\" = " + quotedHome + " ]; then\n" + scopedOutput + "\nelse\ncat <<'JSON'\n" + version(defaultSocket) + "\nJSON\nfi\n"
	if runtime.GOOS == "windows" {
		path += ".cmd"
		scopedOutput = "exit /b 1"
		if running {
			scopedOutput = "echo " + version(scopedSocket)
		}
		payload = "@echo off\r\nif \"%CODEX_HOME%\"==\"" + home + "\" (\r\n" + scopedOutput + "\r\n) else (\r\necho " + version(defaultSocket) + "\r\n)\r\n"
	}
	if err := os.WriteFile(path, []byte(payload), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
