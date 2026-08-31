package installer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixInstallerDelegatesConfiguredSetupToRA2A(t *testing.T) {
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
	home, fakeBin := installerEnvironment(t, "Linux")
	command := exec.Command("sh", "../install.sh", "--pin", "short", "--codex", filepath.Join(fakeBin, "codex"))
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "PIN must be exactly 6") {
		t.Fatalf("err=%v output=%s", err, output)
	}
}

func TestUnixInstallerDefaultsNameToNodeID(t *testing.T) {
	home, fakeBin := installerEnvironment(t, "Linux")
	command := exec.Command("sh", "../install.sh", "--pin", "A2B3C4", "--node-id", "device-c", "--codex", filepath.Join(fakeBin, "codex"))
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(home, "ra2a-calls.log"), "--node-id device-c", "--name device-c")
}

func TestUnixUninstallUsesExplicitCodex(t *testing.T) {
	home, fakeBin := installerEnvironment(t, "Darwin")
	codex := filepath.Join(fakeBin, "explicit-codex")
	writeExecutable(t, codex, "#!/bin/sh\nprintf '%s\\n' \"$*\" >\"$HOME/codex-remove.log\"\n")
	command := exec.Command("sh", "../install.sh", "--uninstall", "--codex", codex)
	command.Env = append(os.Environ(), "HOME="+home, "PATH=/usr/bin:/bin")
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

func TestReleaseWorkflowBuildsChecksummedCrossPlatformAssets(t *testing.T) {
	content, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"tags:", "v*", "ra2a version", "go test ./...", "go vet ./...", "darwin arm64", "linux amd64", "windows amd64", "sha256", "gh release create"} {
		if !strings.Contains(string(content), marker) {
			t.Errorf("release workflow missing %q", marker)
		}
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
