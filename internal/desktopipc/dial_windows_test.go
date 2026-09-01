//go:build windows

package desktopipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

func TestDialContextConnectsWindowsNamedPipe(t *testing.T) {
	pipePath, listener := listenTestPipe(t)

	serverDone := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer connection.Close()
		buffer := make([]byte, 4)
		if _, readErr := io.ReadFull(connection, buffer); readErr != nil {
			serverDone <- readErr
			return
		}
		if string(buffer) != "ping" {
			serverDone <- fmt.Errorf("request = %q", buffer)
			return
		}
		_, writeErr := connection.Write([]byte("pong"))
		serverDone <- writeErr
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, connectedPath, err := DialContext(ctx, pipePath)
	if err != nil {
		t.Fatalf("dial pipe: %v", err)
	}
	defer connection.Close()
	if connectedPath != pipePath {
		t.Fatalf("connected path = %q, want %q", connectedPath, pipePath)
	}
	if _, err := connection.Write([]byte("ping")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(connection, response); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(response) != "pong" {
		t.Fatalf("response = %q", response)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestDialContextHonorsCanceledContextOnWindows(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pipePath := uniquePipePath()
	connection, _, err := DialContext(ctx, pipePath)
	if connection != nil {
		connection.Close()
		t.Fatal("dial returned a connection for canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestDialContextReportsMissingWindowsNamedPipe(t *testing.T) {
	pipePath := uniquePipePath()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, _, err := DialContext(ctx, pipePath)
	if connection != nil {
		connection.Close()
		t.Fatal("dial returned a connection without a pipe server")
	}
	if err == nil || !strings.Contains(err.Error(), "connect Desktop IPC") || !strings.Contains(err.Error(), pipePath) {
		t.Fatalf("error = %v", err)
	}
}

func TestDialContextTimesOutWhenWindowsNamedPipeIsBusy(t *testing.T) {
	pipePath, listener := listenTestPipe(t)
	accepted := make(chan net.Conn, 1)
	acceptErrors := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			acceptErrors <- err
			return
		}
		accepted <- connection
	}()
	firstCtx, firstCancel := context.WithTimeout(context.Background(), time.Second)
	defer firstCancel()
	first, _, err := DialContext(firstCtx, pipePath)
	if err != nil {
		t.Fatalf("occupy pipe: %v", err)
	}
	defer first.Close()
	var server net.Conn
	select {
	case server = <-accepted:
		defer server.Close()
	case err := <-acceptErrors:
		t.Fatalf("accept first connection: %v", err)
	case <-firstCtx.Done():
		t.Fatalf("accept first connection: %v", firstCtx.Err())
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer timeoutCancel()
	second, _, err := DialContext(timeoutCtx, pipePath)
	if second != nil {
		second.Close()
		t.Fatal("second dial unexpectedly connected to busy pipe")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestWindowsNamedPipeConnectionSupportsDeadline(t *testing.T) {
	pipePath, listener := listenTestPipe(t)
	releaseServer := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		<-releaseServer
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, _, err := DialContext(ctx, pipePath)
	if err != nil {
		t.Fatalf("dial pipe: %v", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	_, err = connection.Read(make([]byte, 1))
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("read error = %v, want timeout", err)
	}
	close(releaseServer)
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestWindowsNamedPipePeerObservesClose(t *testing.T) {
	pipePath, listener := listenTestPipe(t)
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		_, err = io.ReadAll(connection)
		serverDone <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, _, err := DialContext(ctx, pipePath)
	if err != nil {
		t.Fatalf("dial pipe: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server read after close: %v", err)
	}
}

func TestClientProtocolThroughWindowsNamedPipe(t *testing.T) {
	pipePath, listener := listenTestPipe(t)
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		initialize, err := readFrame(connection)
		if err != nil {
			serverDone <- err
			return
		}
		if initialize.Method != "initialize" || initialize.Version != 1 {
			serverDone <- fmt.Errorf("initialize = %#v", initialize)
			return
		}
		if err := writeFrame(connection, envelope{
			Type:      "client-discovery-request",
			RequestID: "discovery-1",
		}); err != nil {
			serverDone <- err
			return
		}
		discovery, err := readFrame(connection)
		if err != nil {
			serverDone <- err
			return
		}
		if discovery.Type != "client-discovery-response" || discovery.RequestID != "discovery-1" {
			serverDone <- fmt.Errorf("discovery response = %#v", discovery)
			return
		}
		if err := writeFrame(connection, envelope{
			Type:       "response",
			RequestID:  initialize.RequestID,
			ResultType: "success",
			Result:     map[string]any{"clientId": "windows-desktop-client"},
		}); err != nil {
			serverDone <- err
			return
		}
		start, err := readFrame(connection)
		if err != nil {
			serverDone <- err
			return
		}
		if start.Method != "thread-follower-start-turn" || start.Version != 2 || start.SourceClientID != "windows-desktop-client" {
			serverDone <- fmt.Errorf("start turn = %#v", start)
			return
		}
		assertStartTurnParams(t, start.Params, "thread-windows", "hello through pipe", "message-windows")
		serverDone <- writeFrame(connection, envelope{
			Type:       "response",
			RequestID:  start.RequestID,
			ResultType: "success",
			Result: map[string]any{
				"result": map[string]any{"turn": map[string]any{"id": "turn-windows"}},
			},
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, _, err := DialContext(ctx, pipePath)
	if err != nil {
		t.Fatalf("dial pipe: %v", err)
	}
	defer connection.Close()
	client := New(connection)
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	result, err := client.StartTurn(ctx, "thread-windows", "hello through pipe", "message-windows")
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	if result.TurnID != "turn-windows" {
		t.Fatalf("turn id = %q", result.TurnID)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestClientReportsWindowsPipeServerClosingEarly(t *testing.T) {
	pipePath, listener := listenTestPipe(t)
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			err = connection.Close()
		}
		serverDone <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, _, err := DialContext(ctx, pipePath)
	if err != nil {
		t.Fatalf("dial pipe: %v", err)
	}
	defer connection.Close()
	if err := New(connection).Initialize(ctx); err == nil {
		t.Fatal("initialize succeeded after server closed")
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func listenTestPipe(t *testing.T) (string, net.Listener) {
	t.Helper()
	pipePath := uniquePipePath()
	listener, err := winio.ListenPipe(pipePath, nil)
	if err != nil {
		t.Fatalf("listen pipe: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return pipePath, listener
}

func uniquePipePath() string {
	return fmt.Sprintf(`\\.\pipe\ra2a-desktop-ipc-%d-%d`, os.Getpid(), time.Now().UnixNano())
}
