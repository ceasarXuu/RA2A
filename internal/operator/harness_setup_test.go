package operator

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenCodeOnlySetupDoesNotRequireCodex(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux OpenCode-only fixture")
	}
	withTempHome(t)
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"launchctl", "systemctl"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	openCode := filepath.Join(bin, "opencode")
	if err := os.WriteFile(openCode, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Setup(Config{NodeID: "open-only", Name: "OpenCode Only", PIN: "A1B2C3"}); err != nil {
		t.Fatalf("OpenCode-only setup: %v", err)
	}
	config, err := Load()
	if err != nil || config.Codex != "" || config.OpenCode != openCode {
		t.Fatalf("unexpected config %+v: %v", config, err)
	}
	path, err := OpenCodeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"ra2a"`) {
		t.Fatalf("OpenCode MCP was not registered: %s, %v", data, err)
	}
}

func TestNoSupportedHarnessCannotBeConfigured(t *testing.T) {
	withTempHome(t)
	if err := Validate(Config{NodeID: "empty", Name: "empty", PIN: "A1B2C3"}); err == nil || !strings.Contains(err.Error(), "supported harness") {
		t.Fatalf("expected an actionable missing-harness error, got %v", err)
	}
}

func TestFirstRunWithoutHarnessReportsProblemBeforePrompt(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux empty-PATH fixture")
	}
	withTempHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	var output bytes.Buffer
	err := SetupInteractive(strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "no supported harness") || output.Len() != 0 {
		t.Fatalf("missing harness must fail before prompting: err=%v output=%q", err, output.String())
	}
}

func TestHarnessDetectionDropsMissingCodexWhileKeepingOpenCode(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux missing-binary fixture")
	}
	home := t.TempDir()
	t.Setenv("PATH", home)
	opencode := filepath.Join(home, "opencode")
	if err := os.WriteFile(opencode, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := DetectHarnesses(Config{Codex: filepath.Join(home, "removed-codex"), OpenCode: opencode})
	if config.Codex != "" || config.OpenCode != opencode {
		t.Fatalf("missing Codex must not prevent OpenCode participation: %+v", config)
	}
}
