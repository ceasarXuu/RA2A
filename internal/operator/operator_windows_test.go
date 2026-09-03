package operator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsScheduledTaskScriptUsesInteractiveCurrentUser(t *testing.T) {
	script := windowsScheduledTaskScript(`C:\Program Files\RA2A\ra2a.exe`)

	for _, want := range []string{
		`[System.Security.Principal.WindowsIdentity]::GetCurrent().Name`,
		`New-ScheduledTaskPrincipal -UserId $u -LogonType Interactive -RunLevel Limited`,
		`New-ScheduledTaskTrigger -AtLogOn -User $u`,
		`New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes 1)`,
		`-MultipleInstances IgnoreNew`,
		`-AllowStartIfOnBatteries`,
		`-DontStopIfGoingOnBatteries`,
		`Register-ScheduledTask -TaskName RA2A -Action $a -Trigger @($t,$w) -Principal $p -Settings $s -Force`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("scheduled task script missing %q:\n%s", want, script)
		}
	}
}

func TestWindowsScheduledTaskScriptEscapesExecutable(t *testing.T) {
	script := windowsScheduledTaskScript(`C:\Users\O'Brien\ra2a.exe`)
	if !strings.Contains(script, `C:\Users\O''Brien\ra2a.exe`) {
		t.Fatalf("scheduled task script did not escape executable path:\n%s", script)
	}
}

func TestWindowsStopScheduledTaskScriptDisablesAndStopsTask(t *testing.T) {
	script := windowsStopScheduledTaskScript()
	for _, want := range []string{
		`Get-ScheduledTask -TaskName RA2A`,
		`Disable-ScheduledTask -TaskName RA2A`,
		`Stop-ScheduledTask -TaskName RA2A`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("stop script missing %q:\n%s", want, script)
		}
	}
}

func TestWindowsExitScheduledTaskScriptUnregistersTask(t *testing.T) {
	script := windowsExitScheduledTaskScript()
	for _, want := range []string{
		`Get-ScheduledTask -TaskName RA2A`,
		`Stop-ScheduledTask -TaskName RA2A`,
		`Unregister-ScheduledTask -TaskName RA2A`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("exit script missing %q:\n%s", want, script)
		}
	}
}

func TestWindowsConfigPathUsesUserHome(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path contract")
	}
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "sandboxed-local-app-data"))

	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "ra2a", "config.json")
	if got != want {
		t.Fatalf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestWindowsLoadMigratesLegacyLocalAppDataConfig(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows migration contract")
	}
	home := t.TempDir()
	localAppData := filepath.Join(home, "legacy-local-app-data")
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", localAppData)
	legacyPath := filepath.Join(localAppData, "RA2A", "config.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"nodeId":"rog306","name":"rog306","pin":"78e901","codex":"C:\\Codex\\codex.exe"}`)
	if err := os.WriteFile(legacyPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.NodeID != "rog306" || config.PIN != "78e901" {
		t.Fatalf("migrated config = %+v", config)
	}
	newPath := filepath.Join(home, ".config", "ra2a", "config.json")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("migrated config missing at %s: %v", newPath, err)
	}
}
