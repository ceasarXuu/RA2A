package appserverproxyprobe

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestServeObservesThreadOwnershipWhileRelaying(t *testing.T) {
	tempDir, err := os.MkdirTemp(filepath.Join("..", "..", ".cache"), "proxy-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	upstreamPath := filepath.Join(tempDir, "upstream.sock")
	proxyPath := filepath.Join(tempDir, "proxy.sock")
	upstreamListener, err := net.Listen("unix", upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamListener.Close()

	upstream := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, upgradeErr := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if upgradeErr != nil {
			return
		}
		defer connection.Close()
		for {
			messageType, payload, readErr := connection.ReadMessage()
			if readErr != nil {
				return
			}
			response := []byte(`{"id":0,"result":{}}`)
			if string(payload) != `{"id":0,"method":"initialize","params":{"clientInfo":{"name":"codex_cli_rs"}}}` {
				response = []byte(`{"id":7,"result":{"thread":{"id":"thread-1","source":"vscode"}}}`)
			}
			if writeErr := connection.WriteMessage(messageType, response); writeErr != nil {
				return
			}
		}
	})}
	go upstream.Serve(upstreamListener)
	defer upstream.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan Event, 1)
	errors := make(chan error, 1)
	go func() {
		errors <- Serve(ctx, proxyPath, upstreamPath, func(event Event) { events <- event })
	}()

	connection := dialUnixWebSocketForTest(t, proxyPath)
	defer connection.Close()
	writeAndReadForTest(t, connection, `{"id":0,"method":"initialize","params":{"clientInfo":{"name":"codex_cli_rs"}}}`)
	writeAndReadForTest(t, connection, `{"id":7,"method":"thread/start","params":{}}`)

	select {
	case event := <-events:
		if event.Client.Name != "codex_cli_rs" || event.ThreadID != "thread-1" {
			t.Fatalf("event = %#v", event)
		}
	case err := <-errors:
		t.Fatalf("proxy stopped early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ownership event")
	}
}

func dialUnixWebSocketForTest(t *testing.T, socketPath string) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	deadline := time.Now().Add(5 * time.Second)
	for {
		connection, response, err := dialer.Dial("ws://localhost/", nil)
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if err == nil {
			return connection
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeAndReadForTest(t *testing.T, connection *websocket.Conn, payload string) {
	t.Helper()
	if err := connection.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := connection.ReadMessage(); err != nil {
		t.Fatal(err)
	}
}
