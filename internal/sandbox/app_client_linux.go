//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

type App struct{ NodeID, Directory string }

func (a *App) Kind() agentbridge.AgentKind               { return agentbridge.AgentCodexApp }
func (a *App) Health(context.Context) agentbridge.Health { return agentbridge.Ready() }
func (a *App) Close() error                              { return nil }
func (a *App) client(socket string) *Adapter {
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &Adapter{client: &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("app redirect refused") }}}
}
func (a *App) list(ctx context.Context, socket string) ([]agentbridge.Endpoint, error) {
	client := a.client(socket)
	defer client.client.CloseIdleConnections()
	var endpoints []agentbridge.Endpoint
	err := client.post(ctx, "/endpoints", nil, &endpoints)
	return endpoints, err
}
func (a *App) ListEndpoints(ctx context.Context) ([]agentbridge.Endpoint, error) {
	sockets, err := filepath.Glob(filepath.Join(a.Directory, "*.sock"))
	if err != nil {
		return nil, err
	}
	var endpoints []agentbridge.Endpoint
	for _, socket := range sockets {
		value, e := a.list(ctx, socket)
		if e == nil {
			endpoints = append(endpoints, value...)
		}
	}
	return endpoints, nil
}
func (a *App) Deliver(ctx context.Context, address agentbridge.Address, envelope agentbridge.MessageEnvelope) agentbridge.DeliveryResult {
	sockets, _ := filepath.Glob(filepath.Join(a.Directory, "*.sock"))
	for _, socket := range sockets {
		endpoints, err := a.list(ctx, socket)
		if err != nil {
			continue
		}
		for _, endpoint := range endpoints {
			if endpoint.ID != address.EndpointID {
				continue
			}
			client := a.client(socket)
			defer client.client.CloseIdleConnections()
			var result agentbridge.DeliveryResult
			if client.post(ctx, "/deliver", map[string]string{"ID": endpoint.ID, "Text": agentbridge.RenderIncomingText(envelope)}, &result) != nil || !result.Code.Valid() {
				return unknown("app_rpc_unknown")
			}
			return result
		}
	}
	return agentbridge.DeliveryResult{Code: agentbridge.ResultNotFound}
}
