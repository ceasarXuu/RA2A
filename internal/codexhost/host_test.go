package codexhost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestManagedCommandUsesOfficialUnixListener(t *testing.T) {
	command := managedCommand(context.Background(), Config{CodexPath: "/bin/codex", SocketPath: "/state/app.sock"})
	want := []string{"/bin/codex", "app-server", "--listen", "unix:///state/app.sock"}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("args = %#v, want %#v", command.Args, want)
	}
}

func TestManagedCommandUsesSeparateProcessGroupOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process-group contract")
	}
	command := managedCommand(context.Background(), Config{CodexPath: "/bin/codex", SocketPath: "/state/app.sock"})
	if command.SysProcAttr == nil || !reflect.ValueOf(command.SysProcAttr).Elem().FieldByName("Setpgid").Bool() {
		t.Fatalf("managed command is not isolated in its own process group: %#v", command.SysProcAttr)
	}
}

func TestUnixListenURLUsesURLSeparatorsForWindowsPath(t *testing.T) {
	if got, want := unixListenURL(`C:\Users\test\.codex\control.sock`), "unix://C:/Users/test/.codex/control.sock"; got != want {
		t.Fatalf("unixListenURL = %q, want %q", got, want)
	}
}

func TestStartLaunchesHostAfterInitialConnectFails(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	connectCalls := 0
	startCalls := 0
	connect := func(context.Context, string) (io.ReadWriteCloser, error) {
		connectCalls++
		if connectCalls == 1 {
			return nil, errors.New("socket unavailable")
		}
		return clientSide, nil
	}
	start := func(context.Context, Config) (*managedProcess, error) {
		startCalls++
		return &managedProcess{done: make(chan struct{})}, nil
	}
	serveHostProtocol(t, serverSide, []rpcExchange{
		{method: "initialize", result: map[string]any{}},
		{method: "thread/list", result: map[string]any{
			"data":       []any{map[string]any{"id": "thread-1", "name": "Managed", "status": map[string]any{"type": "idle"}}},
			"nextCursor": nil,
		}},
	})

	host, err := startWith(context.Background(), Config{SocketPath: "/managed.sock"}, start, connect, time.Millisecond)
	if err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer host.Close()
	threads, err := host.ListThreadSummaries(context.Background())
	if err != nil {
		t.Fatalf("list threads: %v", err)
	}
	if startCalls != 1 || connectCalls != 2 || len(threads) != 1 || threads[0].ID != "thread-1" {
		t.Fatalf("start=%d connect=%d threads=%#v", startCalls, connectCalls, threads)
	}
}

func TestStartDoesNotAttachToExistingSocket(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	startCalls := 0
	connectCalls := 0
	start := func(context.Context, Config) (*managedProcess, error) {
		startCalls++
		return &managedProcess{done: make(chan struct{})}, nil
	}
	connect := func(context.Context, string) (io.ReadWriteCloser, error) {
		connectCalls++
		return clientSide, nil
	}
	serveHostProtocol(t, serverSide, []rpcExchange{{method: "initialize", result: map[string]any{}}})

	host, err := startWith(context.Background(), Config{SocketPath: "/existing.sock"}, start, connect, time.Millisecond)
	if err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer host.Close()
	if startCalls != 1 || connectCalls != 1 {
		t.Fatalf("start calls = %d, connect calls = %d", startCalls, connectCalls)
	}
}

func TestManagedSocketPathIsUniquePerLaunch(t *testing.T) {
	first := uniqueSocketPath("/state/app-server-control.sock", 100)
	second := uniqueSocketPath("/state/app-server-control.sock", 101)
	if first == second || first == "/state/app-server-control.sock" || second == "/state/app-server-control.sock" {
		t.Fatalf("socket paths are not unique: %q %q", first, second)
	}
}

func TestOwnerRecordPathIsStableAcrossLaunches(t *testing.T) {
	if got, want := ownerRecordPath("/state/app-server-control.sock"), "/state/app-server-control.sock.ra2a-owner.json"; got != want {
		t.Fatalf("owner path = %q, want %q", got, want)
	}
}

func TestCleanupOwnerRecordKillsOnlyRecordedManagedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable process fixture unavailable")
	}
	root := t.TempDir()
	ownerPath := filepath.Join(root, "owner.json")
	socketPath := filepath.Join(root, "ra2a.sock")
	command := exec.Command("sleep", "60")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	if err := writeOwnerRecord(ownerPath, ownerRecord{PID: command.Process.Pid, SocketPath: socketPath}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupOwnerRecord(ownerPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("owner record still exists: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket still exists: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("recorded process was not terminated")
	}
}

func TestSendMessageResumesThenStartsTurnOnManagedHost(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	connect := func(context.Context, string) (io.ReadWriteCloser, error) { return clientSide, nil }
	start := func(context.Context, Config) (*managedProcess, error) {
		return &managedProcess{done: make(chan struct{})}, nil
	}
	serveHostProtocol(t, serverSide, []rpcExchange{
		{method: "initialize", result: map[string]any{}},
		{method: "thread/resume", result: map[string]any{"thread": map[string]any{"id": "thread-1"}}},
		{method: "turn/start", result: map[string]any{"turn": map[string]any{"id": "turn-1"}}},
	})
	host, err := startWith(context.Background(), Config{}, start, connect, time.Millisecond)
	if err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer host.Close()
	if err := host.SendMessage(context.Background(), "thread-1", "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}
}

func TestSendMessageMapsActiveWriterToSessionBusy(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	connect := func(context.Context, string) (io.ReadWriteCloser, error) { return clientSide, nil }
	start := func(context.Context, Config) (*managedProcess, error) {
		return &managedProcess{done: make(chan struct{})}, nil
	}
	serveHostProtocol(t, serverSide, []rpcExchange{
		{method: "initialize", result: map[string]any{}},
		{method: "thread/resume", rpcError: map[string]any{"code": -32600, "message": "thread already has an active writer"}},
	})
	host, err := startWith(context.Background(), Config{}, start, connect, time.Millisecond)
	if err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer host.Close()
	if err := host.SendMessage(context.Background(), "thread-1", "hello"); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("error = %v", err)
	}
}

type rpcExchange struct {
	method   string
	result   any
	rpcError any
}

func serveHostProtocol(t *testing.T, connection net.Conn, exchanges []rpcExchange) {
	t.Helper()
	go func() {
		decoder := json.NewDecoder(bufio.NewReader(connection))
		encoder := json.NewEncoder(connection)
		for _, exchange := range exchanges {
			var request map[string]any
			if err := decoder.Decode(&request); err != nil {
				return
			}
			if request["method"] != exchange.method {
				t.Errorf("method = %v, want %s", request["method"], exchange.method)
				return
			}
			response := map[string]any{"id": request["id"], "result": exchange.result}
			if exchange.rpcError != nil {
				response = map[string]any{"id": request["id"], "error": exchange.rpcError}
			}
			if err := encoder.Encode(response); err != nil {
				return
			}
			if exchange.method == "initialize" {
				var initialized map[string]any
				if err := decoder.Decode(&initialized); err != nil || initialized["method"] != "initialized" {
					t.Errorf("initialized notification = %#v, err=%v", initialized, err)
					return
				}
			}
		}
	}()
}
