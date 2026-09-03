package desktopipc

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const maxFrameBytes = 256 * 1024 * 1024

type envelope struct {
	Type           string         `json:"type"`
	RequestID      string         `json:"requestId,omitempty"`
	SourceClientID string         `json:"sourceClientId,omitempty"`
	Version        int            `json:"version,omitempty"`
	Method         string         `json:"method,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
	ResultType     string         `json:"resultType,omitempty"`
	Result         map[string]any `json:"result,omitempty"`
	Error          any            `json:"error,omitempty"`
	Request        map[string]any `json:"request,omitempty"`
	Response       map[string]any `json:"response,omitempty"`
}

type Client struct {
	conn     net.Conn
	clientID string
}

type TurnResult struct {
	TurnID string
}

type textInput struct {
	Type         string `json:"type"`
	Text         string `json:"text"`
	TextElements []any  `json:"text_elements"`
}

type DeliveryUnknownError struct {
	Cause error
}

type NotDeliveredError struct {
	Cause error
}

type requestRejectedError struct {
	Method string
	Cause  any
}

type noActiveTurnError struct {
	Cause error
}

func (err *DeliveryUnknownError) Error() string {
	return fmt.Sprintf("desktop IPC delivery result is unknown: %v", err.Cause)
}

func (err *DeliveryUnknownError) Unwrap() error { return err.Cause }

func (err *NotDeliveredError) Error() string {
	return fmt.Sprintf("Desktop IPC request was not delivered: %v", err.Cause)
}

func (err *NotDeliveredError) Unwrap() error { return err.Cause }

func (err *requestRejectedError) Error() string {
	return fmt.Sprintf("Desktop IPC %s error: %v", err.Method, err.Cause)
}

func (err *noActiveTurnError) Error() string {
	return fmt.Sprintf("Desktop IPC has no active turn to steer: %v", err.Cause)
}

func (err *noActiveTurnError) Unwrap() error { return err.Cause }

func IsDeliveryUnknown(err error) bool {
	var target *DeliveryUnknownError
	return errors.As(err, &target)
}

func IsNotDelivered(err error) bool {
	var target *NotDeliveredError
	return errors.As(err, &target)
}

func New(conn net.Conn) *Client {
	return &Client{conn: conn}
}

func NewMessageID() string {
	return newRequestID()
}

func (client *Client) Initialize(ctx context.Context) error {
	result, err := client.call(ctx, envelope{
		Type:           "request",
		SourceClientID: "initializing-client",
		Version:        1,
		Method:         "initialize",
		Params:         map[string]any{"clientType": "ra2a-bridge"},
	})
	if err != nil {
		return fmt.Errorf("initialize Desktop IPC: %w", err)
	}
	client.clientID = stringField(result, "clientId")
	if client.clientID == "" {
		return errors.New("initialize Desktop IPC: response did not include clientId")
	}
	return nil
}

func (client *Client) StartTurn(
	ctx context.Context,
	threadID string,
	text string,
	messageID string,
) (TurnResult, error) {
	if client.clientID == "" {
		return TurnResult{}, &NotDeliveredError{Cause: errors.New("Desktop IPC client is not initialized")}
	}
	if err := client.synchronizeThreadSettings(ctx, threadID); err != nil {
		return TurnResult{}, &NotDeliveredError{Cause: fmt.Errorf("synchronize Desktop thread settings: %w", err)}
	}
	request := envelope{
		Type:           "request",
		SourceClientID: client.clientID,
		Version:        2,
		Method:         "thread-follower-start-turn",
		Params: map[string]any{
			"conversationId": threadID,
			"turnStart": map[string]any{
				"request": map[string]any{
					"threadId":            threadID,
					"input":               []textInput{newTextInput(text)},
					"clientUserMessageId": messageID,
				},
				"context": map[string]any{"inheritThreadSettings": true},
			},
		},
	}
	result, err := client.call(ctx, request)
	if err != nil && isEmptyModelRejection(err) {
		// The follower handler enters startTurn directly, while the normal UI
		// path waits for pending thread-settings updates first. An explicit
		// empty-model rejection is safe to retry after that private barrier.
		if barrierErr := client.synchronizeThreadSettings(ctx, threadID); barrierErr != nil {
			return TurnResult{}, &NotDeliveredError{Cause: fmt.Errorf("wait for Desktop thread settings before retry: %w", barrierErr)}
		}
		result, err = client.call(ctx, request)
	}
	if err != nil {
		var rejected *requestRejectedError
		if errors.As(err, &rejected) {
			return TurnResult{}, &NotDeliveredError{Cause: fmt.Errorf("start Desktop-owned turn: %w", err)}
		}
		return TurnResult{}, &DeliveryUnknownError{Cause: fmt.Errorf("start Desktop-owned turn: %w", err)}
	}
	if nested, ok := result["result"].(map[string]any); ok {
		result = nested
	}
	turn, _ := result["turn"].(map[string]any)
	turnID := stringField(turn, "id")
	if turnID == "" {
		return TurnResult{}, &DeliveryUnknownError{Cause: errors.New("Desktop IPC accepted start turn without a turn ID")}
	}
	return TurnResult{TurnID: turnID}, nil
}

func (client *Client) synchronizeThreadSettings(ctx context.Context, threadID string) error {
	_, err := client.call(ctx, envelope{
		Type:           "request",
		SourceClientID: client.clientID,
		Version:        1,
		Method:         "thread-follower-update-thread-settings",
		Params: map[string]any{
			"conversationId": threadID,
			"threadSettings": map[string]any{},
		},
	})
	return err
}

func (client *Client) SendMessage(
	ctx context.Context,
	threadID string,
	text string,
	messageID string,
) (TurnResult, error) {
	result, err := client.steerTurn(ctx, threadID, text, messageID)
	if err == nil {
		return result, nil
	}
	var inactive *noActiveTurnError
	if !errors.As(err, &inactive) {
		return TurnResult{}, err
	}
	return client.StartTurn(ctx, threadID, text, messageID)
}

func (client *Client) steerTurn(
	ctx context.Context,
	threadID string,
	text string,
	messageID string,
) (TurnResult, error) {
	if client.clientID == "" {
		return TurnResult{}, &NotDeliveredError{Cause: errors.New("Desktop IPC client is not initialized")}
	}
	result, err := client.call(ctx, envelope{
		Type:           "request",
		SourceClientID: client.clientID,
		Version:        1,
		Method:         "thread-follower-steer-turn",
		Params: map[string]any{
			"conversationId":      threadID,
			"input":               []textInput{newTextInput(text)},
			"clientUserMessageId": messageID,
			"serviceTier":         nil,
			"attachments":         []any{},
			"additionalContext":   nil,
			"toolOutput":          nil,
			"restoreMessage": map[string]any{
				"id":        messageID,
				"text":      text,
				"createdAt": time.Now().UnixMilli(),
				"context": map[string]any{
					"prompt":             text,
					"workspaceRoots":     []any{},
					"commentAttachments": []any{},
					"fileAttachments":    []any{},
					"imageAttachments":   []any{},
					"addedFiles":         []any{},
				},
			},
		},
	})
	if err != nil {
		var rejected *requestRejectedError
		if errors.As(err, &rejected) {
			wrapped := fmt.Errorf("steer Desktop-owned turn: %w", err)
			if isInactiveSteerRejection(rejected.Cause) {
				return TurnResult{}, &noActiveTurnError{Cause: wrapped}
			}
			return TurnResult{}, &NotDeliveredError{Cause: wrapped}
		}
		return TurnResult{}, &DeliveryUnknownError{Cause: fmt.Errorf("steer Desktop-owned turn: %w", err)}
	}
	if nested, ok := result["result"].(map[string]any); ok {
		result = nested
	}
	turnID := stringField(result, "turnId")
	if turnID == "" {
		return TurnResult{}, &DeliveryUnknownError{Cause: errors.New("Desktop IPC accepted steer turn without a turn ID")}
	}
	return TurnResult{TurnID: turnID}, nil
}

func newTextInput(text string) textInput {
	return textInput{Type: "text", Text: text, TextElements: []any{}}
}

func isInactiveSteerRejection(cause any) bool {
	message := strings.ToLower(fmt.Sprint(cause))
	return strings.Contains(message, "no active turn") ||
		strings.Contains(message, "steerturninactiveerror") ||
		strings.Contains(message, "active turn already ended")
}

func isEmptyModelRejection(err error) bool {
	var rejected *requestRejectedError
	if !errors.As(err, &rejected) {
		return false
	}
	message := strings.ToLower(fmt.Sprint(rejected.Cause))
	return strings.Contains(message, "the '' model is not supported") ||
		strings.Contains(message, `the "" model is not supported`)
}

func (client *Client) call(ctx context.Context, request envelope) (map[string]any, error) {
	request.RequestID = newRequestID()
	if deadline, ok := ctx.Deadline(); ok {
		if err := client.conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
		defer client.conn.SetDeadline(time.Time{})
	}
	if err := writeFrame(client.conn, request); err != nil {
		return nil, err
	}
	for {
		response, err := readFrame(client.conn)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if response.Type == "client-discovery-request" {
			if err := writeFrame(client.conn, envelope{
				Type:      "client-discovery-response",
				RequestID: response.RequestID,
				Response:  map[string]any{"canHandle": false},
			}); err != nil {
				return nil, err
			}
			continue
		}
		if response.Type != "response" || response.RequestID != request.RequestID {
			continue
		}
		if response.ResultType == "error" || response.Error != nil {
			return nil, &requestRejectedError{Method: request.Method, Cause: response.Error}
		}
		return response.Result, nil
	}
}

func writeFrame(writer io.Writer, value envelope) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(len(body)))
	if err := writeAll(writer, header); err != nil {
		return err
	}
	return writeAll(writer, body)
}

func readFrame(reader io.Reader) (envelope, error) {
	var result envelope
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return result, err
	}
	length := binary.LittleEndian.Uint32(header)
	if length > maxFrameBytes {
		return result, fmt.Errorf("Desktop IPC frame is too large: %d bytes", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("decode Desktop IPC frame: %w", err)
	}
	return result, nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func newRequestID() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("ra2a-%d", time.Now().UnixNano())
	}
	return "ra2a-" + hex.EncodeToString(random)
}

func stringField(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}
