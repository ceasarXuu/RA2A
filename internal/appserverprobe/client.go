package appserverprobe

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type Client struct {
	decoder        *json.Decoder
	encoder        *json.Encoder
	nextID         int64
	experimentalAP bool
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

// NewMessageID returns a client-generated stable ID suitable for
// clientUserMessageId in thread/queue/add and turn/start.
func NewMessageID() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("ra2a-%d", time.Now().UnixNano())
	}
	return "ra2a-" + hex.EncodeToString(random)
}

func New(input io.Reader, output io.Writer) *Client {
	return &Client{decoder: json.NewDecoder(input), encoder: json.NewEncoder(output)}
}

// NewExperimental returns a client that negotiates capabilities.experimentalApi
// during initialize. The Codex CLI delivery surface (thread/queue/*) rejects
// clients that do not declare it; the Desktop path does not need it.
func NewExperimental(input io.Reader, output io.Writer) *Client {
	client := New(input, output)
	client.experimentalAP = true
	return client
}

func (client *Client) Initialize() error {
	_, err := client.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "ra2a", "title": "RA2A", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": client.experimentalAP},
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

func (client *Client) ResolveThreadModel(threadID string) (string, error) {
	result, err := client.call("thread/read", map[string]any{
		"threadId": threadID, "includeTurns": false,
	})
	if err != nil {
		return "", fmt.Errorf("read thread model: %w", err)
	}
	var threadResult struct {
		Thread struct {
			Model string `json:"model"`
			Path  string `json:"path"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &threadResult); err != nil {
		return "", fmt.Errorf("decode thread/read response: %w", err)
	}
	if model := strings.TrimSpace(threadResult.Thread.Model); model != "" {
		return model, nil
	}
	if model, err := rolloutModel(threadResult.Thread.Path); err == nil {
		return model, nil
	}
	return "", errors.New("thread has no model: set a model in Codex Desktop and retry")
}

func (client *Client) InjectMessage(threadID, text string) (json.RawMessage, error) {
	if _, err := client.call("thread/resume", map[string]string{"threadId": threadID}); err != nil {
		return nil, fmt.Errorf("resume thread: %w", err)
	}
	return client.StartTurn(threadID, text)
}

// ReadAccountRateLimits returns the account usage snapshot used by the TUI
// rate-limit warnings and the switch-model prompt. Read-only diagnostic.
func (client *Client) ReadAccountRateLimits() (json.RawMessage, error) {
	return client.call("account/rateLimits/read", map[string]any{})
}

// QueuedSubmission is the stable identity the app-server assigns to a queued
// user turn; field names follow the V5 contract projection and are read
// defensively until V8 pins the exact response shape.
type QueuedSubmission struct {
	ID string
}

// QueueMessage persists a user turn for FIFO submission when the thread next
// becomes idle. It is the CLI delivery surface: it never creates a second
// writer and queues during an active turn (V7 semantics).
func (client *Client) QueueMessage(threadID, messageID, text string) (QueuedSubmission, error) {
	result, err := client.call("thread/queue/add", map[string]any{
		"threadId":            threadID,
		"clientUserMessageId": messageID,
		"input":               []map[string]string{{"type": "text", "text": text}},
	})
	if err != nil {
		return QueuedSubmission{}, fmt.Errorf("queue message: %w", err)
	}
	return decodeQueuedSubmission(result)
}

func decodeQueuedSubmission(result json.RawMessage) (QueuedSubmission, error) {
	var decoded struct {
		QueuedSubmission struct {
			ID string `json:"id"`
		} `json:"queuedSubmission"`
		Submission struct {
			ID string `json:"id"`
		} `json:"submission"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(result, &decoded); err != nil {
		return QueuedSubmission{}, fmt.Errorf("decode queued submission: %w", err)
	}
	submission := decoded.QueuedSubmission.ID
	if submission == "" {
		submission = decoded.Submission.ID
	}
	if submission == "" {
		submission = decoded.ID
	}
	return QueuedSubmission{ID: submission}, nil
}

func (client *Client) StartEphemeralThread(cwd string) (string, error) {
	threadID, _, err := client.StartEphemeralThreadDetails(cwd)
	return threadID, err
}

func (client *Client) StartEphemeralThreadDetails(cwd string) (string, json.RawMessage, error) {
	result, err := client.call("thread/start", map[string]any{"cwd": cwd, "ephemeral": true})
	if err != nil {
		return "", nil, err
	}
	var decoded struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &decoded); err != nil {
		return "", nil, fmt.Errorf("decode thread/start response: %w", err)
	}
	if decoded.Thread.ID == "" {
		return "", nil, errors.New("thread/start response did not include a thread ID")
	}
	return decoded.Thread.ID, result, nil
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
