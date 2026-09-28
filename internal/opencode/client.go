// Package opencode adapts local OpenCode sessions into RA2A endpoints.
//
// It attaches to an OpenCode headless server as an ordinary HTTP client and
// subscribes to its event stream, so busy/idle and turn completion are observed
// rather than guessed. The delivery response is deliberately not treated as
// confirmation: OpenCode answers with a full assistant message that is still
// in progress, and the terminal state only arrives on the event stream.
package opencode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL    = "http://127.0.0.1:4096"
	defaultCallTimout = 30 * time.Second
	defaultIdleWait   = 120 * time.Second
)

type Config struct {
	BaseURL     string
	ClientName  string
	HTTPClient  *http.Client
	CallTimeout time.Duration
	IdleWait    time.Duration
	Stderr      io.Writer
}

type Session struct {
	ID        string
	Title     string
	Directory string
	Agent     string
	Version   string
	Updated   time.Time
}

type Message struct {
	SessionID string
	Role      string
	Completed time.Time
	Err       *MessageError
	Texts     []string
}

type MessageError struct {
	Name string `json:"name"`
}

type sessionPayload struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	Agent     string `json:"agent"`
	Version   string `json:"version"`
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

type listSessionsResponse struct {
	sessions []sessionPayload
}

type assistantMessagePayload struct {
	SessionID string `json:"sessionID"`
	Role      string `json:"role"`
	Time      struct {
		Completed int64 `json:"completed"`
	} `json:"time"`
	Error *MessageError `json:"error"`
}

type messageResponse struct {
	Info  assistantMessagePayload `json:"info"`
	Parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"parts"`
}

// Client is a thin OpenCode server client. It is intentionally minimal: the
// adapter only needs session listing, message posting and the event stream.
type Client struct {
	baseURL     string
	clientName  string
	http        *http.Client
	callTimeout time.Duration
	idleWait    time.Duration
	stderr      io.Writer

	mu       sync.Mutex
	busy     map[string]bool
	waiters  map[string]chan error
	watching bool
}

func NewClient(config Config) *Client {
	baseURL := strings.TrimSuffix(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 0}
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = defaultCallTimout
	}
	if config.IdleWait == 0 {
		config.IdleWait = defaultIdleWait
	}
	if config.ClientName == "" {
		config.ClientName = "ra2a"
	}
	return &Client{
		baseURL: baseURL, clientName: config.ClientName, http: client,
		callTimeout: config.CallTimeout, idleWait: config.IdleWait, stderr: config.Stderr,
		busy: make(map[string]bool), waiters: make(map[string]chan error),
	}
}

// BaseURL reports the server this client talks to.
func (client *Client) BaseURL() string { return client.baseURL }

func (client *Client) ListSessions(ctx context.Context) ([]Session, error) {
	body, err := client.get(ctx, "/session")
	if err != nil {
		return nil, err
	}
	var payloads []sessionPayload
	if err := json.Unmarshal(body, &payloads); err != nil {
		return nil, fmt.Errorf("decode opencode sessions: %w", err)
	}
	sessions := make([]Session, 0, len(payloads))
	for _, payload := range payloads {
		sessions = append(sessions, Session{
			ID: payload.ID, Title: payload.Title, Directory: payload.Directory,
			Agent: payload.Agent, Version: payload.Version,
			Updated: time.UnixMilli(payload.Time.Updated),
		})
	}
	return sessions, nil
}

func (client *Client) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	body, err := client.get(ctx, "/session/"+url.PathEscape(sessionID)+"/message")
	if err != nil {
		return nil, err
	}
	var entries []struct {
		Info  assistantMessagePayload `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("decode opencode messages: %w", err)
	}
	messages := make([]Message, 0, len(entries))
	for _, entry := range entries {
		message := Message{
			SessionID: entry.Info.SessionID, Role: entry.Info.Role,
			Err: entry.Info.Error,
		}
		if entry.Info.Time.Completed > 0 {
			message.Completed = time.UnixMilli(entry.Info.Time.Completed)
		}
		for _, part := range entry.Parts {
			if part.Type == "text" && part.Text != "" {
				message.Texts = append(message.Texts, part.Text)
			}
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// PostMessage appends a user message and starts a turn.
//
// Unlike Codex, OpenCode's POST blocks until the turn finishes, so a
// client-side deadline can expire while the message has already landed. A
// deadline here is therefore reported as an unknown outcome, never as a
// failure, and the caller reconciles it against the session history.
func (client *Client) PostMessage(ctx context.Context, sessionID, text string) (Message, error) {
	payload := map[string]any{"parts": []map[string]any{{"type": "text", "text": text}}}
	body, err := client.post(ctx, "/session/"+url.PathEscape(sessionID)+"/message", payload)
	if err != nil {
		return Message{}, err
	}
	var response messageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return Message{}, fmt.Errorf("decode opencode message response: %w", err)
	}
	texts := make([]string, 0, len(response.Parts))
	for _, part := range response.Parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return Message{
		SessionID: response.Info.SessionID, Role: response.Info.Role,
		Err: response.Info.Error, Texts: texts,
	}, nil
}

func (client *Client) Busy(sessionID string) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.busy[sessionID]
}

