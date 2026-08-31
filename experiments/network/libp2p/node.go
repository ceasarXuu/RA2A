package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
)

const (
	serviceName = "_ra2a-poc._tcp"
	streamID    = protocol.ID("/ra2a/poc/1.0.0")
)

type node struct {
	host      host.Host
	discovery mdns.Service
	found     chan peer.AddrInfo
}

func newNode(ctx context.Context, key []byte) (*node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h, err := libp2p.NewWithoutDefaults(
		libp2p.RandomIdentity,
		libp2p.DefaultPeerstore,
		libp2p.DisableMetrics(),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.PrivateNetwork(pnet.PSK(key)),
	)
	if err != nil {
		return nil, fmt.Errorf("start libp2p host: %w", err)
	}
	n := &node{host: h, found: make(chan peer.AddrInfo, 16)}
	h.SetStreamHandler(streamID, func(stream network.Stream) {
		defer stream.Close()
		_, _ = io.Copy(stream, stream)
	})
	n.discovery = mdns.NewMdnsService(h, serviceName, n)
	if err := n.discovery.Start(); err != nil {
		_ = h.Close()
		return nil, fmt.Errorf("start mDNS: %w", err)
	}
	return n, nil
}

func (n *node) HandlePeerFound(info peer.AddrInfo) {
	select {
	case n.found <- info:
	default:
	}
}

func (n *node) info() peer.AddrInfo {
	return peer.AddrInfo{ID: n.host.ID(), Addrs: n.host.Addrs()}
}

func (n *node) id() peer.ID {
	return n.host.ID()
}

func (n *node) waitForPeer(ctx context.Context, target peer.ID) error {
	for {
		select {
		case info := <-n.found:
			if info.ID == target {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (n *node) exchange(ctx context.Context, target peer.AddrInfo, payload []byte) ([]byte, error) {
	if err := n.host.Connect(ctx, target); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	stream, err := n.host.NewStream(ctx, target.ID, streamID)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	defer stream.Close()
	if _, err := stream.Write(payload); err != nil {
		return nil, fmt.Errorf("write stream: %w", err)
	}
	if err := stream.CloseWrite(); err != nil {
		return nil, fmt.Errorf("close stream writer: %w", err)
	}
	response, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}
	return response, nil
}

func (n *node) close() error {
	return errors.Join(n.discovery.Close(), n.host.Close())
}

func selfTest(ctx context.Context) error {
	key := []byte("0123456789abcdef0123456789abcdef")
	a, err := newNode(ctx, key)
	if err != nil {
		return err
	}
	defer a.close()
	b, err := newNode(ctx, key)
	if err != nil {
		return err
	}
	defer b.close()
	if err := a.waitForPeer(ctx, b.id()); err != nil {
		return err
	}
	response, err := a.exchange(ctx, b.info(), []byte("ra2a-probe"))
	if err != nil {
		return err
	}
	if string(response) != "ra2a-probe" {
		return fmt.Errorf("unexpected response %q", response)
	}
	return nil
}
