package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestSamePSKCanExchangeDTLSDatagram(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{0x41}, 32)

	got, err := dtlsRoundTrip(ctx, key, key, []byte("ra2a-probe"))
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if string(got) != "ra2a-probe" {
		t.Fatalf("response = %q", got)
	}
}

func TestDifferentPSKCannotExchangeDTLSDatagram(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := dtlsRoundTrip(ctx, bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x42}, 32), []byte("must-fail"))
	if err == nil {
		t.Fatal("round trip succeeded with a different PSK")
	}
}

func TestZeroconfDiscoversRegisteredService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := discoverRegisteredService(ctx, "ra2a-poc", "_ra2a-poc._tcp", 42424); err != nil {
		t.Fatalf("discover service: %v", err)
	}
}

func TestDTLSCanReconnectAfterClosedSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{0x43}, 32)
	for _, message := range []string{"first", "second"} {
		got, err := dtlsRoundTrip(ctx, key, key, []byte(message))
		if err != nil {
			t.Fatalf("round trip %q: %v", message, err)
		}
		if string(got) != message {
			t.Fatalf("response = %q", got)
		}
	}
}

func TestSelfTestCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := selfTest(ctx); err != nil {
		t.Fatalf("self test: %v", err)
	}
}
