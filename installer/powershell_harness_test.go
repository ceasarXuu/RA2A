package installer_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPowerShellSourceInstallerDetectsBothHarnessesWithoutSwitches(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isolated PowerShell fixture uses Unix executable shims")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("PowerShell is not installed")
	}
	home, fakeBin := installerEnvironment(t, "Linux")
	for _, name := range []string{"codex.exe", "opencode.exe"} {
		writeExecutable(t, filepath.Join(fakeBin, name), "#!/bin/sh\nexit 0\n")
	}
	env := append(os.Environ(), "HOME="+home, "LOCALAPPDATA="+home,
		"TEMP="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	// Only Windows-specific service commands are stubbed. The real script still
	// performs detection, build, wrapper installation, and repeat-upgrade logic.
	script := `function Stop-ScheduledTask { }; function Get-CimInstance { }; & "../install.ps1"`
	for repeat := 0; repeat < 2; repeat++ {
		command := exec.Command("pwsh", "-NoProfile", "-Command", script)
		command.Env = env
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("PowerShell install %d: %v\n%s", repeat, err, output)
		}
		for _, name := range []string{"codex.exe", "opencode.exe"} {
			if _, err := os.Stat(filepath.Join(home, ".local", "bin", name)); err != nil {
				t.Fatalf("%s was not installed automatically: %v", name, err)
			}
		}
		for _, name := range []string{"codex", "opencode"} {
			path := filepath.Join(home, ".local", "bin", ".ra2a-"+name+"-native-path")
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), fakeBin) {
				t.Fatalf("native %s reference was lost on install %d: %q, %v", name, repeat, data, err)
			}
		}
	}
	command := exec.Command("pwsh", "-NoProfile", "-Command",
		`function Unregister-ScheduledTask { }; & "../install.ps1" -Uninstall`)
	command.Env = env
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell source uninstall: %v\n%s", err, output)
	}
	for _, name := range []string{"codex.exe", "opencode.exe"} {
		if _, err := os.Stat(filepath.Join(fakeBin, name)); err != nil {
			t.Fatalf("native %s lost after source uninstall: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", name)); !os.IsNotExist(err) {
			t.Fatalf("source uninstall left %s wrapper: %v", name, err)
		}
	}
}

func TestPowerShellSourceInstallerSupportsOpenCodeOnlySetup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isolated PowerShell fixture uses Unix executable shims")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("PowerShell is not installed")
	}
	home, fakeBin := installerEnvironment(t, "Linux")
	if err := os.Rename(filepath.Join(fakeBin, "codex"), filepath.Join(fakeBin, "disabled-codex")); err != nil {
		t.Fatal(err)
	}
	opencode := filepath.Join(fakeBin, "opencode.exe")
	writeExecutable(t, opencode, "#!/bin/sh\nexit 0\n")
	command := exec.Command("pwsh", "-NoProfile", "-Command",
		`function Stop-ScheduledTask { }; function Get-CimInstance { }; & "../install.ps1" -Pin A2B3C4 -NodeId open-only`)
	command.Env = append(os.Environ(), "HOME="+home, "LOCALAPPDATA="+home,
		"TEMP="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell OpenCode-only setup: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(home, "ra2a-calls.log"), "setup --pin A2B3C4", "--opencode "+opencode)
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", ".ra2a-codex-wrapper")); !os.IsNotExist(err) {
		t.Fatalf("OpenCode-only setup installed Codex wrapper: %v", err)
	}
}

