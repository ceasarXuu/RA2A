//go:build linux

package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/codexstdio"
	"golang.org/x/sys/unix"
)

const fixtureAppThread = "11111111-1111-4111-8111-111111111111"

func fixtureApp(t *testing.T) (AppHost, *atomic.Int32) {
	t.Helper()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "thread-writer-locks"), 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(home, "thread-writer-locks", fixtureAppThread+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	appIn, appWriter := io.Pipe()
	appReader, appOut := io.Pipe()
	nativeOut, nativeWriter := io.Pipe()
	nativeReader, nativeIn := io.Pipe()
	bridge := codexstdio.New(appIn, appOut, nativeOut, nativeIn)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = bridge.Run(ctx) }()
	t.Cleanup(bridge.Close)
	go func() { _, _ = io.Copy(io.Discard, appReader) }()
	calls := new(atomic.Int32)
	go func() {
		scanner := bufio.NewScanner(nativeReader)
		for scanner.Scan() {
			var frame struct {
				ID     json.RawMessage
				Method string
			}
			if json.Unmarshal(scanner.Bytes(), &frame) != nil || frame.Method == "initialized" {
				continue
			}
			var result any = map[string]any{}
			switch frame.Method {
			case "thread/loaded/list":
				result = map[string]any{"data": []string{fixtureAppThread}, "nextCursor": nil}
			case "thread/read":
				result = map[string]any{"thread": map[string]any{"id": fixtureAppThread, "canAcceptDirectInput": true, "status": map[string]any{"type": "idle"}}}
			case "turn/start":
				calls.Add(1)
				result = map[string]any{"turn": map[string]any{"id": "fixture-receipt"}}
			}
			_ = json.NewEncoder(nativeWriter).Encode(map[string]any{"id": frame.ID, "result": result})
		}
	}()
	_ = json.NewEncoder(appWriter).Encode(map[string]any{"id": 1, "method": "initialize"})
	// Initialize response must be consumed before original client's notification.
	for i := 0; i < 50; i++ {
		_ = json.NewEncoder(appWriter).Encode(map[string]any{"method": "initialized"})
		call, c := context.WithTimeout(ctx, 10*time.Millisecond)
		_, err = bridge.Call(call, "thread/loaded/list", map[string]any{})
		c()
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return AppHost{Bridge: bridge, PID: os.Getpid(), Start: AppProcessStart(os.Getpid()), PeerPID: os.Getpid(), PeerStart: AppProcessStart(os.Getpid()), Home: home, Executable: exe, NodeID: "fixture"}, calls
}

func TestAppHostActualWriterAndSingleNativeReceipt(t *testing.T) {
	h, calls := fixtureApp(t)
	if got := h.endpoints(context.Background()); len(got) != 1 || got[0].Agent != agentbridge.AgentCodexApp {
		t.Fatalf("endpoints: %v", got)
	}
	result := h.deliver(context.Background(), fixtureAppThread, "private marker")
	if result.Code != agentbridge.ResultDelivered || result.TurnID != "fixture-receipt" || calls.Load() != 1 {
		t.Fatalf("receipt: %v calls=%d", result, calls.Load())
	}
	h.PID++
	if len(h.endpoints(context.Background())) != 0 || h.deliver(context.Background(), fixtureAppThread, "must not write").Code != agentbridge.ResultNotFound || calls.Load() != 1 {
		t.Fatal("wrong writer was not refused")
	}
	h.PID--
	h.Start = "reused"
	if h.deliver(context.Background(), fixtureAppThread, "must not write").Code != agentbridge.ResultNotFound || calls.Load() != 1 {
		t.Fatal("reused writer accepted")
	}
	if h.deliver(context.Background(), "../../other", "must not write").Code != agentbridge.ResultNotFound {
		t.Fatal("unsafe thread ID accepted")
	}
}

func TestAppWorkerPeerStartAndSocketPermissions(t *testing.T) {
	h, _ := fixtureApp(t)
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := ListenApp(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if info, e := os.Stat(listener.Addr().String()); e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("socket permissions", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Serve(ctx, listener) }()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", listener.Addr().String())
	}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://private/endpoints")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	bad := h
	bad.PeerStart = "invalid"
	if bad.Serve(ctx, listener) == nil {
		t.Fatal("PID reuse accepted")
	}
	if err = os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = ListenApp(directory); err == nil {
		t.Fatal("public socket directory accepted")
	}
}

func TestAppSocketDoesNotAuthorizeAgentPeer(t *testing.T) {
	h, _ := fixtureApp(t)
	// The configured controller is another live process. The Agent test caller
	// shares the UID and sees the socket but does not have that kernel peer PID.
	h.PeerPID = os.Getppid()
	h.PeerStart = AppProcessStart(h.PeerPID)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := ListenApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Serve(ctx, listener) }()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", listener.Addr().String())
	}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://private/endpoints")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("same-UID Agent accepted: %d", response.StatusCode)
	}
}
