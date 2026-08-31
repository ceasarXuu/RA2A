package mcpcontext

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type callParams struct {
	Name string         `json:"name"`
	Meta map[string]any `json:"_meta"`
}

func Serve(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(input)
	encoder := json.NewEncoder(output)
	for {
		var message request
		if err := decoder.Decode(&message); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode request: %w", err)
		}
		if len(message.ID) == 0 {
			continue
		}

		switch message.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if params.ProtocolVersion == "" {
				params.ProtocolVersion = "2025-06-18"
			}
			if err := encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": message.ID,
				"result": map[string]any{
					"protocolVersion": params.ProtocolVersion,
					"capabilities":    map[string]any{"tools": map[string]bool{"listChanged": false}},
					"serverInfo":      map[string]string{"name": "ra2a-mcp-context-probe", "version": "0.1.0"},
				},
			}); err != nil {
				return err
			}
		case "ping":
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{}}); err != nil {
				return err
			}
		case "tools/list":
			tool := map[string]any{
				"name":        "probe_context",
				"description": "Reports MCP metadata keys and thread/session identity candidates for RA2A feasibility testing.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
			}
			if err := encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": message.ID,
				"result": map[string]any{"tools": []any{tool}},
			}); err != nil {
				return err
			}
		case "tools/call":
			var params callParams
			if err := json.Unmarshal(message.Params, &params); err != nil {
				return fmt.Errorf("decode tools/call params: %w", err)
			}
			if params.Name != "probe_context" {
				if err := encoder.Encode(map[string]any{
					"jsonrpc": "2.0", "id": message.ID,
					"result": map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": "unknown tool"}}},
				}); err != nil {
					return err
				}
				continue
			}
			report := metadataReport(params.Meta)
			text, err := json.Marshal(report)
			if err != nil {
				return err
			}
			if err := encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": message.ID,
				"result": map[string]any{
					"isError": false, "structuredContent": report,
					"content": []any{map[string]string{"type": "text", "text": string(text)}},
				},
			}); err != nil {
				return err
			}
		default:
			if err := encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": message.ID,
				"error": map[string]any{"code": -32601, "message": "method not found"},
			}); err != nil {
				return err
			}
		}
	}
}

func metadataReport(meta map[string]any) map[string]any {
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	candidates := map[string]string{}
	collectIdentityCandidates("", meta, candidates)
	return map[string]any{
		"hasIdentity":        len(candidates) > 0,
		"metaKeys":           keys,
		"identityCandidates": candidates,
	}
}

func collectIdentityCandidates(prefix string, value any, candidates map[string]string) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	for key, child := range object {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
		if text, ok := child.(string); ok && (strings.Contains(normalized, "thread") || strings.Contains(normalized, "session")) {
			candidates[path] = text
		}
		collectIdentityCandidates(path, child, candidates)
	}
}
