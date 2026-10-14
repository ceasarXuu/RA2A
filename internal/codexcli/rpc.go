package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const rpcHandshakeURL = "ws://localhost/"

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (err *rpcError) Error() string {
	return fmt.Sprintf("app-server error %d: %s", err.Code, err.Message)
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type notificationHandler func(method string, params json.RawMessage)

// rpcConn speaks the app-server JSON-RPC dialect over a WebSocket carried on an
// AF_UNIX socket. `codex app-server proxy` cannot be used for this: it is a raw
// byte relay, so the caller still has to speak the WebSocket framing itself.
type rpcConn struct {
	conn    *websocket.Conn
	write   sync.Mutex
	nextID  int64
	mu      sync.Mutex
	waiters map[string]chan rpcMessage
	notify  notificationHandler
	closed  chan struct{}
	once    sync.Once
	readErr error
}

func dialRPC(ctx context.Context, socketPath string, handshakeTimeout time.Duration) (*rpcConn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: handshakeTimeout,
		NetDial: func(string, string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	header := http.Header{}
	conn, response, err := dialer.DialContext(ctx, rpcHandshakeURL, header)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("app-server handshake failed with %s: %w", response.Status, err)
		}
		return nil, fmt.Errorf("app-server handshake failed: %w", err)
	}
	client := &rpcConn{
		conn:    conn,
		waiters: make(map[string]chan rpcMessage),
		closed:  make(chan struct{}),
	}
	go client.readLoop()
	return client, nil
}

func (client *rpcConn) readLoop() {
	for {
		_, payload, err := client.conn.ReadMessage()
		if err != nil {
			client.fail(err)
			return
		}
		var message rpcMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			continue
		}
		switch {
		case message.Method != "" && len(message.ID) == 0:
			client.mu.Lock()
			notify := client.notify
			client.mu.Unlock()
			if notify != nil {
				notify(message.Method, message.Params)
			}
		case len(message.ID) > 0:
			key := string(message.ID)
			client.mu.Lock()
			waiter := client.waiters[key]
			delete(client.waiters, key)
			client.mu.Unlock()
			if waiter != nil {
				waiter <- message
				close(waiter)
			}
		}
	}
}

func (client *rpcConn) fail(err error) {
	client.once.Do(func() {
		client.mu.Lock()
		client.readErr = err
		waiters := client.waiters
		client.waiters = make(map[string]chan rpcMessage)
		close(client.closed)
		client.mu.Unlock()
		for _, waiter := range waiters {
			close(waiter)
		}
	})
}

func (client *rpcConn) Close() error {
	client.fail(errors.New("connection closed"))
	return client.conn.Close()
}

func (client *rpcConn) call(ctx context.Context, method string, params any, result any) error {
	if params == nil {
		params = map[string]any{}
	}
	client.mu.Lock()
	select {
	case <-client.closed:
		err := client.readErr
		client.mu.Unlock()
		return fmt.Errorf("%s: app-server connection closed: %v", method, err)
	default:
	}
	client.nextID++
	id := client.nextID
	key := fmt.Sprintf("%d", id)
	waiter := make(chan rpcMessage, 1)
	client.waiters[key] = waiter
	client.mu.Unlock()

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		client.forget(key)
		return err
	}
	client.write.Lock()
	err = client.conn.WriteMessage(websocket.TextMessage, payload)
	client.write.Unlock()
	if err != nil {
		client.forget(key)
		client.fail(err)
		_ = client.conn.Close()
		return fmt.Errorf("send %s: %w", method, err)
	}
	select {
	case <-ctx.Done():
		client.forget(key)
		return ctx.Err()
	case <-client.closed:
		return fmt.Errorf("%s: app-server connection closed: %v", method, client.readErr)
	case message, open := <-waiter:
		if !open {
			return fmt.Errorf("%s: app-server connection closed: %v", method, client.readErr)
		}
		if message.Error != nil {
			return message.Error
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(message.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	}
}

func (client *rpcConn) notifyServer(method string, params any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	client.write.Lock()
	defer client.write.Unlock()
	return client.conn.WriteMessage(websocket.TextMessage, payload)
}

func (client *rpcConn) forget(key string) {
	client.mu.Lock()
	delete(client.waiters, key)
	client.mu.Unlock()
}

func (client *rpcConn) setNotificationHandler(handler notificationHandler) {
	client.mu.Lock()
	client.notify = handler
	client.mu.Unlock()
}

type initializeResult struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

func (client *rpcConn) initialize(ctx context.Context, clientName, clientVersion string, experimental bool) (initializeResult, error) {
	var result initializeResult
	params := map[string]any{
		"clientInfo": map[string]any{"name": clientName, "version": clientVersion},
	}
	if experimental {
		params["capabilities"] = map[string]any{"experimentalApi": true}
	}
	if err := client.call(ctx, "initialize", params, &result); err != nil {
		return initializeResult{}, err
	}
	if err := client.notifyServer("initialized", map[string]any{}); err != nil {
		return initializeResult{}, fmt.Errorf("send initialized: %w", err)
	}
	return result, nil
}
