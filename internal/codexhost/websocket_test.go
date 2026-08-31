package codexhost

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/gorilla/websocket"
)

func TestConnectWebSocketCarriesJSONOverUnixSocket(t *testing.T) {
	placeholder, err := os.CreateTemp(".", ".app-server-*.sock")
	if err != nil {
		t.Fatalf("reserve socket path: %v", err)
	}
	socketPath := placeholder.Name()
	_ = placeholder.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	requests := make(chan map[string]any, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, upgradeErr := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if upgradeErr != nil {
			return
		}
		defer connection.Close()
		var decoded map[string]any
		if readErr := connection.ReadJSON(&decoded); readErr != nil {
			return
		}
		requests <- decoded
		_ = connection.WriteJSON(map[string]any{"id": 7, "result": map[string]any{"ok": true}})
	})}
	go server.Serve(listener)
	defer server.Close()

	stream, err := connectWebSocket(context.Background(), socketPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer stream.Close()
	if err := json.NewEncoder(stream).Encode(map[string]any{"id": 7, "method": "thread/list"}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var response map[string]any
	if err := json.NewDecoder(stream).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	request := <-requests
	if request["method"] != "thread/list" || response["id"] != float64(7) {
		t.Fatalf("request=%#v response=%#v", request, response)
	}
}
