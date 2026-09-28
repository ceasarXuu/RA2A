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
