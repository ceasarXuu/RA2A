package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestCoAPOverDTLSExchangesBlockwisePayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key, err := pskFromPIN("A2B3C4")
	if err != nil {
		t.Fatalf("convert PIN: %v", err)
	}
	payload := bytes.Repeat([]byte("ra2a"), 4096)

	got, err := coapRoundTrip(ctx, key, payload)
	if err != nil {
		t.Fatalf("CoAP round trip: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("response length = %d, want %d", len(got), len(payload))
	}
}
