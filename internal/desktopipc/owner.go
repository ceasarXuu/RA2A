package desktopipc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// FindThreadOwner queries the live Desktop route without loading or resuming a
// thread. An empty ID with no error means Desktop explicitly reported no owner;
// transport failures, unsupported responses, and missing fields remain errors.
func (client *Client) FindThreadOwner(ctx context.Context, threadID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if client.clientID == "" {
		return "", errors.New("Desktop IPC client is not initialized")
	}
	canceled := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		_ = client.conn.SetDeadline(time.Now())
		close(canceled)
	})
	defer func() {
		if !stopCancel() {
			<-canceled
		}
		_ = client.conn.SetDeadline(time.Time{})
	}()
	response, err := client.callEnvelope(ctx, envelope{
		Type: "request", SourceClientID: client.clientID, Version: 1,
		Method: "thread-owner-discovery",
		Params: map[string]any{"hostId": "local", "conversationId": threadID},
	})
	if err != nil {
		if response.ResultType == "error" && response.Error == "no-client-found" {
			return "", nil
		}
		return "", fmt.Errorf("discover Desktop thread owner: %w", err)
	}
	ownerID := strings.TrimSpace(response.HandledByClientID)
	supported, _ := response.Result["supportsUntrustedAppInput"].(bool)
	if response.ResultType != "success" || ownerID == "" || !supported {
		return "", errors.New("Desktop owner discovery returned incomplete or unsupported ownership evidence")
	}
	return ownerID, nil
}

// SelectThreadOwner pins subsequent follower requests to a discovered owner.
// The caller must refresh ownership evidence before delivery; discovery alone
// does not change this selection. This pins only the IPC client, not the native
// writer lock, and does not disable Desktop's resume fallback. Empty clears it.
func (client *Client) SelectThreadOwner(ownerID string) {
	client.ownerID = ownerID
}
