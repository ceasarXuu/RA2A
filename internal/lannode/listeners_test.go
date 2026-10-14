package lannode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/mux"
)

func testBoundServers(t *testing.T, list func() ([]string, error)) (*Node, *boundServers) {
	t.Helper()
	n := &Node{key: []byte("123456"), config: Config{
		Sessions: func(context.Context) ([]Session, error) { return []Session{{ID: "fixture"}}, nil },
	}}
	router := mux.NewRouter()
	if err := router.Handle("/v1/sessions", mux.HandlerFunc(n.handleSessions)); err != nil {
		t.Fatal(err)
	}
	b, err := newBoundServers(n.key, router, list)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.close)
	return n, b
}

func TestBoundServersKeepEachDTLSPeerAddress(t *testing.T) {
	n, b := testBoundServers(t, func() ([]string, error) { return []string{"127.0.0.2"}, nil })
	if _, ok := b.active["127.0.0.2"]; !ok {
		if runtime.GOOS != "linux" {
			t.Skip("second loopback address is not bindable on this host")
		}
		t.Fatal("second loopback address not bound")
	}
	for _, ip := range []string{"127.0.0.1", "127.0.0.2"} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		peer := Peer{ID: "test", Address: net.JoinHostPort(ip, fmt.Sprint(b.port))}
		sessions, err := n.ListSessions(ctx, peer)
		cancel()
		if err != nil || len(sessions) != 1 || sessions[0].ID != "fixture" {
			t.Fatalf("connected UDP/DTLS on %s: sessions=%v error=%v", ip, sessions, err)
		}
		if got := b.active[ip].listener.Addr().(*net.UDPAddr).IP.String(); got != ip {
			t.Fatalf("listener source = %s, expected %s", got, ip)
		}
	}
}

func TestBoundServersReconcileAddressesAndRetryFailedBind(t *testing.T) {
	var ips []string
	var listErr error
	n, b := testBoundServers(t, func() ([]string, error) { return ips, listErr })
	blocker, err := net.ListenPacket("udp4", net.JoinHostPort("127.0.0.2", fmt.Sprint(b.port)))
	if err != nil {
		if runtime.GOOS != "linux" {
			t.Skip("second loopback address is not bindable on this host")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Close() })
	ips = []string{"127.0.0.2"}
	if b.reconcile() || len(b.active) != 1 {
		t.Fatal("failed bind was published")
	}
	_ = blocker.Close()
	if !b.reconcile() || len(b.active) != 2 {
		t.Fatal("unchanged address list did not retry failed bind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := n.ListSessions(ctx, Peer{ID: "test", Address: net.JoinHostPort("127.0.0.2", fmt.Sprint(b.port))}); err != nil {
		t.Fatal(err)
	}
	listErr = errors.New("temporary enumeration failure")
	ips = nil
	if b.reconcile() || len(b.active) != 2 {
		t.Fatal("enumeration failure discarded working binding")
	}
	listErr = nil
	removed := b.active["127.0.0.2"].listener
	if !b.reconcile() || len(b.active) != 1 {
		t.Fatal("removed address remains bound")
	}
	if _, err := removed.AcceptWithContext(context.Background()); err == nil {
		t.Fatal("removed listener still accepts")
	}
	b.close()
	ips = []string{"127.0.0.2"}
	if b.reconcile() || len(b.active) != 0 {
		t.Fatal("closed transport restarted")
	}
}

func TestBoundServersAdvertiseOnlySuccessfulAddresses(t *testing.T) {
	_, b := testBoundServers(t, func() ([]string, error) { return nil, nil })
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, iface := range ifaces {
		addrs, err := b.interfaceAddrs(&iface)
		if err != nil {
			t.Fatal(err)
		}
		for _, addr := range addrs {
			found = true
			if addr.(*net.IPNet).IP.String() != "127.0.0.1" {
				t.Fatalf("unbound address advertised: %s", addr)
			}
		}
	}
	if !found {
		t.Fatal("bound loopback missing from address filter")
	}
	b.close()
	for _, iface := range ifaces {
		addrs, err := b.interfaceAddrs(&iface)
		if err != nil || len(addrs) != 0 {
			t.Fatalf("closed transport advertised: %v, %v", addrs, err)
		}
	}
}
