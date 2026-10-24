//go:build linux || darwin

package main

import (
	"os"
	"strings"
)

func controlAddress() string {
	if value := os.Getenv("RA2A_SANDBOX_CONTROL_ADDR"); value != "" {
		return value
	}
	return "127.0.0.1:47321"
}

func sandboxBindAddresses() func() ([]string, error) {
	raw := os.Getenv("RA2A_SANDBOX_BIND_ADDRESSES")
	if raw == "" {
		return nil
	}
	addresses := splitAddresses(raw)
	return func() ([]string, error) { return addresses, nil }
}

func sandboxAdvertiseAddresses() []string {
	return splitAddresses(os.Getenv("RA2A_SANDBOX_ADVERTISE_ADDRESSES"))
}

func splitAddresses(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}