func (client *Client) get(ctx context.Context, path string) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, client.callTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencode GET %s: %s", path, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 32<<20))
}

func (client *Client) post(ctx context.Context, path string, payload any) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, client.callTimeout)
	defer cancel()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, client.baseURL+path, strings.NewReader(string(encoded)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", ErrOutcomeUnknown, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencode POST %s: %s", path, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 32<<20))
}

// ConfirmsLanded reports whether the session history contains a user message
// carrying the given marker. It is the reconciliation path for a POST whose
// outcome was unknown.
func (client *Client) ConfirmsLanded(ctx context.Context, sessionID, marker string) (bool, error) {
	messages, err := client.Messages(ctx, sessionID)
	if err != nil {
		return false, err
	}
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		for _, text := range message.Texts {
			if strings.Contains(text, marker) {
				return true, nil
			}
		}
	}
	return false, nil
}

// Watch consumes the server event stream and records busy/idle transitions. It
// returns when the context is cancelled or the stream breaks.
func (client *Client) Watch(ctx context.Context) error {
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, client.baseURL+"/event", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("opencode event stream: %s", response.Status)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		client.handleEvent(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (client *Client) handleEvent(payload string) {
	var event struct {
		Type       string `json:"type"`
		Properties struct {
			SessionID string `json:"sessionID"`
			Status    struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return
	}
	switch event.Type {
	case "session.status":
		if event.Properties.SessionID == "" {
			return
		}
		client.setBusy(event.Properties.SessionID, event.Properties.Status.Type == "busy")
	case "session.idle":
		if event.Properties.SessionID == "" {
			return
		}
		client.setBusy(event.Properties.SessionID, false)
		client.notifyIdle(event.Properties.SessionID)
	}
}

func (client *Client) setBusy(sessionID string, busy bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.busy[sessionID] = busy
}

func (client *Client) notifyIdle(sessionID string) {
	client.mu.Lock()
	waiter := client.waiters[sessionID]
	delete(client.waiters, sessionID)
	client.mu.Unlock()
	if waiter != nil {
		waiter <- nil
		close(waiter)
	}
}

// AwaitIdle waits for the session to report idle, which is the only terminal
// signal OpenCode publishes. It returns the session's last message so the caller
// can see whether the turn succeeded.
func (client *Client) AwaitIdle(ctx context.Context, sessionID string) (Message, error) {
	waiter := make(chan error, 1)
	client.mu.Lock()
	if previous, exists := client.waiters[sessionID]; exists {
		previous <- errors.New("superseded by a newer delivery")
		close(previous)
	}
	client.waiters[sessionID] = waiter
	client.mu.Unlock()
	defer func() {
		client.mu.Lock()
		if current, exists := client.waiters[sessionID]; exists && current == waiter {
			delete(client.waiters, sessionID)
		}
		client.mu.Unlock()
	}()

	timer := time.NewTimer(client.idleWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case err := <-waiter:
		if err != nil {
			return Message{}, err
		}
	case <-timer.C:
		return Message{}, errors.New("opencode session did not report idle before the deadline")
	}
	messages, err := client.Messages(ctx, sessionID)
	if err != nil {
		return Message{}, err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return messages[i], nil
		}
	}
	return Message{}, errors.New("no assistant message was produced")
}

var (
	ErrUnreachable    = errors.New("OPENCODE_UNREACHABLE")
	ErrOutcomeUnknown = errors.New("OPENCODE_OUTCOME_UNKNOWN")
)

func IsUnreachable(err error) bool { return errors.Is(err, ErrUnreachable) }

// IsOutcomeUnknown reports that the request may or may not have been applied.
func IsOutcomeUnknown(err error) bool { return errors.Is(err, ErrOutcomeUnknown) }
