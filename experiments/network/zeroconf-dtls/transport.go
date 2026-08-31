package main

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/libp2p/zeroconf/v2"
	"github.com/pion/dtls/v3"
)

func dtlsRoundTrip(ctx context.Context, serverKey, clientKey, payload []byte) ([]byte, error) {
	listener, err := dtls.ListenWithOptions("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")},
		dtls.WithPSK(func([]byte) ([]byte, error) { return serverKey, nil }),
		dtls.WithPSKIdentityHint([]byte("ra2a-server")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	if err != nil {
		return nil, fmt.Errorf("listen DTLS: %w", err)
	}
	defer listener.Close()

	serverResult := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer conn.Close()
		dtlsConn := conn.(*dtls.Conn)
		if handshakeErr := dtlsConn.HandshakeContext(ctx); handshakeErr != nil {
			serverResult <- handshakeErr
			return
		}
		buffer := make([]byte, 2048)
		n, readErr := conn.Read(buffer)
		if readErr == nil {
			_, readErr = conn.Write(buffer[:n])
		}
		serverResult <- readErr
	}()

	client, err := dtls.DialWithOptions("udp", listener.Addr().(*net.UDPAddr),
		dtls.WithPSK(func([]byte) ([]byte, error) { return clientKey, nil }),
		dtls.WithPSKIdentityHint([]byte("ra2a-client")),
		dtls.WithCipherSuites(dtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret),
	)
	if err != nil {
		return nil, fmt.Errorf("dial DTLS: %w", err)
	}
	defer client.Close()
	if err := client.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("client handshake: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = client.SetDeadline(deadline)
	}
	if _, err := client.Write(payload); err != nil {
		return nil, fmt.Errorf("write DTLS: %w", err)
	}
	response := make([]byte, 2048)
	n, err := client.Read(response)
	if err != nil {
		return nil, fmt.Errorf("read DTLS: %w", err)
	}
	if err := <-serverResult; err != nil {
		return nil, fmt.Errorf("server DTLS: %w", err)
	}
	return response[:n], nil
}

func discoverRegisteredService(ctx context.Context, instance, service string, port int) error {
	browseCtx, cancelBrowse := context.WithCancel(ctx)
	defer cancelBrowse()
	server, err := zeroconf.Register(instance, service, "local.", port, []string{"version=1"}, nil)
	if err != nil {
		return fmt.Errorf("register zeroconf service: %w", err)
	}
	defer server.Shutdown()

	entries := make(chan *zeroconf.ServiceEntry, 16)
	browseResult := make(chan error, 1)
	go func() {
		browseResult <- zeroconf.Browse(browseCtx, service, "local.", entries)
	}()
	for {
		select {
		case entry := <-entries:
			if entry.Instance == instance && entry.Port == port {
				return nil
			}
		case err := <-browseResult:
			if err == nil {
				return errors.New("zeroconf browse stopped before discovery")
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func selfTest(ctx context.Context) error {
	if err := discoverRegisteredService(ctx, "ra2a-poc", "_ra2a-poc._tcp", 42424); err != nil {
		return err
	}
	key, err := pskFromPIN("A2B3C4")
	if err != nil {
		return err
	}
	response, err := coapRoundTrip(ctx, key, []byte("ra2a-probe"))
	if err != nil {
		return err
	}
	if string(response) != "ra2a-probe" {
		return fmt.Errorf("unexpected response %q", response)
	}
	return nil
}
