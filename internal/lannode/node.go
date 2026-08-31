package lannode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/libp2p/zeroconf/v2"
	"github.com/pion/dtls/v3"
	coapdtls "github.com/plgd-dev/go-coap/v3/dtls"
	coapserver "github.com/plgd-dev/go-coap/v3/dtls/server"
	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapnet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/options"
)

const (
	serviceName = "_ra2a._udp"
	domain      = "local."
)

type Config struct {
	ID          string
	Name        string
	PIN         string
	Sessions    func(context.Context) ([]Session, error)
	SendMessage func(context.Context, Message) error
}

type Peer struct {
	ID      string
	Name    string
	Address string
}

type Session struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type Message struct {
	TargetSessionID string `json:"targetSessionId"`
	Text            string `json:"text"`
	Source          string `json:"source,omitempty"`
	MessageID       string `json:"messageId,omitempty"`
}

type sessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

type Node struct {
	config     Config
	key        []byte
	listener   *coapnet.DTLSListener
	server     *coapserver.Server
	advertiser *zeroconf.Server
	cancel     context.CancelFunc
	closeOnce  sync.Once
	peersMu    sync.RWMutex
	peers      map[string]Peer
	peerUpdate chan struct{}
}

func Start(parent context.Context, config Config) (*Node, error) {
	if config.ID == "" || config.Name == "" {
		return nil, errors.New("node ID and name are required")
	}
	if len(config.PIN) != 6 {
		return nil, errors.New("PIN must be exactly 6 bytes")
	}

	ctx, cancel := context.WithCancel(parent)
	n := &Node{
		config:     config,
		key:        []byte(config.PIN),
		cancel:     cancel,
		peers:      make(map[string]Peer),
		peerUpdate: make(chan struct{}, 1),
	}
	if err := n.startServer(); err != nil {
		cancel()
		return nil, err
	}
	port := n.listener.Addr().(*net.UDPAddr).Port
	advertiser, err := zeroconf.Register(config.ID, serviceName, domain, port,
		[]string{"version=1", "id=" + config.ID, "name=" + config.Name}, nil)
	if err != nil {
		n.Close()
		return nil, fmt.Errorf("advertise node: %w", err)
	}
	n.advertiser = advertiser

	entries := make(chan *zeroconf.ServiceEntry, 16)
	go n.collectPeers(ctx, entries)
	go func() { _ = zeroconf.Browse(ctx, serviceName, domain, entries) }()
	go func() {
		<-ctx.Done()
		n.Close()
	}()
	return n, nil
}

func (n *Node) startServer() error {
	dtlsOptions := coapnet.NewDTLSServerOptions(
		dtls.WithPSK(func([]byte) ([]byte, error) { return n.key, nil }),
		dtls.WithPSKIdentityHint([]byte("ra2a-server")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	listener, err := coapnet.NewDTLSListener("udp4", "0.0.0.0:0", dtlsOptions)
	if err != nil {
		return fmt.Errorf("listen CoAP/DTLS: %w", err)
	}

	router := mux.NewRouter()
	if err := router.Handle("/v1/sessions", mux.HandlerFunc(n.handleSessions)); err != nil {
		_ = listener.Close()
		return fmt.Errorf("register sessions handler: %w", err)
	}
	if err := router.Handle("/v1/messages", mux.HandlerFunc(n.handleMessage)); err != nil {
		_ = listener.Close()
		return fmt.Errorf("register message handler: %w", err)
	}
	n.listener = listener
	n.server = coapdtls.NewServer(options.WithMux(router))
	go func() { _ = n.server.Serve(listener) }()
	return nil
}

func (n *Node) handleMessage(w mux.ResponseWriter, request *mux.Message) {
	if request.Code() != codes.POST {
		_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, strings.NewReader("POST required"))
		return
	}
	var incoming Message
	if err := json.NewDecoder(request.Body()).Decode(&incoming); err != nil || incoming.TargetSessionID == "" || incoming.Text == "" {
		_ = w.SetResponse(codes.BadRequest, message.TextPlain, strings.NewReader("targetSessionId and text are required"))
		return
	}
	if n.config.SendMessage == nil {
		_ = w.SetResponse(codes.ServiceUnavailable, message.TextPlain, strings.NewReader("message delivery unavailable"))
		return
	}
	if err := n.config.SendMessage(request.Context(), incoming); err != nil {
		code := codes.InternalServerError
		if strings.Contains(err.Error(), "SESSION_BUSY") {
			code = codes.PreconditionFailed
		}
		_ = w.SetResponse(code, message.TextPlain, strings.NewReader(err.Error()))
		return
	}
	_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
}

func (n *Node) handleSessions(w mux.ResponseWriter, request *mux.Message) {
	sessions := make([]Session, 0)
	var err error
	if n.config.Sessions != nil {
		sessions, err = n.config.Sessions(request.Context())
	}
	if err != nil {
		_ = w.SetResponse(codes.InternalServerError, message.TextPlain, strings.NewReader(err.Error()))
		return
	}
	payload, err := json.Marshal(sessionsResponse{Sessions: sessions})
	if err == nil {
		err = w.SetResponse(codes.Content, message.AppJSON, bytes.NewReader(payload))
	}
	if err != nil {
		_ = w.SetResponse(codes.InternalServerError, message.TextPlain, strings.NewReader(err.Error()))
	}
}

func (n *Node) collectPeers(ctx context.Context, entries <-chan *zeroconf.ServiceEntry) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-entries:
			if !ok {
				return
			}
			peer, ok := n.peerFromEntry(entry)
			if !ok {
				continue
			}
			n.peersMu.Lock()
			n.peers[peer.ID] = peer
			n.peersMu.Unlock()
			select {
			case n.peerUpdate <- struct{}{}:
			default:
			}
		}
	}
}

