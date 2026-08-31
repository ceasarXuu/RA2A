package main

import "errors"

func pskFromPIN(pin string) ([]byte, error) {
	if len(pin) != 6 {
		return nil, errors.New("PIN must be exactly 6 bytes")
	}
	return []byte(pin), nil
}
