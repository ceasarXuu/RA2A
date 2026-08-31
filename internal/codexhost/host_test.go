package codexhost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"reflect"
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

func TestStartAttachesWithoutLaunchingWhenHostExists(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	startCalls := 0
	start := func(context.Context, Config) (*managedProcess, error) {
		startCalls++
		return nil, errors.New("must not launch")
	}
	connect := func(context.Context, string) (io.ReadWriteCloser, error) { return clientSide, nil }
	serveHostProtocol(t, serverSide, []rpcExchange{{method: "initialize", result: map[string]any{}}})

	host, err := startWith(context.Background(), Config{SocketPath: "/existing.sock"}, start, connect, time.Millisecond)
	if err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer host.Close()
	if startCalls != 0 {
		t.Fatalf("start calls = %d", startCalls)
	}
}

func TestSendMessageResumesThenStartsTurnOnManagedHost(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	connect := func(context.Context, string) (io.ReadWriteCloser, error) { return clientSide, nil }
	serveHostProtocol(t, serverSide, []rpcExchange{
		{method: "initialize", result: map[string]any{}},
		{method: "thread/resume", result: map[string]any{"thread": map[string]any{"id": "thread-1"}}},
		{method: "turn/start", result: map[string]any{"turn": map[string]any{"id": "turn-1"}}},
	})
	host, err := startWith(context.Background(), Config{}, nil, connect, time.Millisecond)
	if err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer host.Close()
	if err := host.SendMessage(context.Background(), "thread-1", "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}
}

type rpcExchange struct {
	method string
	result any
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
			if err := encoder.Encode(map[string]any{"id": request["id"], "result": exchange.result}); err != nil {
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