func (n *Node) peerFromEntry(entry *zeroconf.ServiceEntry) (Peer, bool) {
	id := entry.Instance
	name := entry.Instance
	for _, item := range entry.Text {
		key, value, found := strings.Cut(item, "=")
		if !found {
			continue
		}
		switch key {
		case "id":
			id = value
		case "name":
			name = value
		}
	}
	if id == "" || entry.Port == 0 {
		return Peer{}, false
	}
	var ip net.IP
	if id == n.config.ID {
		ip = net.ParseIP("127.0.0.1")
	} else if len(entry.AddrIPv4) > 0 {
		ip = entry.AddrIPv4[0]
	} else if len(entry.AddrIPv6) > 0 {
		ip = entry.AddrIPv6[0]
	} else {
		return Peer{}, false
	}
	return Peer{ID: id, Name: name, Address: net.JoinHostPort(ip.String(), fmt.Sprint(entry.Port))}, true
}

func (n *Node) WaitForPeer(ctx context.Context, id string) (Peer, error) {
	for {
		n.peersMu.RLock()
		peer, ok := n.peers[id]
		n.peersMu.RUnlock()
		if ok {
			return peer, nil
		}
		select {
		case <-ctx.Done():
			return Peer{}, fmt.Errorf("wait for peer %q: %w", id, ctx.Err())
		case <-n.peerUpdate:
		}
	}
}

func (n *Node) Peers() []Peer {
	n.peersMu.RLock()
	peers := make([]Peer, 0, len(n.peers))
	for _, peer := range n.peers {
		peers = append(peers, peer)
	}
	n.peersMu.RUnlock()
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	return peers
}

func (n *Node) Peer(id string) (Peer, bool) {
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()
	peer, ok := n.peers[id]
	return peer, ok
}

func (n *Node) ListSessions(ctx context.Context, peer Peer) ([]Session, error) {
	clientOptions := coapdtls.NewDTLSClientOptions(
		dtls.WithPSK(func([]byte) ([]byte, error) { return n.key, nil }),
		dtls.WithPSKIdentityHint([]byte("ra2a-client")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	client, err := coapdtls.Dial(peer.Address, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", peer.ID, err)
	}
	defer client.Close()
	response, err := client.Get(ctx, "/v1/sessions")
	if err != nil {
		return nil, fmt.Errorf("request sessions from %s: %w", peer.ID, err)
	}
	defer client.ReleaseMessage(response)
	if response.Code() != codes.Content {
		return nil, fmt.Errorf("request sessions from %s: CoAP code %v", peer.ID, response.Code())
	}
	body, err := io.ReadAll(response.Body())
	if err != nil {
		return nil, fmt.Errorf("read sessions from %s: %w", peer.ID, err)
	}
	var decoded sessionsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode sessions from %s: %w", peer.ID, err)
	}
	return decoded.Sessions, nil
}

func (n *Node) SendMessage(ctx context.Context, peer Peer, outgoing Message) error {
	payload, err := json.Marshal(outgoing)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	clientOptions := coapdtls.NewDTLSClientOptions(
		dtls.WithPSK(func([]byte) ([]byte, error) { return n.key, nil }),
		dtls.WithPSKIdentityHint([]byte("ra2a-client")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	client, err := coapdtls.Dial(peer.Address, clientOptions)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", peer.ID, err)
	}
	defer client.Close()
	response, err := client.Post(ctx, "/v1/messages", message.AppJSON, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("send message to %s: %w", peer.ID, err)
	}
	defer client.ReleaseMessage(response)
	if response.Code() != codes.Changed {
		body, _ := io.ReadAll(response.Body())
		return fmt.Errorf("send message to %s: CoAP code %v: %s", peer.ID, response.Code(), strings.TrimSpace(string(body)))
	}
	return nil
}

func (n *Node) Close() {
	n.closeOnce.Do(func() {
		n.cancel()
		if n.advertiser != nil {
			n.advertiser.Shutdown()
		}
		if n.server != nil {
			n.server.Stop()
		}
		if n.listener != nil {
			_ = n.listener.Close()
		}
	})
}
