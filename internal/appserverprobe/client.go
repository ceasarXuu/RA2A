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
	Error  *RPCError       `json:"error"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (err *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", err.Code, err.Message)
}

type ThreadSummary struct {
	ID     string
	Title  string
	Status string
}

const maxThreadTitleBytes = 160

func New(input io.Reader, output io.Writer) *Client {
	return &Client{decoder: json.NewDecoder(input), encoder: json.NewEncoder(output)}
}

func (client *Client) Initialize() error {
	_, err := client.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "ra2a", "title": "RA2A", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": false},
	})
	if err != nil {
		return err
	}
	if err := client.encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]any{},
	}); err != nil {
		return fmt.Errorf("encode initialized notification: %w", err)
	}
	return nil
}

func (client *Client) ListThreads(sourceKinds []string) (json.RawMessage, error) {
	params := map[string]any{"archived": false, "limit": 100, "useStateDbOnly": true}
	if sourceKinds != nil {
		params["sourceKinds"] = sourceKinds
	}
	return client.call("thread/list", params)
}

func (client *Client) ListThreadSummaries() ([]ThreadSummary, error) {
	sourceKinds := []string{
		"cli", "vscode", "exec", "appServer", "subAgent", "subAgentReview",
		"subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown",
	}
	var summaries []ThreadSummary
	var cursor any
	for {
		params := map[string]any{
			"archived":       false,
			"limit":          100,
			"sourceKinds":    sourceKinds,
			"useStateDbOnly": true,
		}
		if cursor != nil {
			params["cursor"] = cursor
		}
		result, err := client.call("thread/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Data []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Preview string `json:"preview"`
				Status  struct {
					Type string `json:"type"`
				} `json:"status"`
			} `json:"data"`
			NextCursor any `json:"nextCursor"`
		}
		if err := json.Unmarshal(result, &page); err != nil {
			return nil, fmt.Errorf("decode thread/list response: %w", err)
		}
		for _, thread := range page.Data {
			title := thread.Name
			if title == "" {
				title = thread.Preview
			}
			summaries = append(summaries, ThreadSummary{
				ID: thread.ID, Title: boundedTitle(title), Status: thread.Status.Type,
			})
		}
		if page.NextCursor == nil {
			return summaries, nil
		}
		cursor = page.NextCursor
	}
}

func boundedTitle(title string) string {
	if len(title) <= maxThreadTitleBytes {
		return title
	}
	const suffix = "…"
	limit := maxThreadTitleBytes - len(suffix)
	end := 0
	for offset := range title {
		if offset > limit {
			break
		}
		end = offset
	}
	return title[:end] + suffix
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
			return nil, fmt.Errorf("%s: %w", method, message.Error)
		}
		return message.Result, nil
	}
}
