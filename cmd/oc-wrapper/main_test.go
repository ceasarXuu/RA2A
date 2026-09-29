package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestContainsRA2A(t *testing.T) {
	if !containsRA2A([]string{"--ra2a"}) {
		t.Fatal("the flag must be detected")
	}
	if !containsRA2A([]string{"serve", "--ra2a", "--port", "4099"}) {
		t.Fatal("the flag must be detected among other arguments")
	}
	for _, args := range [][]string{nil, {}, {"serve"}, {"--ra2a-server"}} {
		if containsRA2A(args) {
			t.Fatalf("%v must not be treated as the RA2A flag", args)
		}
	}
}

func TestWithoutRA2AStripsOnlyTheFlag(t *testing.T) {
	got := withoutRA2A([]string{"--ra2a", "serve", "--ra2a", "--port"})
	want := []string{"serve", "--port"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(withoutRA2A([]string{"--ra2a"})) != 0 {
		t.Fatal("a bare flag must leave nothing behind")
	}
}

func TestNativeExecutableNeverReturnsTheWrapperItself(t *testing.T) {
	directory := t.TempDir()
	// A path that is not this binary must never be reported as the wrapper.
	other := filepath.Join(directory, "opencode.other")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if isSelf(other) {
		t.Fatal("an unrelated path must not be reported as the wrapper")
	}
	real := filepath.Join(directory, "opencode.real")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RA2A_OPENCODE_BINARY", real)
	if got := nativeExecutable(); got != real {
		t.Fatalf("the recorded native binary must win, got %q", got)
	}
	missing := filepath.Join(directory, "absent")
	t.Setenv("RA2A_OPENCODE_BINARY", missing)
	if got := nativeExecutable(); got == missing {
		t.Fatal("a recorded path that does not exist must fall through")
	}
	t.Setenv("RA2A_OPENCODE_BINARY", "")
	if got := nativeExecutable(); got == "" {
		t.Fatal("the native executable must never resolve to an empty path")
	}
}

func TestOwnerPathHonoursTheOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "owner.json")
	t.Setenv("RA2A_OC_OWNER_FILE", override)
	if got := ownerPath(); got != override {
		t.Fatalf("got %q, want %q", got, override)
	}
	t.Setenv("RA2A_OC_OWNER_FILE", "")
	if got := ownerPath(); !filepath.IsAbs(got) {
		t.Fatalf("the default owner path must be absolute, got %q", got)
	}
}

func TestEnvOrFallsBack(t *testing.T) {
	t.Setenv("RA2A_TEST_ENV", "  ")
	if got := envOr("RA2A_TEST_ENV", "fallback"); got != "fallback" {
		t.Fatalf("a blank value must fall back, got %q", got)
	}
	t.Setenv("RA2A_TEST_ENV", "set")
	if got := envOr("RA2A_TEST_ENV", "fallback"); got != "set" {
		t.Fatalf("got %q", got)
	}
}

// The installer places the wrapper in a PATH directory that precedes the real
// binary, so the resolution must skip itself instead of resolving back to it.
func TestNativeExecutableSkipsTheWrapperItselfInPath(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip("no executable path")
	}
	directory := t.TempDir()
	wrapperDir := filepath.Join(directory, "bin")
	realDir := filepath.Join(directory, "real")
	for _, path := range []string{wrapperDir, realDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// The first PATH entry holds this very binary, exactly as the wrapper would
	// find itself after installation.
	shadow := filepath.Join(wrapperDir, "opencode")
	if err := os.Symlink(self, shadow); err != nil {
		t.Fatalf("shadow the current executable: %v", err)
	}
	real := filepath.Join(realDir, "opencode")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+realDir)
	t.Setenv("RA2A_OPENCODE_BINARY", "")
	got := nativeExecutable()
	if got == shadow {
		t.Fatal("the wrapper must never resolve to itself")
	}
	if got != real {
		t.Fatalf("expected the real binary %q, got %q", real, got)
	}
}

// The TUI is launched through the top-level command pointed at the supervised
// server, not through `attach`: attach rejects flags such as --yolo, so
// `opencode --yolo --ra2a` failed with a yargs help dump.
func TestLaunchArgsKeepsUserFlagsAndTargetsTheSharedServer(t *testing.T) {
	got, err := launchArgs("http://127.0.0.1:4099", []string{"--yolo"})
	if err != nil {
		t.Fatalf("launchArgs: %v", err)
	}
	want := []string{"--port", "4099", "--hostname", "127.0.0.1", "--yolo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Flags that would move the TUI to a different server must be refused rather
// than silently accepted: RA2A delivers into the supervised server only, so
// quiet redirection would break live rendering with no visible cause.
func TestLaunchArgsRefusesFlagsThatMoveTheServer(t *testing.T) {
	for _, arg := range []string{"--port", "--hostname", "--mdns", "--mdns-domain"} {
		if _, err := launchArgs("http://127.0.0.1:4099", []string{arg, "5000"}); err == nil {
			t.Fatalf("%s must be refused together with --ra2a", arg)
		}
	}
}

func TestLaunchArgsRequiresAnExplicitPort(t *testing.T) {
	if _, err := launchArgs("http://127.0.0.1", nil); err == nil {
		t.Fatal("a server URL without a port must be refused")
	}
}
