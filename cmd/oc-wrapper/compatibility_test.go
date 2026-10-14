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
	"time"
)

func TestV2LaunchBypassesLegacyServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable fixture")
	}
	dir := t.TempDir()
	native := filepath.Join(dir, "opencode")
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'opencode v2.0.28'; exit; fi\nprintf '%s\\n' \"$*\" > \"$TEST_NATIVE_ARGS\"\n"
	if err := os.WriteFile(native, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("v2 launch called legacy API: %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()
	t.Setenv("RA2A_OPENCODE_BINARY", native)
	t.Setenv("RA2A_OPENCODE_URL", server.URL)
	t.Setenv("RA2A_OC_OWNER_FILE", filepath.Join(dir, "owner"))
	t.Setenv("TEST_NATIVE_ARGS", argsFile)
	args := []string{"--continue", "--auto"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := run(ctx, args, os.Stdout, os.Stderr); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil || strings.TrimSpace(string(got)) != "--continue --auto" {
		t.Fatalf("native arguments: %q, %v", got, err)
	}
	err = run(context.Background(), []string{"--ra2a"}, os.Stdout, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("explicit integration must explain v2 support: %v", err)
	}
}
