package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestSamePSKCanExchangeMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{0x11}, 32)
	a := newTestNode(t, ctx, key)
	b := newTestNode(t, ctx, key)

	got, err := a.exchange(ctx, b.info(), []byte("ra2a-probe"))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if string(got) != "ra2a-probe" {
		t.Fatalf("response = %q", got)
	}
}

func TestDifferentPSKCannotConnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := newTestNode(t, ctx, bytes.Repeat([]byte{0x11}, 32))
	b := newTestNode(t, ctx, bytes.Repeat([]byte{0x22}, 32))

	if _, err := a.exchange(ctx, b.info(), []byte("must-fail")); err == nil {
		t.Fatal("exchange succeeded with a different PSK")
	}
}

func TestMDNSDiscoversPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{0x33}, 32)
	a := newTestNode(t, ctx, key)
	b := newTestNode(t, ctx, key)

	if err := a.waitForPeer(ctx, b.id()); err != nil {
		t.Fatalf("discover peer: %v", err)
	}
}

func TestMessageReconnectsAfterConnectionDrop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{0x44}, 32)
	a := newTestNode(t, ctx, key)
	b := newTestNode(t, ctx, key)

	if _, err := a.exchange(ctx, b.info(), []byte("first")); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if err := a.host.Network().ClosePeer(b.id()); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	got, err := a.exchange(ctx, b.info(), []byte("second"))
	if err != nil {
		t.Fatalf("reconnect exchange: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("response = %q", got)
	}
}

func TestSelfTestCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if err := selfTest(ctx); err != nil {
		t.Fatalf("self test: %v", err)
	}
}

func newTestNode(t *testing.T, ctx context.Context, key []byte) *node {
	t.Helper()
	n, err := newNode(ctx, key)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}
	t.Cleanup(func() { _ = n.close() })
	return n
}
