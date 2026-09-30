package installer_test

import (
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
)

func TestUnixInstallerOpenCodeOnlyAutomaticallyInstallsLauncher(t *testing.T) {
	requireUnixShell(t)
	home, fakeBin := installerEnvironment(t, "Linux")
	if err := os.Rename(filepath.Join(fakeBin, "codex"), filepath.Join(fakeBin, "disabled-codex")); err != nil {
		t.Fatal(err)
	}
	openCode := filepath.Join(fakeBin, "opencode")
	writeExecutable(t, openCode, "#!/bin/sh\nexit 0\n")
	command := exec.Command("sh", "../install.sh", "--pin", "A2B3C4", "--node-id", "open-only")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("OpenCode-only install: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(home, "ra2a-calls.log"), "setup --pin A2B3C4", "--opencode "+openCode)
	for _, path := range []string{filepath.Join(home, ".local", "bin", "opencode"), filepath.Join(home, ".local", "bin", ".ra2a-opencode-wrapper")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing OpenCode launcher %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", ".ra2a-codex-wrapper")); !os.IsNotExist(err) {
		t.Fatal("an OpenCode-only machine must not install a Codex wrapper")
	}
}

func TestRemoteUnixInstallerAutoInstallsVerifiedLaunchers(t *testing.T) {
	requireUnixShell(t)
	home := t.TempDir()
	fakeBin := filepath.Join(home, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex", "opencode"} {
		writeExecutable(t, filepath.Join(fakeBin, name), "#!/bin/sh\nexit 0\n")
	}
	for _, name := range []string{"systemctl", "launchctl"} {
		writeExecutable(t, filepath.Join(fakeBin, name), "#!/bin/sh\nexit 0\n")
	}
	version := "v0.0.3"
	osName := runtime.GOOS
	arch := runtime.GOARCH
	assets := map[string][]byte{}
	for name, body := range map[string]string{
		"ra2a":             "#!/bin/sh\nexit 0\n",
		"codex-wrapper":    "#!/bin/sh\nprintf 'codex wrapper\\n'\n",
		"opencode-wrapper": "#!/bin/sh\nprintf 'opencode wrapper\\n'\n",
	} {
		assets[fmt.Sprintf("%s-%s-%s-%s", name, version, osName, arch)] = []byte(body)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(request.URL.Path, "/download/"+version+"/")
		checksum := strings.HasSuffix(name, ".sha256")
		name = strings.TrimSuffix(name, ".sha256")
		body, exists := assets[name]
		if !exists {
			http.NotFound(writer, request)
			return
		}
		if checksum {
			fmt.Fprintf(writer, "%x  %s\n", sha256.Sum256(body), name)
			return
		}
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	env := append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin", "NO_PROXY=127.0.0.1", "RA2A_RELEASE_ROOT="+server.URL, "RA2A_VERSION="+version)
	for i := 0; i < 2; i++ {
		command := exec.Command("sh", "../install-remote.sh")
		command.Env = env
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("remote install %d: %v\n%s", i, err, output)
		}
	}
	for name, prefix := range map[string]string{"codex": "codex", "opencode": "opencode"} {
		installed := filepath.Join(home, ".local", "bin", name)
		got, err := os.ReadFile(installed)
		want := assets[fmt.Sprintf("%s-wrapper-%s-%s-%s", prefix, version, osName, arch)]
		if err != nil || string(got) != string(want) {
			t.Fatalf("%s was not verified/installed: %q, %v", name, got, err)
		}
	}
	command := exec.Command("sh", "../install-remote.sh", "--uninstall")
	command.Env = env
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("remote uninstall: %v\n%s", err, output)
	}
	for _, name := range []string{"codex", "opencode"} {
		if _, err := os.Stat(filepath.Join(fakeBin, name)); err != nil {
			t.Fatalf("uninstall damaged native %s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", name)); !os.IsNotExist(err) {
			t.Fatalf("remote uninstall left RA2A %s launcher: %v", name, err)
		}
	}
}

