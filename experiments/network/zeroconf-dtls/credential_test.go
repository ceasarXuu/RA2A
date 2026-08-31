package main

import (
	"bytes"
	"testing"
)

func TestPSKFromPINUsesSixCharactersDirectly(t *testing.T) {
	got, err := pskFromPIN("A2B3C4")
	if err != nil {
		t.Fatalf("convert PIN: %v", err)
	}
	if !bytes.Equal(got, []byte("A2B3C4")) {
		t.Fatalf("PSK = %x, want raw PIN bytes", got)
	}
}

func TestPSKFromPINRejectsWrongLength(t *testing.T) {
	for _, pin := range []string{"A2B3C", "A2B3C45"} {
		if _, err := pskFromPIN(pin); err == nil {
			t.Fatalf("PIN %q was accepted", pin)
		}
	}
}
