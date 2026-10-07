package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ceasarXuu/RA2A/internal/lannode"
	"github.com/ceasarXuu/RA2A/internal/mailbox"
)

const DefaultEndpoint = "http://127.0.0.1:47321"
const targetProbeTimeout = 8 * time.Second
const peerRefreshTimeout = 2 * time.Second

var ErrInvalidRequest = errors.New("INVALID_REQUEST")
var ErrTargetNotFound = errors.New("TARGET_NOT_FOUND")
var ErrTargetUnreachable = errors.New("TARGET_UNREACHABLE")
var ErrDeliveryUnknown = errors.New("DELIVERY_UNKNOWN")
var ErrDesktopOwnerUnavailable = errors.New("DESKTOP_OWNER_UNAVAILABLE")

type Target struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Status        string            `json:"status"`
	SessionsStale bool              `json:"sessionsStale"`
	Sessions      []lannode.Session `json:"sessions"`
}

type SendRequest struct {
	To   string         `json:"to"`
	Text string         `json:"text"`
	From string         `json:"from,omitempty"`
	Meta map[string]any `json:"meta,omitempty"`
	// SourceSessionID is the legacy caller field. It is still accepted so an
	// older local MCP client keeps working, but new callers send From.
	SourceSessionID string `json:"sourceSessionId,omitempty"`
	MessageID       string `json:"messageId,omitempty"`
}

type Backend interface {
	ListTargets(context.Context) ([]Target, error)
	Send(context.Context, SendRequest) error
}

type LAN interface {
	Peers() []lannode.Peer
	Peer(string) (lannode.Peer, bool)
	RefreshPeer(context.Context, string, string) (lannode.Peer, error)
	ListSessions(context.Context, lannode.Peer) ([]lannode.Session, error)
	SendMessage(context.Context, lannode.Peer, lannode.Message) error
}

type Coordinator struct {
	localID  string
	lan      LAN
	cacheMu  sync.RWMutex
	sessions map[string][]lannode.Session
}

func NewCoordinator(localID string, lan LAN) *Coordinator {
	return &Coordinator{localID: localID, lan: lan, sessions: make(map[string][]lannode.Session)}
}

func (coordinator *Coordinator) ListTargets(ctx context.Context) ([]Target, error) {
	type probeResult struct {
		peer   lannode.Peer
		target Target
		err    error
	}
	peers := coordinator.lan.Peers()
	results := make(chan probeResult, len(peers))
	targets := make(map[string]Target, len(peers))
	for _, peer := range peers {
		targets[peer.ID] = coordinator.unavailableTarget(peer)
	}
	probeCtx, cancel := context.WithTimeout(ctx, targetProbeTimeout)
	defer cancel()
	for _, peer := range peers {
		go func(peer lannode.Peer) {
			sessions, err := coordinator.lan.ListSessions(probeCtx, peer)
			results <- probeResult{peer: peer, target: Target{ID: peer.ID, Name: peer.Name, Status: "ready", Sessions: sessions}, err: err}
		}(peer)
	}
	for range peers {
		select {
		case result := <-results:
			if result.err == nil {
				coordinator.storeSessions(result.peer.ID, result.target.Sessions)
				targets[result.peer.ID] = result.target
			}
		case <-probeCtx.Done():
			for {
				select {
				case result := <-results:
					if result.err == nil {
						coordinator.storeSessions(result.peer.ID, result.target.Sessions)
						targets[result.peer.ID] = result.target
					}
				default:
					return sortedTargets(targets), nil
				}
			}
		}
	}
	return sortedTargets(targets), nil
}

func (coordinator *Coordinator) unavailableTarget(peer lannode.Peer) Target {
	coordinator.cacheMu.RLock()
	sessions, ok := coordinator.sessions[peer.ID]
	coordinator.cacheMu.RUnlock()
	if !ok {
		return Target{ID: peer.ID, Name: peer.Name, Status: "unreachable", Sessions: []lannode.Session{}}
	}
	return Target{ID: peer.ID, Name: peer.Name, Status: "degraded", SessionsStale: true, Sessions: append([]lannode.Session(nil), sessions...)}
}

func (coordinator *Coordinator) storeSessions(id string, sessions []lannode.Session) {
	coordinator.cacheMu.Lock()
	coordinator.sessions[id] = append([]lannode.Session(nil), sessions...)
	coordinator.cacheMu.Unlock()
}