func TestRemoteUnixInstallerRejectsCorruptLauncherBeforeChangingExistingInstall(t *testing.T) {
	requireUnixShell(t)
	home := t.TempDir()
	fakeBin := filepath.Join(home, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(fakeBin, "opencode"), "#!/bin/sh\nexit 0\n")
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(bin, "ra2a"), "#!/bin/sh\nprintf 'previous version\\n'\n")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asset := filepath.Base(request.URL.Path)
		if strings.HasSuffix(asset, ".sha256") {
			if strings.HasPrefix(asset, "opencode-wrapper-") {
				fmt.Fprintln(writer, strings.Repeat("0", 64), asset)
			} else {
				fmt.Fprintf(writer, "%x  %s\n", sha256.Sum256([]byte("release payload")), asset)
			}
			return
		}
		_, _ = writer.Write([]byte("release payload"))
	}))
	defer server.Close()
	command := exec.Command("sh", "../install-remote.sh")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin", "NO_PROXY=127.0.0.1",
		"RA2A_RELEASE_ROOT="+server.URL, "RA2A_VERSION=v0.0.3")
	if output, err := command.CombinedOutput(); err == nil || !strings.Contains(string(output), "checksum") {
		t.Fatalf("corrupt launcher checksum was not rejected: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(bin, "ra2a"))
	if err != nil || !strings.Contains(string(data), "previous version") {
		t.Fatalf("existing RA2A changed before all assets were verified: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(bin, "opencode")); !os.IsNotExist(err) {
		t.Fatalf("corrupt launcher was published: %v", err)
	}
}

func TestUnixUpgradeRetiresMissingCodexWithoutBlockingOpenCode(t *testing.T) {
	requireUnixShell(t)
	home, fakeBin := installerEnvironment(t, "Linux")
	writeExecutable(t, filepath.Join(fakeBin, "opencode"), "#!/bin/sh\nexit 0\n")
	env := append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	install := func() {
		t.Helper()
		command := exec.Command("sh", "../install.sh")
		command.Env = env
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("install after harness change: %v\n%s", err, output)
		}
	}
	install()
	if err := os.Rename(filepath.Join(fakeBin, "codex"), filepath.Join(fakeBin, "removed-codex")); err != nil {
		t.Fatal(err)
	}
	install()
	bin := filepath.Join(home, ".local", "bin")
	if _, err := os.Stat(filepath.Join(bin, ".ra2a-codex-wrapper")); !os.IsNotExist(err) {
		t.Fatalf("stale Codex integration was not retired: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bin, ".ra2a-opencode-wrapper")); err != nil {
		t.Fatalf("removing Codex must not disable OpenCode: %v", err)
	}
}

func TestUnixSourceBuildFailureDoesNotReplaceExistingRA2A(t *testing.T) {
	requireUnixShell(t)
	home, fakeBin := installerEnvironment(t, "Linux")
	writeExecutable(t, filepath.Join(fakeBin, "opencode"), "#!/bin/sh\nexit 0\n")
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	const old = "#!/bin/sh\nprintf 'previous RA2A\\n'\n"
	writeExecutable(t, filepath.Join(bin, "ra2a"), old)
	goScript := filepath.Join(fakeBin, "go")
	body, err := os.ReadFile(goScript)
	if err != nil {
		t.Fatal(err)
	}
	broken := "#!/bin/sh\ncase \"$*\" in *./cmd/oc-wrapper*) exit 23 ;; esac\n" + strings.TrimPrefix(string(body), "#!/bin/sh\n")
	writeExecutable(t, goScript, broken)
	command := exec.Command("sh", "../install.sh")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("wrapper build failure must abort install: %s", output)
	}
	got, err := os.ReadFile(filepath.Join(bin, "ra2a"))
	if err != nil || string(got) != old {
		t.Fatalf("existing RA2A was replaced before all builds passed: %q, %v", got, err)
	}
}
