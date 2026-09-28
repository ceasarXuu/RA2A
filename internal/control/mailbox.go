package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/mailbox"
)

type mailboxRequest struct {
	To   string `json:"to"`
	Text string `json:"text"`
	From string `json:"from,omitempty"`
}

type mailboxReadRequest struct {
	To    string `json:"to"`
	Limit int    `json:"limit,omitempty"`
	Peek  bool   `json:"peek,omitempty"`
}

type mailboxReadResponse struct {
	Recipient string            `json:"recipient"`
	Pending   int               `json:"pending"`
	Messages  []mailbox.Message `json:"messages"`
}

// RegisterMailboxRoutes exposes the mailbox to adapted agents and to un-adapted
// harnesses alike. It is mounted on the loopback control plane, so reading a
// mailbox requires the same local access that sending already requires.
func RegisterMailboxRoutes(mux *http.ServeMux, store *mailbox.Store, nodeID string) {
	if store == nil {
		return
	}
	writeJSON := func(writer http.ResponseWriter, status int, value any) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(value)
	}
	mux.HandleFunc("POST /v1/mailbox", func(writer http.ResponseWriter, request *http.Request) {
		var payload mailboxRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "INVALID_REQUEST"})
			return
		}
		if !mailbox.ValidRecipient(payload.To) || payload.Text == "" {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "INVALID_REQUEST"})
			return
		}
		id, err := newMessageID()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "MESSAGE_ID_FAILED"})
			return
		}
		from := payload.From
		if from == "" {
			from = "ra2a://" + nodeID + "/local"
		}
		message, err := store.Deliver(payload.To, from, payload.Text, id, timeNow())
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, mailbox.ErrBadMessage) {
				status = http.StatusBadRequest
			}
			writeJSON(writer, status, map[string]string{"error": "MAILBOX_REJECTED"})
			return
		}
		// A mailbox delivery is complete the moment it is stored: no agent is
		// contacted, so there is no turn to wait for.
		writeJSON(writer, http.StatusOK, map[string]any{
			"status": "delivered", "delivery": "stored", "id": message.ID,
		})
	})
	mux.HandleFunc("POST /v1/mailbox/read", func(writer http.ResponseWriter, request *http.Request) {
		var payload mailboxReadRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "INVALID_REQUEST"})
			return
		}
		messages, pending, err := store.Read(payload.To, payload.Limit, payload.Peek)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "INVALID_REQUEST"})
			return
		}
		if messages == nil {
			messages = []mailbox.Message{}
		}
		writeJSON(writer, http.StatusOK, mailboxReadResponse{
			Recipient: payload.To, Pending: pending, Messages: messages,
		})
	})
	mux.HandleFunc("GET /v1/mailbox", func(writer http.ResponseWriter, request *http.Request) {
		recipient := request.URL.Query().Get("to")
		if !mailbox.ValidRecipient(recipient) {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "INVALID_REQUEST"})
			return
		}
		limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
		messages, pending, err := store.Read(recipient, limit, request.URL.Query().Get("peek") == "true")
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "MAILBOX_UNAVAILABLE"})
			return
		}
		if messages == nil {
			messages = []mailbox.Message{}
		}
		writeJSON(writer, http.StatusOK, mailboxReadResponse{
			Recipient: recipient, Pending: pending, Messages: messages,
		})
	})
	mux.HandleFunc("GET /v1/mailboxes", func(writer http.ResponseWriter, request *http.Request) {
		names, err := store.Recipients()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "MAILBOX_UNAVAILABLE"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"mailboxes": names})
	})
}

// DeliverMailbox stores a message addressed to a mailbox on this node. It is the
// path LAN delivery takes before any adapter lookup, because a mailbox is not a
// conversation endpoint and must never reach an agent.
func DeliverMailbox(store *mailbox.Store, nodeID string, envelope agentbridge.MessageEnvelope) (agentbridge.DeliveryResult, bool) {
	if store == nil {
		return agentbridge.DeliveryResult{}, false
	}
	_, recipient, ok, err := mailbox.ParseAddress(envelope.TargetAddress)
	if !ok {
		return agentbridge.DeliveryResult{}, false
	}
	if err != nil {
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnsupported, NativeErrorClass: "malformed_mailbox_address",
			Detail: err.Error(),
		}, true
	}
	from := envelope.SourceAddress
	if from == "" {
		from = "ra2a://" + nodeID + "/unknown"
	}
	id := envelope.ID
	if id == "" {
		id = envelope.TargetAddress + "-" + strconv.FormatInt(envelope.CreatedAt.UnixNano(), 36)
	}
	if _, err := store.Deliver(recipient, from, envelope.Text, id, timeNow()); err != nil {
		return agentbridge.DeliveryResult{
			Code: agentbridge.ResultUnsupported, NativeErrorClass: "mailbox_rejected",
			Detail: err.Error(),
		}, true
	}
	return agentbridge.Delivered(""), true
}

func timeNow() time.Time { return time.Now().UTC() }

// MailboxSendRequest is the client-side shape for storing a message.
type MailboxSendRequest struct {
	To   string `json:"to"`
	Text string `json:"text"`
	From string `json:"from,omitempty"`
}

// MailboxReadRequest is the client-side shape for polling a mailbox.
type MailboxReadRequest struct {
	To    string `json:"to"`
	Limit int    `json:"limit,omitempty"`
	Peek  bool   `json:"peek,omitempty"`
}

// ListMailboxes reports which mailboxes exist on the local node.
func (client *Client) ListMailboxes(ctx context.Context) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint+"/v1/mailboxes", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("DAEMON_UNAVAILABLE: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, decodeHTTPError(response.Body)
	}
	var payload struct {
		Mailboxes []string `json:"mailboxes"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Mailboxes, nil
}

// ReadMailbox polls a mailbox. This is the entry point an un-adapted harness
// uses: no adapter, no session and no model call are involved.
func (client *Client) ReadMailbox(ctx context.Context, payload MailboxReadRequest) (mailboxReadResponse, error) {
	var result mailboxReadResponse
	body, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		client.endpoint+"/v1/mailbox/read", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return result, fmt.Errorf("DAEMON_UNAVAILABLE: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, decodeHTTPError(response.Body)
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	return result, err
}

// SendMailbox stores a message in a local mailbox.
func (client *Client) SendMailbox(ctx context.Context, payload MailboxSendRequest) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		client.endpoint+"/v1/mailbox", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("DAEMON_UNAVAILABLE: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return decodeHTTPError(response.Body)
	}
	return nil
}
