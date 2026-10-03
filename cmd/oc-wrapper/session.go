package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// approveRequests answers only prompts issued for this attached session. An
// explicit deny never produces a permission.asked event, preserving deny rules.
func approveRequests(ctx context.Context, baseURL, sessionID string, ready chan<- error, stderr io.Writer) {
	approveCurrentRequests(ctx, baseURL, func() string { return sessionID }, ready, stderr)
}

func approveCurrentRequests(ctx context.Context, baseURL string, current func() string, ready chan<- error, stderr io.Writer) {
	for {
		connected := false
		err := streamApprovalRequests(ctx, baseURL, current, ready, stderr, &connected)
		if ready != nil {
			if !connected {
				return // initial handshake failed; run() has the error
			}
			ready = nil
		}
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintf(stderr, "opencode_permission_stream_dropped session=%s error=%v\n", current(), err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func streamApprovalRequests(ctx context.Context, baseURL string, current func() string, ready chan<- error, stderr io.Writer, connected *bool) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/event", nil)
	if err != nil {
		if ready != nil {
			ready <- err
		}
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		if ready != nil {
			ready <- err
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("OpenCode event stream: %s", response.Status)
		if ready != nil {
			ready <- err
		}
		return err
	}
	*connected = true
	if ready != nil {
		ready <- nil
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type       string `json:"type"`
			Properties struct {
				ID        string `json:"id"`
				SessionID string `json:"sessionID"`
			} `json:"properties"`
		}
		sessionID := current()
		if sessionID == "" || json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) != nil ||
			event.Properties.SessionID != sessionID || event.Properties.ID == "" {
			continue
		}
		var address string
		switch event.Type {
		case "permission.asked":
			address = strings.TrimSuffix(baseURL, "/") + "/permission/" + url.PathEscape(event.Properties.ID) + "/reply"
		case "permission.v2.asked":
			address = strings.TrimSuffix(baseURL, "/") + "/api/session/" + url.PathEscape(sessionID) + "/permission/" + url.PathEscape(event.Properties.ID) + "/reply"
		default:
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		reply, err := http.NewRequestWithContext(callCtx, http.MethodPost, address, strings.NewReader(`{"reply":"once"}`))
		if err == nil {
			reply.Header.Set("Content-Type", "application/json")
			var result *http.Response
			result, err = (&http.Client{}).Do(reply)
			if err == nil {
				_ = result.Body.Close()
				if result.StatusCode < 200 || result.StatusCode >= 300 {
					err = fmt.Errorf("HTTP %s", result.Status)
				}
			}
		}
		cancel()
		if err != nil {
			fmt.Fprintf(stderr, "opencode_permission_reply_failed session=%s request=%s error=%v\n", sessionID, event.Properties.ID, err)
		} else {
			fmt.Fprintf(stderr, "opencode_permission_auto_approved session=%s request=%s\n", sessionID, event.Properties.ID)
		}
	}
	return scanner.Err()
}
