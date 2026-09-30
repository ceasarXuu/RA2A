package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBareOpenCodeAutomaticallyAttachesToSharedSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fake executable fixture")
	}
	directory := t.TempDir()
	argsFile := filepath.Join(directory, "native-args")
	native := filepath.Join(directory, "native-opencode")
	if err := os.WriteFile(native, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$TEST_NATIVE_ARGS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/session":
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			_, _ = w.Write([]byte(`{"id":"ses_auto"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("RA2A_OPENCODE_BINARY", native)
	t.Setenv("RA2A_OPENCODE_URL", server.URL)
	t.Setenv("RA2A_OC_OWNER_FILE", filepath.Join(directory, "owner"))
	t.Setenv("RA2A_OC_SESSION_DIR", filepath.Join(directory, "leases"))
	t.Setenv("TEST_NATIVE_ARGS", argsFile)
	if err := run(context.Background(), nil, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("bare opencode: %v", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil || !strings.Contains(string(args), "attach "+server.URL+" --session ses_auto") {
		t.Fatalf("native TUI did not attach to the shared server: %q, %v", args, err)
	}
	if err := run(context.Background(), []string{"--version"}, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("native --version passthrough: %v", err)
	}
	args, _ = os.ReadFile(argsFile)
	if strings.TrimSpace(string(args)) != "--version" {
		t.Fatalf("--version must be passed to the native executable, got %q", args)
	}
}
