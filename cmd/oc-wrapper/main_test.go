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

func TestInteractiveTUIAutoAttachAndNativeCommandsPassThrough(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"--auto"}, {"--continue"}, {"--session", "ses_1"}, {"my-project"}} {
		if !interactiveTUI(args) {
			t.Fatalf("interactive TUI %v should attach automatically", args)
		}
	}
	for _, args := range [][]string{{"--help"}, {"--version"}, {"mcp", "list"}, {"run", "hello"}, {"serve", "--port", "4099"}, {"models"}, {"session", "list"}, {"--port", "4098"}, {"--model", "my-model"}} {
		if interactiveTUI(args) {
			t.Fatalf("native subcommand %v must pass through", args)
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
	// CI machines need not have OpenCode installed. The missing-native case
	// must fail closed, not accidentally find a developer's PATH entry.
	t.Setenv("PATH", directory)
	t.Setenv("RA2A_OPENCODE_BINARY", real)
	if got := nativeExecutable(); got != real {
		t.Fatalf("the recorded native binary must win, got %q", got)
	}
	missing := filepath.Join(directory, "absent")
	t.Setenv("RA2A_OPENCODE_BINARY", missing)
	if got := nativeExecutable(); got != "" {
		t.Fatalf("a missing native OpenCode must not resolve to an arbitrary executable, got %q", got)
	}
	t.Setenv("RA2A_OPENCODE_BINARY", "")
	if got := nativeExecutable(); got != "" {
		t.Fatalf("an empty override without a native binary must fail closed, got %q", got)
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
	t.Setenv("RA2A_OPENCODE_BINARY", self)
	if got := nativeExecutable(); got != real {
		t.Fatalf("self-referential binary override must not recurse, got %q", got)
	}
}

// --yolo/--auto have no attach equivalent, so they are translated into the
// attach client's permission policy rather than forwarded. Forwarding them made
// `opencode --yolo --ra2a` die on a yargs argument dump.
func TestTranslateAttachArgsMovesYoloIntoThePermissionPolicy(t *testing.T) {
	kept, autoApprove, err := translateAttachArgs([]string{"--yolo", "--continue"})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if !reflect.DeepEqual(kept, []string{"--continue"}) {
		t.Fatalf("the flag must not be forwarded to attach, got %v", kept)
	}
	if !autoApprove {
		t.Fatal("yolo must enable the per-session auto-approval responder")
	}
	if _, autoApprove, _ := translateAttachArgs([]string{"--auto=false"}); autoApprove {
		t.Fatal("an explicit --auto=false must not enable the policy")
	}
}

// Flags that would move the TUI to a different server are refused rather than
// silently accepted: RA2A delivers into the supervised server only.
func TestTranslateAttachArgsRefusesFlagsThatMoveTheServer(t *testing.T) {
	for _, arg := range []string{"--port", "--hostname", "--mdns", "--mdns-domain"} {
		if _, _, err := translateAttachArgs([]string{arg, "5000"}); err == nil {
			t.Fatalf("%s must be refused together with --ra2a", arg)
		}
	}
}

// Arguments attach does understand must survive untouched.
func TestTranslateAttachArgsKeepsSupportedFlags(t *testing.T) {
	kept, autoApprove, err := translateAttachArgs([]string{"--continue", "--session", "ses_x", "--mini"})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	want := []string{"--continue", "--session", "ses_x", "--mini"}
	if !reflect.DeepEqual(kept, want) {
		t.Fatalf("got %v, want %v", kept, want)
	}
	if autoApprove {
		t.Fatal("no policy must be set when the user did not ask for one")
	}
}