func sortedTargets(byID map[string]Target) []Target {
	targets := make([]Target, 0, len(byID))
	for _, target := range byID {
		for index := range target.Sessions {
			if target.Sessions[index].Address == "" {
				target.Sessions[index].Address = "ra2a://" + target.ID + "/" + target.Sessions[index].ID
			}
		}
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets
}

func (coordinator *Coordinator) Send(ctx context.Context, request SendRequest) error {
	nodeID, sessionID, err := parseTarget(request.To)
	if err != nil || request.Text == "" || request.SourceSessionID == "" {
		return ErrInvalidRequest
	}
	peer, ok := coordinator.lan.Peer(nodeID)
	if !ok {
		return ErrTargetNotFound
	}
	messageID, err := newMessageID()
	if err != nil {
		return fmt.Errorf("create message ID: %w", err)
	}
	outgoing := lannode.Message{
		TargetSessionID: sessionID,
		Text:            request.Text,
		Source:          "ra2a://" + coordinator.localID + "/" + request.SourceSessionID,
		MessageID:       messageID,
	}
	err = coordinator.lan.SendMessage(ctx, peer, outgoing)
	if errors.Is(err, lannode.ErrPeerUnreachable) {
		refreshCtx, cancel := context.WithTimeout(ctx, peerRefreshTimeout)
		refreshed, refreshErr := coordinator.lan.RefreshPeer(refreshCtx, nodeID, peer.Address)
		cancel()
		if refreshErr != nil {
			return fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
		}
		err = coordinator.lan.SendMessage(ctx, refreshed, outgoing)
		if errors.Is(err, lannode.ErrPeerUnreachable) {
			return fmt.Errorf("%w: %v", ErrTargetUnreachable, err)
		}
	}
	if errors.Is(err, lannode.ErrEndpointNotFound) {
		return fmt.Errorf("%w: %v", ErrTargetNotFound, err)
	}
	if err != nil && strings.Contains(err.Error(), ErrDeliveryUnknown.Error()) {
		return fmt.Errorf("%w: %v", ErrDeliveryUnknown, err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", ErrDeliveryUnknown, err)
	}
	return err
}

// parseTarget accepts two address shapes: a two-segment endpoint address, and a
// three-segment mailbox address whose remaining path is carried verbatim so the
// mailbox namespace survives a LAN hop. Nothing else is addressable.
func parseTarget(target string) (string, string, error) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "ra2a" || parsed.Host == "" {
		return "", "", ErrInvalidRequest
	}
	path := strings.TrimPrefix(parsed.Path, "/")
	if path == "" {
		return "", "", ErrInvalidRequest
	}
	if !strings.Contains(path, "/") {
		return parsed.Host, path, nil
	}
	if _, recipient, ok, err := mailbox.ParseAddress(target); ok && err == nil {
		return parsed.Host, mailbox.Segment + "/" + recipient, nil
	}
	return "", "", ErrInvalidRequest
}

func newMessageID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// NewHandler builds the loopback control plane. When a mailbox store is
// supplied the mailbox routes are mounted alongside the endpoint routes, which
// is what lets an un-adapted harness read its mail without an adapter.
func NewHandler(backend Backend, store *mailbox.Store) http.Handler {
	mux := http.NewServeMux()
	RegisterMailboxRoutes(mux, store, nodeIDOf(backend))
	mux.HandleFunc("GET /v1/targets", func(writer http.ResponseWriter, request *http.Request) {
		targets, err := backend.ListTargets(request.Context())
		if err != nil {
			writeError(writer, http.StatusServiceUnavailable, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"targets": targets})
	})
	mux.HandleFunc("POST /v1/send", func(writer http.ResponseWriter, request *http.Request) {
		var sendRequest SendRequest
		if err := json.NewDecoder(request.Body).Decode(&sendRequest); err != nil {
			writeError(writer, http.StatusBadRequest, ErrInvalidRequest)
			return
		}
		if err := backend.Send(request.Context(), sendRequest); err != nil {
			status := http.StatusBadGateway
			switch {
			case errors.Is(err, ErrInvalidRequest):
				status = http.StatusBadRequest
			case errors.Is(err, ErrTargetNotFound):
				status = http.StatusNotFound
			case errors.Is(err, ErrTargetUnreachable):
				status = http.StatusServiceUnavailable
			case errors.Is(err, ErrDeliveryUnknown):
				status = http.StatusGatewayTimeout
			case strings.Contains(err.Error(), "SESSION_BUSY"):
				status = http.StatusConflict
			}
			writeError(writer, status, err)
			return
		}
		// Local deliveries are confirmed by the adapter result before this point;
		// LAN deliveries are only handed to the transport. Reporting the difference
		// keeps "accepted" from reading as "delivered" for cross-node sends.
		writeJSON(writer, http.StatusOK, map[string]string{
			"status": "accepted", "delivery": deliveryConfirmation(backend, sendRequest.To),
		})
	})
	return mux
}

// DeliveryConfirmer is implemented by backends that know whether a success
// response means the target confirmed the delivery, or only that the message was
// handed to a transport.
type DeliveryConfirmer interface {
	DeliveryConfirmed(target string) bool
}

func deliveryConfirmation(backend Backend, target string) string {
	if confirmer, ok := backend.(DeliveryConfirmer); ok && confirmer.DeliveryConfirmed(target) {
		return "confirmed"
	}
	return "handed_to_transport"
}

// nodeIDOf lets the mailbox routes label local deliveries without the control
// package needing to know how a backend is built.
func nodeIDOf(backend Backend) string {
	if provider, ok := backend.(interface{ LocalNodeID() string }); ok {
		return provider.LocalNodeID()
	}
	return "local"
}

func Start(ctx context.Context, address string, backend Backend, store *mailbox.Store) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid control address: %w", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("control server must bind to loopback")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen for local MCP control: %w", err)
	}
	server := &http.Server{Handler: NewHandler(backend, store)}
	go func() { _ = server.Serve(listener) }()
	go func() { <-ctx.Done(); _ = server.Close() }()
	return nil
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

type Client struct {
	endpoint string
	http     *http.Client
}

func NewClient(endpoint string) *Client {
	return &Client{endpoint: strings.TrimSuffix(endpoint, "/"), http: &http.Client{Timeout: 15 * time.Second}}
}

func (client *Client) ListTargets(ctx context.Context) ([]Target, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint+"/v1/targets", nil)
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
		Targets []Target `json:"targets"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Targets, nil
}

func (client *Client) Send(ctx context.Context, sendRequest SendRequest) error {
	payload, err := json.Marshal(sendRequest)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint+"/v1/send", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrDeliveryUnknown
		}
		return fmt.Errorf("DAEMON_UNAVAILABLE: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return decodeHTTPError(response.Body)
	}
	return nil
}

func decodeHTTPError(reader io.Reader) error {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(reader).Decode(&payload); err != nil || payload.Error == "" {
		return errors.New("daemon request failed")
	}
	return errors.New(payload.Error)
}
