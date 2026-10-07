package lannode

import (
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/pion/dtls/v3"
	coapdtls "github.com/plgd-dev/go-coap/v3/dtls"
	coapserver "github.com/plgd-dev/go-coap/v3/dtls/server"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapnet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/net/blockwise"
	"github.com/plgd-dev/go-coap/v3/options"
)

type boundServer struct {
	listener *coapnet.DTLSListener
	server   *coapserver.Server
}

// Each socket is bound to its advertised IP, so DTLS replies cannot acquire
// another interface's source address through the wildcard routing path.
type boundServers struct {
	mu     sync.Mutex
	port   int
	closed bool
	key    []byte
	router *mux.Router
	list   func() ([]string, error)
	active map[string]boundServer
}

func newBoundServers(key []byte, router *mux.Router, list func() ([]string, error)) (*boundServers, error) {
	b := &boundServers{key: key, router: router, list: list, active: make(map[string]boundServer)}
	if err := b.bind("127.0.0.1"); err != nil {
		return nil, err
	}
	b.reconcile()
	return b, nil
}

func (b *boundServers) bind(ip string) error {
	opts := coapnet.NewDTLSServerOptions(
		dtls.WithPSK(func([]byte) ([]byte, error) { return b.key, nil }),
		dtls.WithPSKIdentityHint([]byte("ra2a-server")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	l, err := coapnet.NewDTLSListener("udp4", net.JoinHostPort(ip, fmt.Sprint(b.port)), opts)
	if err != nil {
		return fmt.Errorf("listen CoAP/DTLS on %s: %w", ip, err)
	}
	if b.port == 0 {
		b.port = l.Addr().(*net.UDPAddr).Port
	}
	s := coapdtls.NewServer(options.WithMux(b.router), options.WithBlockwise(true, blockwise.SZX1024, sessionBlockwiseTimeout))
	b.active[ip] = boundServer{listener: l, server: s}
	go func() { _ = s.Serve(l) }()
	return nil
}

func (b *boundServers) reconcile() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	ips, err := b.list()
	if err != nil {
		slog.Warn("enumerate DTLS addresses", "error", err)
		return false // Keep known bindings on enumeration failure.
	}
	wanted := map[string]bool{"127.0.0.1": true}
	for _, ip := range ips {
		wanted[ip] = true
	}
	changed := false
	for ip, server := range b.active {
		if !wanted[ip] {
			server.server.Stop()
			_ = server.listener.Close()
			delete(b.active, ip)
			changed = true
		}
	}
	for ip := range wanted {
		if _, ok := b.active[ip]; ok {
			continue
		}
		if err := b.bind(ip); err != nil {
			slog.Warn("bind DTLS address; retry on next refresh", "address", ip, "error", err)
			continue
		}
		changed = true
	}
	return changed
}

func (b *boundServers) interfaceAddrs(iface *net.Interface) ([]net.Addr, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var published []net.Addr
	for _, addr := range addrs {
		ip, ok := addr.(*net.IPNet)
		if ok && ip.IP.To4() != nil && !b.closed {
			if _, bound := b.active[ip.IP.String()]; bound {
				published = append(published, addr)
			}
		}
	}
	return published, nil
}

func (b *boundServers) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for ip, server := range b.active {
		server.server.Stop()
		_ = server.listener.Close()
		delete(b.active, ip)
	}
}

func localIPv4() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var ips []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			ip, ok := addr.(*net.IPNet)
			if ok && ip.IP.To4() != nil && !ip.IP.IsUnspecified() {
				ips = append(ips, ip.IP.String())
			}
		}
	}
	return ips, nil
}
