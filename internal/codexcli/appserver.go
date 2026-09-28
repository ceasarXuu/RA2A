package codexcli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// threadIDPattern matches the identifiers the app-server accepts: a bare UUID or
// the same UUID behind a `urn:uuid:` prefix. Anything else is rejected
// synchronously with "invalid thread id", so the adapter refuses to send it.
var threadIDPattern = regexp.MustCompile(`^(urn:uuid:)?[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validThreadID(threadID string) bool { return threadIDPattern.MatchString(threadID) }

type threadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

type threadRecord struct {
	ID                   string        `json:"id"`
	SessionID            string        `json:"sessionId"`
	Source               string        `json:"source"`
	Originator           string        `json:"originator"`
	CLIVersion           string        `json:"cliVersion"`
	Model                string        `json:"model"`
	Name                 string        `json:"name"`
	Preview              string        `json:"preview"`
	Path                 string        `json:"path"`
	Cwd                  string        `json:"cwd"`
	Ephemeral            bool          `json:"ephemeral"`
	CanAcceptDirectInput *bool         `json:"canAcceptDirectInput"`
	Status               *threadStatus `json:"status"`
}

func (thread threadRecord) loaded() bool { return thread.Status != nil }

func (thread threadRecord) active() bool {
	return thread.Status != nil && thread.Status.Type == "active"
}

func (thread threadRecord) acceptsDirectInput() bool {
	return thread.CanAcceptDirectInput != nil && *thread.CanAcceptDirectInput
}

func (thread threadRecord) displayTitle() string {
	if title := strings.TrimSpace(thread.Name); title != "" {
		return title
	}
	if preview := strings.TrimSpace(thread.Preview); preview != "" {
		return truncateRunes(preview, 160)
	}
	return thread.ID
}

type turnError struct {
	Message string `json:"message"`
}

type turnRecord struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Error  *turnError `json:"error"`
}

type threadListResponse struct {
	Data []threadRecord `json:"data"`
	Next *string        `json:"nextCursor"`
	Back *string        `json:"backwardsCursor"`
}

type loadedListResponse struct {
	Data []string `json:"data"`
	Next *string  `json:"nextCursor"`
}

type threadResponse struct {
	Thread threadRecord `json:"thread"`
}

type turnResponse struct {
	Turn turnRecord `json:"turn"`
}

type turnSteerResponse struct {
	TurnID string `json:"turnId"`
}

type turnsListResponse struct {
	Data []struct {
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Items  []threadItemRef `json:"items"`
	} `json:"data"`
}

type threadItemRef struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type unsubscribeResponse struct {
	Status string `json:"status"`
}

type turnCompletedParams struct {
	ThreadID string     `json:"threadId"`
	Turn     turnRecord `json:"turn"`
}

type threadStatusChangedParams struct {
	ThreadID string        `json:"threadId"`
	Status   *threadStatus `json:"status"`
}

type appServer struct {
	conn *rpcConn
}

func (server *appServer) threadLoadedList(ctx context.Context) ([]string, error) {
	var response loadedListResponse
	if err := server.conn.call(ctx, "thread/loaded/list", map[string]any{}, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (server *appServer) threadRead(ctx context.Context, threadID string) (threadRecord, error) {
	var response threadResponse
	err := server.conn.call(ctx, "thread/read", map[string]any{"threadId": threadID}, &response)
	return response.Thread, err
}

func (server *appServer) threadStart(ctx context.Context, cwd, model string) (threadRecord, error) {
	params := map[string]any{"cwd": cwd, "approvalPolicy": "never", "sandbox": "danger-full-access"}
	if model != "" {
		params["model"] = model
	}
	var response threadResponse
	err := server.conn.call(ctx, "thread/start", params, &response)
	return response.Thread, err
}

func (server *appServer) threadList(ctx context.Context) ([]threadRecord, error) {
	var response threadListResponse
	if err := server.conn.call(ctx, "thread/list", map[string]any{}, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (server *appServer) threadResume(ctx context.Context, threadID string) (threadRecord, error) {
	var response threadResponse
	err := server.conn.call(ctx, "thread/resume", map[string]any{"threadId": threadID}, &response)
	return response.Thread, err
}

func (server *appServer) threadUnsubscribe(ctx context.Context, threadID string) (string, error) {
	var response unsubscribeResponse
	err := server.conn.call(ctx, "thread/unsubscribe", map[string]any{"threadId": threadID}, &response)
	return response.Status, err
}

func (server *appServer) threadTurnsList(ctx context.Context, threadID string) ([]string, error) {
	var response turnsListResponse
	if err := server.conn.call(ctx, "thread/turns/list", map[string]any{"threadId": threadID}, &response); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(response.Data))
	for _, turn := range response.Data {
		ids = append(ids, turn.ID)
	}
	return ids, nil
}

// userInput keeps `textElements` explicit. The protocol marks it optional, but
// the Desktop IPC regression showed the renderer is sensitive to its absence.
func userInput(text string) []map[string]any {
	return []map[string]any{{"type": "text", "text": text, "textElements": []any{}}}
}

func (server *appServer) turnStart(ctx context.Context, threadID, text string) (turnRecord, error) {
	var response turnResponse
	err := server.conn.call(ctx, "turn/start",
		map[string]any{"threadId": threadID, "input": userInput(text)}, &response)
	return response.Turn, err
}

func (server *appServer) turnSteer(ctx context.Context, threadID, expectedTurnID, text string) (turnRecord, error) {
	var response turnSteerResponse
	err := server.conn.call(ctx, "turn/steer",
		map[string]any{"threadId": threadID, "expectedTurnId": expectedTurnID, "input": userInput(text)},
		&response)
	if err != nil {
		return turnRecord{}, err
	}
	if response.TurnID == "" {
		return turnRecord{}, errors.New("turn/steer response omitted turnId")
	}
	return turnRecord{ID: response.TurnID}, nil
}

func classifyRPCError(err error) string {
	if err == nil {
		return ""
	}
	var rpcErr *rpcError
	if errors.As(err, &rpcErr) {
		switch {
		case strings.Contains(rpcErr.Message, "invalid thread id"),
			strings.Contains(rpcErr.Message, "invalid session id"):
			return "invalid_thread_id"
		case strings.Contains(rpcErr.Message, "thread not found"),
			strings.Contains(rpcErr.Message, "no rollout found"),
			strings.Contains(rpcErr.Message, "thread not loaded"),
			strings.Contains(rpcErr.Message, "failed to read thread"):
			return "thread_not_found"
		case strings.Contains(rpcErr.Message, "requires experimentalApi capability"):
			return "capability_rejected"
		case strings.Contains(rpcErr.Message, "thread already has an active or pending turn"):
			return "turn_active"
		case strings.Contains(rpcErr.Message, "not allowed for multi-agent"):
			return "subagent_input_rejected"
		}
		return fmt.Sprintf("rpc_%d", rpcErr.Code)
	}
	return "transport"
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
