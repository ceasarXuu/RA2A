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

func TestRemoteUnixInstallerDownloadsChecksummedRelease(t *testing.T) {
	requireUnixShell(t)
	payload := []byte("#!/bin/sh\nprintf 'v0.0.3\\n'\n")
	asset := "ra2a-v0.0.3-" + runtime.GOOS + "-" + runtime.GOARCH
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/download/v0.0.3/" + asset:
			_, _ = writer.Write(payload)
		case "/download/v0.0.3/" + asset + ".sha256":
			fmt.Fprintf(writer, "%s  %s\n", digest, asset)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	home := t.TempDir()
	command := exec.Command("sh", "../install-remote.sh")
	command.Env = append(os.Environ(), "HOME="+home, "NO_PROXY=127.0.0.1", "RA2A_RELEASE_ROOT="+server.URL, "RA2A_VERSION=v0.0.3")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("remote install: %v\n%s", err, output)
	}
	installed := filepath.Join(home, ".local", "bin", "ra2a")
	got, err := os.ReadFile(installed)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("installed=%q err=%v", got, err)
	}
	info, err := os.Stat(installed)
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("installed mode=%v err=%v", info, err)
	}
}

func TestRemoteUnixInstallerPreservesExistingBinaryOnChecksumFailure(t *testing.T) {
	requireUnixShell(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, ".sha256") {
			fmt.Fprintln(writer, strings.Repeat("0", 64), " asset")
			return
		}
		_, _ = writer.Write([]byte("corrupt replacement"))
	}))
	defer server.Close()
	home := t.TempDir()
	installed := filepath.Join(home, ".local", "bin", "ra2a")
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("working binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "../install-remote.sh")
	command.Env = append(os.Environ(), "HOME="+home, "NO_PROXY=127.0.0.1", "RA2A_RELEASE_ROOT="+server.URL, "RA2A_VERSION=v0.0.3")
	if output, err := command.CombinedOutput(); err == nil || !strings.Contains(string(output), "checksum") {
		t.Fatalf("err=%v output=%s", err, output)
	}
	got, err := os.ReadFile(installed)
	if err != nil || string(got) != "working binary" {
		t.Fatalf("existing binary changed: %q err=%v", got, err)
	}
}

func TestRemoteUnixInstallerPassesNonInteractiveSetupArguments(t *testing.T) {
	requireUnixShell(t)
	payload := []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$HOME/remote-setup.log\"\n")
	asset := "ra2a-v0.0.3-" + runtime.GOOS + "-" + runtime.GOARCH
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, ".sha256") {
			fmt.Fprintf(writer, "%s  %s\n", digest, asset)
			return
		}
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	home := t.TempDir()
	command := exec.Command("sh", "../install-remote.sh", "--pin", "A2B3C4", "--node-id", "device-b", "--name", "Device B", "--codex", "/opt/codex")
	command.Env = append(os.Environ(), "HOME="+home, "NO_PROXY=127.0.0.1", "RA2A_RELEASE_ROOT="+server.URL, "RA2A_VERSION=v0.0.3")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("remote setup: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(home, "remote-setup.log"), "setup --pin A2B3C4 --node-id device-b --name Device B --codex /opt/codex")
}

func TestUnixInstallerDelegatesConfiguredSetupToRA2A(t *testing.T) {
	requireUnixShell(t)
	for _, osName := range []string{"Darwin", "Linux"} {
		t.Run(osName, func(t *testing.T) {
			home, fakeBin := installerEnvironment(t, osName)
			codex := filepath.Join(fakeBin, "codex")
			command := exec.Command("sh", "../install.sh", "--pin", "A2B3C4", "--node-id", "device-b", "--name", "Device B", "--codex", codex)
			command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("install: %v\n%s", err, output)
			}
			assertFileContains(t, filepath.Join(home, "ra2a-calls.log"), "setup", "--pin A2B3C4", "--node-id device-b", "--name Device B", "--codex "+codex)
			info, err := os.Stat(filepath.Join(home, ".local", "bin", "ra2a"))
			if err != nil || info.Mode()&0o111 == 0 {
				t.Fatalf("installed binary mode=%v err=%v", info, err)
			}
		})
	}
}

func TestUnixInstallerWithoutOptionsLeavesInteractiveSetupToRA2A(t *testing.T) {
	requireUnixShell(t)
	home, fakeBin := installerEnvironment(t, "Darwin")
	command := exec.Command("sh", "../install.sh")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Run ra2a to finish setup") {
		t.Fatalf("unexpected output:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(home, "ra2a-calls.log")); !os.IsNotExist(err) {
		t.Fatalf("interactive setup should not run during install: %v", err)
	}
}

