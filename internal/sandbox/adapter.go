// Package sandbox contains opt-in test harness components, never daemon defaults.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

// Adapter reserves a trusted controller slot before any native delivery.
// The controller, not the Agent, assigns counters and remembers IDs durably.
type Adapter struct {
	agentbridge.Adapter
	client *http.Client
}

func Wrap(adapter agentbridge.Adapter, socket string) (*Adapter, error) {
	if adapter == nil || !adapter.Kind().Valid() || !filepath.IsAbs(socket) {
		return nil, fmt.Errorf("sandbox adapter requires native adapter and absolute budget socket")
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &Adapter{Adapter: adapter, client: &http.Client{Transport: transport, Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("budget redirect refused") }}}, nil
}

type reservation struct {
	Allow     bool                        `json:"allow"`
	Duplicate bool                        `json:"duplicate"`
	Error     string                      `json:"error,omitempty"`
	Result    *agentbridge.DeliveryResult `json:"result,omitempty"`
}

func (a *Adapter) post(ctx context.Context, path string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://budget"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("budget HTTP %d", response.StatusCode)
	}
	if result == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return err
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(result)
}

func unknown(class string) agentbridge.DeliveryResult {
	return agentbridge.DeliveryResult{Code: agentbridge.ResultUnknown, NativeErrorClass: class,
		Detail: "sandbox budget confirmation unavailable; do not replay"}
}

func (a *Adapter) Deliver(ctx context.Context, target agentbridge.Address, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	if envelope.ID == "" {
		return unknown("sandbox_missing_message_id")
	}
	request := struct {
		Kind agentbridge.AgentKind `json:"kind"`
		ID   string                `json:"id"`
	}{a.Kind(), envelope.ID}
	var slot reservation
	if err := a.post(ctx, "/reserve", request, &slot); err != nil {
		return unknown("sandbox_budget_unavailable")
	}
	if slot.Duplicate {
		if slot.Result != nil && slot.Result.Code.Valid() {
			return *slot.Result
		}
		return unknown("sandbox_duplicate_unresolved")
	}
	if !slot.Allow || slot.Error != "" {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultUnsupported, NativeErrorClass: "sandbox_budget_exhausted",
			Detail: "该类 Agent 本轮接收预算已用完；含启动、工具回信及失败预留，输入未送入模型，请查看控制器账本"}
	}
	result := a.Adapter.Deliver(ctx, target, envelope)
	// Results may contain native errors with credentials or input text. The ledger
	// needs only the verdict and native turn identity, never arbitrary detail.
	safe := agentbridge.DeliveryResult{Code: result.Code, TurnID: result.TurnID}
	finish := struct {
		Kind   agentbridge.AgentKind      `json:"kind"`
		ID     string                     `json:"id"`
		Result agentbridge.DeliveryResult `json:"result"`
	}{a.Kind(), envelope.ID, safe}
	finishContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.post(finishContext, "/finish", finish, nil); err != nil {
		return unknown("sandbox_finish_unconfirmed")
	}
	return result
}

func (a *Adapter) ResolveCaller(ctx context.Context, caller agentbridge.CallerContext) (agentbridge.Address, error) {
	if resolver, ok := a.Adapter.(agentbridge.CallerResolver); ok {
		return resolver.ResolveCaller(ctx, caller)
	}
	return agentbridge.Address{}, agentbridge.CallerHint("sandbox adapter requires an explicit published from address")
}

func (a *Adapter) CheckTargetID(id string) error {
	if checker, ok := a.Adapter.(agentbridge.TargetShapeChecker); ok {
		return checker.CheckTargetID(id)
	}
	return nil
}

func (a *Adapter) Close() error { a.client.CloseIdleConnections(); return a.Adapter.Close() }
