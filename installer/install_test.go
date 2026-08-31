package installer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixInstallerCreatesUserService(t *testing.T) {
	for _, test := range []struct {
		osName      string
		servicePath string
		markers     []string
	}{
		{osName: "Darwin", servicePath: "Library/LaunchAgents/com.ra2a.daemon.plist", markers: []string{"KeepAlive", "RunAtLoad"}},
		{osName: "Linux", servicePath: ".config/systemd/user/ra2a.service", markers: []string{"Restart=always", "WantedBy=default.target"}},
	} {
		t.Run(test.osName, func(t *testing.T) {
			home, fakeBin := installerEnvironment(t, test.osName)
			command := exec.Command("sh", "../install.sh", "--pin", "A2B3C4", "--node-id", "device-b", "--name", "Device B", "--codex", "/bin/sh")
			command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("install: %v\n%s", err, output)
			}
			assertFileContains(t, filepath.Join(home, test.servicePath), test.markers...)
			assertFileContains(t, filepath.Join(home, ".config/ra2a/run.sh"), "A2B3C4", "device-b", "Device B", "/bin/sh")
			info, err := os.Stat(filepath.Join(home, ".local/bin/ra2a"))
			if err != nil || info.Mode()&0o111 == 0 {
				t.Fatalf("installed binary mode=%v err=%v", info, err)
			}
			if !strings.Contains(string(output), "PIN: A2B3C4") || !strings.Contains(string(output), "status: running") {
				t.Fatalf("unexpected output:\n%s", output)
			}
		})
	}
}

func TestUnixInstallerRejectsInvalidPIN(t *testing.T) {
	home, fakeBin := installerEnvironment(t, "Linux")
	command := exec.Command("sh", "../install.sh", "--pin", "short", "--codex", "/bin/sh")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "PIN must be exactly 6") {
		t.Fatalf("err=%v output=%s", err, output)
	}
}

func TestUnixInstallerDefaultsNameToConfiguredNodeID(t *testing.T) {
	home, fakeBin := installerEnvironment(t, "Linux")
	command := exec.Command("sh", "../install.sh", "--pin", "A2B3C4", "--node-id", "device-c", "--codex", "/bin/sh")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install: %v\n%s", err, output)
	}
	content, err := os.ReadFile(filepath.Join(home, ".config/ra2a/run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "device-c") != 2 {
		t.Fatalf("runner should use node ID as default name:\n%s", content)
	}
}

func TestPowerShellInstallerRegistersRestartingUserTask(t *testing.T) {
	content, err := os.ReadFile("../install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	if strings.Contains(script, "[ValidatePattern") {
		t.Error("optional PIN must be allowed so the installer can generate one")
	}
	for _, marker := range []string{
		"Register-ScheduledTask", "New-ScheduledTaskTrigger -AtLogOn", "RestartCount",
		"go build", "$Pin -notmatch '^[A-Za-z0-9]{6}$'", "State -ne 'Running'", "Uninstall", "RA2A installed", "status: running",
	} {
		if !strings.Contains(script, marker) {
			t.Errorf("install.ps1 missing %q", marker)
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
printf '#!/bin/sh\nexit 0\n' >"$output"
chmod 755 "$output"
`)
	writeExecutable(t, filepath.Join(fakeBin, "launchctl"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(fakeBin, "systemctl"), "#!/bin/sh\nexit 0\n")
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
