package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ceasarXuu/RA2A/internal/control"
	"github.com/ceasarXuu/RA2A/internal/operator"
)

type Backend interface {
	ListTargets(context.Context) ([]control.Target, error)
	Send(context.Context, control.SendRequest) error
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Meta      map[string]any  `json:"_meta"`
}

func Serve(ctx context.Context, input io.Reader, output io.Writer, backend Backend) error {
	decoder := json.NewDecoder(input)
	encoder := json.NewEncoder(output)
	for {
		var message request
		if err := decoder.Decode(&message); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode MCP request: %w", err)
		}
		if len(message.ID) == 0 {
			continue
		}
		var result any
		switch message.Method {
		case "initialize":
			result = initializeResult()
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": productionTools()}
		case "tools/call":
			var params callParams
			if err := json.Unmarshal(message.Params, &params); err != nil {
				result = toolError("INVALID_REQUEST", err)
			} else {
				result = callTool(ctx, backend, params)
			}
		default:
			if err := encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": message.ID,
				"error": map[string]any{"code": -32601, "message": "method not found"},
			}); err != nil {
				return err
			}
			continue
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result}); err != nil {
			return err
		}
	}
}

func initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{"tools": map[string]bool{"listChanged": false}},
		"serverInfo":      map[string]string{"name": "ra2a", "version": operator.Version},
	}
}

func productionTools() []any {
	return []any{
		map[string]any{
			"name":        "list_targets",
			"description": "List discovered RA2A nodes, their availability status, and current or stale Codex sessions.",
			"inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{}, "additionalProperties": false,
			},
		},
		map[string]any{
			"name":        "send_message",
			"description": "Send a text message to a target ra2a://node/session address. Accepted means a remote turn was created, not completed.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to":   map[string]string{"type": "string", "description": "Target address: ra2a://node-id/session-id"},
					"text": map[string]string{"type": "string", "description": "Message text"},
				},
				"required": []string{"to", "text"}, "additionalProperties": false,
			},
		},
	}
}

func callTool(ctx context.Context, backend Backend, params callParams) any {
	switch params.Name {
	case "list_targets":
		targets, err := backend.ListTargets(ctx)
		if err != nil {
			return toolError(errorCode(err), err)
		}
		return toolSuccess(map[string]any{"targets": targets})
	case "send_message":
		var arguments struct {
			To   string `json:"to"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(params.Arguments, &arguments); err != nil || arguments.To == "" || arguments.Text == "" {
			return toolError("INVALID_REQUEST", errors.New("to and text are required"))
		}
		threadID, _ := params.Meta["threadId"].(string)
		if threadID == "" {
			return toolError("CALLER_SESSION_UNKNOWN", errors.New("MCP call metadata did not include threadId"))
		}
		err := backend.Send(ctx, control.SendRequest{
			To: arguments.To, Text: arguments.Text, SourceSessionID: threadID,
		})
		if err != nil {
			return toolError(errorCode(err), err)
		}
		return toolSuccess(map[string]any{"status": "accepted", "to": arguments.To})
	default:
		return toolError("UNKNOWN_TOOL", errors.New("unknown tool"))
	}
}

func toolSuccess(structured map[string]any) map[string]any {
	text, _ := json.Marshal(structured)
	return map[string]any{
		"isError": false, "structuredContent": structured,
		"content": []any{map[string]string{"type": "text", "text": string(text)}},
	}
}

func toolError(code string, err error) map[string]any {
	message := code
	if err != nil && err.Error() != code {
		message += ": " + err.Error()
	}
	return map[string]any{
		"isError": true, "structuredContent": map[string]any{"error": code},
		"content": []any{map[string]string{"type": "text", "text": message}},
	}
}

func errorCode(err error) string {
	for _, code := range []string{
		"SESSION_BUSY", "DELIVERY_UNKNOWN", "TARGET_NOT_FOUND", "INVALID_REQUEST", "DAEMON_UNAVAILABLE",
	} {
		if strings.Contains(err.Error(), code) {
			return code
		}
	}
	return "DELIVERY_FAILED"
}
