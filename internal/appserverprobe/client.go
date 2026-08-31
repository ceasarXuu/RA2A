package appserverprobe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Client struct {
	decoder *json.Decoder
	encoder *json.Encoder
	nextID  int64
}

type response struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func New(input io.Reader, output io.Writer) *Client {
	return &Client{decoder: json.NewDecoder(input), encoder: json.NewEncoder(output)}
}

func (client *Client) Initialize() error {
	_, err := client.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "ra2a-appserver-probe", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": false},
	})
	return err
}

func (client *Client) ListThreads(sourceKinds []string) (json.RawMessage, error) {
	params := map[string]any{"archived": false, "limit": 100, "useStateDbOnly": true}
	if sourceKinds != nil {
		params["sourceKinds"] = sourceKinds
	}
	return client.call("thread/list", params)
}

func (client *Client) StartTurn(threadID, text string) (json.RawMessage, error) {
	return client.call("turn/start", map[string]any{
		"threadId": threadID,
		"input":    []map[string]string{{"type": "text", "text": text}},
	})
}

func (client *Client) InjectMessage(threadID, text string) (json.RawMessage, error) {
	if _, err := client.call("thread/resume", map[string]string{"threadId": threadID}); err != nil {
		return nil, fmt.Errorf("resume thread: %w", err)
	}
	return client.StartTurn(threadID, text)
}

func (client *Client) StartEphemeralThread(cwd string) (string, error) {
	result, err := client.call("thread/start", map[string]any{"cwd": cwd, "ephemeral": true})
	if err != nil {
		return "", err
	}
	var decoded struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &decoded); err != nil {
		return "", fmt.Errorf("decode thread/start response: %w", err)
	}
	if decoded.Thread.ID == "" {
		return "", errors.New("thread/start response did not include a thread ID")
	}
	return decoded.Thread.ID, nil
}

func (client *Client) CallMCPTool(threadID, server, tool string, arguments any) (json.RawMessage, error) {
	if _, err := client.call("thread/resume", map[string]string{"threadId": threadID}); err != nil {
		return nil, fmt.Errorf("resume thread: %w", err)
	}
	return client.call("mcpServer/tool/call", map[string]any{
		"threadId":  threadID,
		"server":    server,
		"tool":      tool,
		"arguments": arguments,
	})
}

func (client *Client) call(method string, params any) (json.RawMessage, error) {
	client.nextID++
	id := client.nextID
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := client.encoder.Encode(request); err != nil {
		return nil, fmt.Errorf("encode %s request: %w", method, err)
	}

	for {
		var message response
		if err := client.decoder.Decode(&message); err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("%s: server closed before response", method)
			}
			return nil, fmt.Errorf("decode %s response: %w", method, err)
		}
		if len(message.ID) == 0 {
			continue
		}
		var responseID int64
		if err := json.Unmarshal(message.ID, &responseID); err != nil || responseID != id {
			continue
		}
		if message.Error != nil {
			return nil, fmt.Errorf("%s: rpc error %d: %s", method, message.Error.Code, message.Error.Message)
		}
		return message.Result, nil
	}
}
