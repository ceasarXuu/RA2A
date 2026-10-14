package ochost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompatibilityRecognizesNativeVersionFormats(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable fixture")
	}
	for _, tc := range []struct {
		version string
		blocked bool
	}{
		{"1.18.34", false}, {"opencode v1.18.34", false},
		{"2.0.28", true}, {"opencode v2.0.28", true},
		{"v3.0.0", true}, {"development", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "opencode")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\necho '"+tc.version+"'\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			err := CheckCompatibility(context.Background(), binary)
			if errors.Is(err, ErrUnsupportedVersion) != tc.blocked {
				t.Fatalf("version %q: %v", tc.version, err)
			}
		})
	}
}

func TestReachableRejectsWebPageAndMalformedSessions(t *testing.T) {
	for _, body := range []string{"<!doctype html><title>OpenCode</title>", "{}", "null", "[", `[] trailing`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			if Reachable(context.Background(), server.URL) {
				t.Fatal("non-session response was accepted as healthy")
			}
		})
	}
}
