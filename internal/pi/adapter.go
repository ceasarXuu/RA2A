// Package pi connects live native Pi extensions to the common agent registry.
package pi

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

//go:embed extension.mjs
var Extension []byte

var sessionID = regexp.MustCompile(`^pi\.[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type lease struct {
	SessionID string                     `json:"sessionID"`
	PID       int                        `json:"pid"`
	Expires   int64                      `json:"expires"`
	URL       string                     `json:"url"`
	Token     string                     `json:"token"`
	Status    agentbridge.EndpointStatus `json:"status"`
	Title     string                     `json:"title"`
}

type Adapter struct {
	nodeID, directory string
	client            *http.Client
}

func Directory() string {
	if value := os.Getenv("RA2A_PI_SESSION_DIR"); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ra2a", "pi-sessions")
}

func New(nodeID, directory string) *Adapter {
	return &Adapter{nodeID, directory, &http.Client{Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("Pi bridge redirect refused") },
		Transport:     &http.Transport{Proxy: nil}}}
}
func (*Adapter) Kind() agentbridge.AgentKind               { return agentbridge.AgentPi }
func (a *Adapter) Close() error                            { a.client.CloseIdleConnections(); return nil }
func (*Adapter) Health(context.Context) agentbridge.Health { return agentbridge.Ready() }

func (a *Adapter) leases() map[string]lease {
	records := make(map[string]lease)
	counts := make(map[string]int)
	files, _ := os.ReadDir(a.directory)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(a.directory, file.Name()))
		var value lease
		if err != nil || json.Unmarshal(data, &value) != nil || !sessionID.MatchString(value.SessionID) {
			continue
		}
		parsed, err := url.Parse(value.URL)
		port, _ := strconv.Atoi(parsedPort(parsed))
		now := time.Now().UnixMilli()
		if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || port < 1 || port > 65535 || len(value.Token) != 64 || value.PID < 1 || file.Name() != fmt.Sprintf("attachment.%d.json", value.PID) || value.Expires <= now || value.Expires > now+10000 {
			continue
		}
		if value.Status != agentbridge.EndpointReady && value.Status != agentbridge.EndpointBusy {
			continue
		}
		counts[value.SessionID]++
		records[value.SessionID] = value
	}
	for id, count := range counts {
		if count != 1 {
			delete(records, id)
		}
	}
	return records
}
func parsedPort(value *url.URL) string {
	if value == nil {
		return ""
	}
	return value.Port()
}

func (a *Adapter) ListEndpoints(context.Context) ([]agentbridge.Endpoint, error) {
	endpoints := []agentbridge.Endpoint{}
	for id, value := range a.leases() {
		endpoints = append(endpoints, agentbridge.Endpoint{ID: id, Agent: agentbridge.AgentPi, NativeSessionID: id[3:], Title: value.Title, Status: value.Status,
			Address: agentbridge.Address{NodeID: a.nodeID, EndpointID: id}, Capabilities: []agentbridge.Capability{agentbridge.CapabilityReceiveText, agentbridge.CapabilityReplyAddress}})
	}
	return endpoints, nil
}

func (a *Adapter) Deliver(ctx context.Context, address agentbridge.Address, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	value, ok := a.leases()[address.EndpointID]
	if !ok {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultNotFound, Detail: "Pi extension is absent or ownership is ambiguous"}
	}
	payload, _ := json.Marshal(map[string]string{"sessionID": address.EndpointID, "id": envelope.ID, "text": agentbridge.RenderIncomingText(envelope)})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, value.URL+"/receive", bytes.NewReader(payload))
	if err != nil {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnknown, Detail: err.Error()}
	}
	request.Header.Set("Authorization", "Bearer "+value.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnknown, Detail: "Pi bridge receipt was not confirmed"}
	}
	defer response.Body.Close()
	var receipt struct {
		Status    string `json:"status"`
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK || readErr != nil || json.Unmarshal(data, &receipt) != nil || receipt.Status != "received_by_bridge" || receipt.ID != envelope.ID || receipt.SessionID != address.EndpointID {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnknown, Detail: "Pi bridge returned no matching receipt"}
	}
	return agentbridge.DeliveryResult{Code: agentbridge.ResultDelivered, Detail: "Pi extension received; model execution is independent"}
}