func TestUnixInstallerRejectsInvalidPIN(t *testing.T) {
	requireUnixShell(t)
	home, fakeBin := installerEnvironment(t, "Linux")
	command := exec.Command("sh", "../install.sh", "--pin", "short", "--codex", filepath.Join(fakeBin, "codex"))
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "PIN must be exactly 6") {
		t.Fatalf("err=%v output=%s", err, output)
	}
}

func TestUnixInstallerDefaultsNameToNodeID(t *testing.T) {
	requireUnixShell(t)
	home, fakeBin := installerEnvironment(t, "Linux")
	command := exec.Command("sh", "../install.sh", "--pin", "A2B3C4", "--node-id", "device-c", "--codex", filepath.Join(fakeBin, "codex"))
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(home, "ra2a-calls.log"), "--node-id device-c", "--name device-c")
}

func TestUnixUninstallUsesExplicitCodex(t *testing.T) {
	requireUnixShell(t)
	home, fakeBin := installerEnvironment(t, "Darwin")
	codex := filepath.Join(fakeBin, "explicit-codex")
	writeExecutable(t, codex, "#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$HOME/codex-remove.log\"\n")
	command := exec.Command("sh", "../install.sh", "--uninstall", "--codex", codex)
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(home, "codex-remove.log"), "mcp remove ra2a")
}

func TestPowerShellInstallerDelegatesLifecycleToRA2A(t *testing.T) {
	content, err := os.ReadFile("../install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"go build", "setup", "$Pin -notmatch '^[A-Za-z0-9]{6}$'", "Uninstall", "Run ra2a to finish setup", "if ($ExistingTask) { Start-ScheduledTask", "$Mcp = $Codex"} {
		if !strings.Contains(string(content), marker) {
			t.Errorf("install.ps1 missing %q", marker)
		}
	}
}

func TestRemotePowerShellInstallerUsesReleaseAndSHA256(t *testing.T) {
	content, err := os.ReadFile("../install-remote.ps1")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"releases/latest", "Get-FileHash", "SHA256", "ra2a-$Version-windows-$Architecture.exe", "Move-Item", "setup --pin"} {
		if !strings.Contains(string(content), marker) {
			t.Errorf("install-remote.ps1 missing %q", marker)
		}
	}
}

func TestReleaseWorkflowBuildsChecksummedCrossPlatformAssets(t *testing.T) {
	content, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"tags:", "v*", "ra2a version", "go test ./...", "go vet ./...", "darwin arm64", "linux amd64", "windows amd64", "sha256", "install-ra2a.sh", "install-ra2a.ps1", "gh release create"} {
		if !strings.Contains(string(content), marker) {
			t.Errorf("release workflow missing %q", marker)
		}
	}
}

func TestReadmesUseReleaseInstallAsPrimaryPath(t *testing.T) {
	tests := []struct {
		path    string
		markers []string
	}{
		{
			path:    "../README.md",
			markers: []string{"releases/latest/download/install-ra2a.sh", "releases/latest/download/install-ra2a.ps1", "No Git, Go"},
		},
		{
			path:    "../README.zh-CN.md",
			markers: []string{"releases/latest/download/install-ra2a.sh", "releases/latest/download/install-ra2a.ps1", "无需 Git、Go"},
		},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			content, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			for _, marker := range test.markers {
				if !strings.Contains(string(content), marker) {
					t.Errorf("%s missing remote install contract %q", test.path, marker)
				}
			}
		})
	}
}

func installerEnvironment(t *testing.T, osName string) (string, string) {
	t.Helper()
	home := t.TempDir()
	fakeBin := filepath.Join(home, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(fakeBin, "uname"), "#!/bin/sh\nprintf '%s\\n' '"+osName+"'\n")
	writeExecutable(t, filepath.Join(fakeBin, "launchctl"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "systemctl"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "go"), `#!/bin/sh
if [ "$1" = "version" ]; then echo "go version go1.24.0 test"; exit 0; fi
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then shift; output=$1; fi
  shift
done
mkdir -p "$(dirname "$output")"
printf '#!/bin/sh\nprintf "%%s\\n" "$*" >>"$HOME/ra2a-calls.log"\nprintf "status: running\\n"\n' >"$output"
chmod 755 "$output"
`)
	writeExecutable(t, filepath.Join(fakeBin, "codex"), "#!/bin/sh\nexit 0\n")
	return home, fakeBin
}

func requireUnixShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell installer fixture")
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFileContains(t *testing.T, path string, markers ...string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range markers {
		if !strings.Contains(string(content), marker) {
			t.Errorf("%s missing %q", path, marker)
		}
	}
}