// Codex only self-updates the standalone managed install. A pin on the Codex App's
// hash-versioned bin breaks `codex update` and goes stale after every App update,
// so re-running the installer must repoint the native reference at the standalone
// install even though another codex.exe is discoverable on PATH. An explicit
// -Codex must still win over that preference.
func TestPowerShellSourceInstallerRepointsRotatingAppPinToStandaloneCodex(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isolated PowerShell fixture uses Unix executable shims")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("PowerShell is not installed")
	}
	home, fakeBin := installerEnvironment(t, "Linux")
	// Discovery must use this fixture's standalone install even when the caller
	// runs tests with an explicitly isolated CODEX_HOME.
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	for _, name := range []string{"codex.exe", "opencode.exe"} {
		writeExecutable(t, filepath.Join(fakeBin, name), "#!/bin/sh\nexit 0\n")
	}
	standalone := filepath.Join(home, ".codex", "packages", "standalone", "current", "bin", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(standalone), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, standalone, "#!/bin/sh\nexit 0\n")
	appPin := filepath.Join(home, "OpenAI", "Codex", "bin", "ca9abb0b4d8ac692", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(appPin), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, appPin, "#!/bin/sh\nexit 0\n")
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Reproduce an existing RA2A launcher whose native reference is the App's
	// per-update versioned bin.
	writeExecutable(t, filepath.Join(bin, "codex.exe"), "#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(filepath.Join(bin, ".ra2a-codex-wrapper"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, ".ra2a-codex-native-path"), []byte(appPin), 0o644); err != nil {
		t.Fatal(err)
	}
	// The installers build native paths with Windows separators while this fixture
	// runs under pwsh on Linux, so compare separator-agnostically.
	samePath := func(got, want string) bool {
		return strings.ReplaceAll(strings.TrimSpace(got), `\`, "/") == filepath.ToSlash(want)
	}
	command := exec.Command("pwsh", "-NoProfile", "-Command",
		`function Stop-ScheduledTask { }; function Get-CimInstance { }; & "../install.ps1" -Pin A2B3C4 -NodeId standalone-codex`)
	command.Env = append(os.Environ(), "HOME="+home, "LOCALAPPDATA="+home,
		"TEMP="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("PowerShell install over a rotating App pin: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "replacing recorded Codex pin") {
		t.Fatalf("repointing an existing pin was not reported:\n%s", output)
	}
	data, err := os.ReadFile(filepath.Join(bin, ".ra2a-codex-native-path"))
	if err != nil || !samePath(string(data), standalone) {
		t.Fatalf("native codex reference was not repointed at the standalone install: %q, %v", data, err)
	}
	override := filepath.Join(fakeBin, "pinned-codex.exe")
	writeExecutable(t, override, "#!/bin/sh\nexit 0\n")
	command = exec.Command("pwsh", "-NoProfile", "-Command",
		`function Stop-ScheduledTask { }; function Get-CimInstance { }; & "../install.ps1" -Pin A2B3C4 -NodeId standalone-codex -Codex "`+override+`"`)
	command.Env = append(os.Environ(), "HOME="+home, "LOCALAPPDATA="+home,
		"TEMP="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell install with an explicit Codex override: %v\n%s", err, output)
	}
	data, err = os.ReadFile(filepath.Join(bin, ".ra2a-codex-native-path"))
	if err != nil || !samePath(string(data), override) {
		t.Fatalf("explicit -Codex did not win over the standalone install: %q, %v", data, err)
	}
}

func TestPowerShellReleaseInstallerVerifiesAndInstallsBothLaunchers(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isolated PowerShell fixture uses Unix executable shims")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("PowerShell is not installed")
	}
	home, fakeBin := installerEnvironment(t, "Linux")
	for _, name := range []string{"codex.exe", "opencode.exe"} {
		writeExecutable(t, filepath.Join(fakeBin, name), "#!/bin/sh\nexit 0\n")
	}
	assets := map[string][]byte{}
	for name, body := range map[string]string{
		"ra2a":             "#!/bin/sh\nexit 0\n",
		"codex-wrapper":    "#!/bin/sh\nprintf 'codex release wrapper\\n'\n",
		"opencode-wrapper": "#!/bin/sh\nprintf 'opencode release wrapper\\n'\n",
	} {
		assets[name+"-v0.0.3-windows-amd64.exe"] = []byte(body)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asset := strings.TrimPrefix(request.URL.Path, "/download/v0.0.3/")
		checksum := strings.HasSuffix(asset, ".sha256")
		asset = strings.TrimSuffix(asset, ".sha256")
		body, found := assets[asset]
		if !found {
			http.NotFound(writer, request)
			return
		}
		if checksum {
			fmt.Fprintf(writer, "%x  %s\n", sha256.Sum256(body), asset)
			return
		}
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	env := append(os.Environ(), "HOME="+home, "LOCALAPPDATA="+home, "TEMP="+home,
		"PATH="+fakeBin+":/usr/bin:/bin", "NO_PROXY=127.0.0.1")
	script := `function Get-ScheduledTask { }; function Stop-ScheduledTask { }; function Get-CimInstance { }; & "../install-remote.ps1" -Version v0.0.3 -ReleaseRoot "` + server.URL + `"`
	for repeat := 0; repeat < 2; repeat++ {
		command := exec.Command("pwsh", "-NoProfile", "-Command", script)
		command.Env = env
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("PowerShell release install %d: %v\n%s", repeat, err, output)
		}
		for _, name := range []string{"codex", "opencode"} {
			path := filepath.Join(home, ".local", "bin", name+".exe")
			data, err := os.ReadFile(path)
			if err != nil || string(data) != string(assets[name+"-wrapper-v0.0.3-windows-amd64.exe"]) {
				t.Fatalf("%s wrapper not installed from verified asset: %q, %v", name, data, err)
			}
		}
	}
	// Windows runs .exe assets directly; the Unix-hosted PowerShell fixture needs
	// the executable bit to invoke the downloaded shell-script stand-in.
	if err := os.Chmod(filepath.Join(home, ".local", "bin", "ra2a.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "pwsh", "-NoProfile", "-Command",
		`function Unregister-ScheduledTask { }; & "../install-remote.ps1" -Uninstall`)
	command.WaitDelay = 2 * time.Second
	command.Env = env
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell release uninstall: %v\n%s", err, output)
	}
	for _, name := range []string{"codex.exe", "opencode.exe"} {
		if _, err := os.Stat(filepath.Join(fakeBin, name)); err != nil {
			t.Fatalf("native %s lost after release uninstall: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", name)); !os.IsNotExist(err) {
			t.Fatalf("release uninstall left %s wrapper: %v", name, err)
		}
	}
}

func TestPowerShellReleaseRejectsBadLauncherChecksumBeforeReplacement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isolated PowerShell fixture uses Unix executable shims")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("PowerShell is not installed")
	}
	home, fakeBin := installerEnvironment(t, "Linux")
	if err := os.Rename(filepath.Join(fakeBin, "codex"), filepath.Join(fakeBin, "disabled-codex")); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(fakeBin, "opencode.exe"), "#!/bin/sh\nexit 0\n")
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	const old = "#!/bin/sh\nprintf 'old RA2A\\n'\n"
	writeExecutable(t, filepath.Join(bin, "ra2a.exe"), old)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset := filepath.Base(r.URL.Path)
		if strings.HasSuffix(asset, ".sha256") {
			if strings.HasPrefix(asset, "opencode-wrapper") {
				fmt.Fprintln(w, strings.Repeat("0", 64), asset)
			} else {
				fmt.Fprintf(w, "%x  %s\n", sha256.Sum256([]byte("new RA2A")), asset)
			}
			return
		}
		_, _ = w.Write([]byte("new RA2A"))
	}))
	defer server.Close()
	command := exec.Command("pwsh", "-NoProfile", "-Command",
		`function Get-CimInstance { }; & "../install-remote.ps1" -Version v0.0.3 -ReleaseRoot "`+server.URL+`"`)
	command.Env = append(os.Environ(), "HOME="+home, "LOCALAPPDATA="+home, "TEMP="+home,
		"PATH="+fakeBin+":/usr/bin:/bin", "NO_PROXY=127.0.0.1")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "checksum") {
		t.Fatalf("corrupt OpenCode launcher checksum not rejected: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(bin, "ra2a.exe"))
	if err != nil || string(data) != old {
		t.Fatalf("RA2A changed before all launcher assets were verified: %q, %v", data, err)
	}
}
