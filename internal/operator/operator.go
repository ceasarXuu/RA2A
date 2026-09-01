package operator

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Version = "v0.0.7"

type Config struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	PIN    string `json:"pin"`
	Codex  string `json:"codex"`
}

func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		root := os.Getenv("LOCALAPPDATA")
		if root == "" {
			root = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(root, "RA2A", "config.json"), nil
	}
	return filepath.Join(home, ".config", "ra2a", "config.json"), nil
}

func Load() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return config, Validate(config)
}

func Save(config Config) error {
	if err := Validate(config); err != nil {
		return err
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	temporary := path + ".new"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func Validate(config Config) error {
	if config.NodeID == "" || strings.TrimSpace(config.Name) == "" || config.Codex == "" {
		return errors.New("node ID, name, and Codex path are required")
	}
	if len(config.PIN) != 6 || strings.IndexFunc(config.PIN, func(r rune) bool {
		return r > 127 || !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) >= 0 {
		return errors.New("PIN must be exactly 6 letters or digits")
	}
	return nil
}

func SetupInteractive(input io.Reader, output io.Writer) error {
	config, err := Load()
	if err == nil {
		if err := serviceRunning(); err != nil {
			if err = InstallAndStart(config); err != nil {
				return err
			}
		}
		fmt.Fprintf(output, "RA2A %s\nname: %s\nstatus: running\n", Version, config.Name)
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "设备名称 [%s]: ", hostname)
	name, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = hostname
	}
	pin, err := generatePIN()
	if err != nil {
		return err
	}
	codex, err := findCodex()
	if err != nil {
		return err
	}
	config = Config{NodeID: hostname, Name: name, PIN: pin, Codex: codex}
	if err := Save(config); err != nil {
		return err
	}
	fmt.Fprintf(output, "\nRA2A %s\nname: %s\nPIN: %s\n", Version, name, pin)
	if err := InstallAndStart(config); err != nil {
		return err
	}
	fmt.Fprintln(output, "status: running")
	return nil
}

func Setup(config Config) error {
	if err := Save(config); err != nil {
		return err
	}
	return InstallAndStart(config)
}

func Set(field, value string) (Config, error) {
	config, err := Load()
	if err != nil {
		return Config{}, err
	}
	switch field {
	case "name":
		config.Name = strings.TrimSpace(value)
	case "pin":
		config.PIN = strings.TrimSpace(value)
	}
	if err := Save(config); err != nil {
		return Config{}, err
	}
	return config, InstallAndStart(config)
}

func Restart() (Config, error) {
	config, err := Load()
	if err == nil {
		err = InstallAndStart(config)
	}
	return config, err
}

func serviceRunning() error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/com.ra2a.daemon", os.Getuid())).Run()
	case "linux":
		return exec.Command("systemctl", "--user", "is-active", "--quiet", "ra2a.service").Run()
	case "windows":
		return exec.Command("powershell.exe", "-NoProfile", "-Command", `if((Get-ScheduledTask -TaskName RA2A).State -ne 'Running'){exit 1}`).Run()
	}
	return errors.New("unsupported operating system")
}

func generatePIN() (string, error) {
	const alphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	for i := range raw {
		raw[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(raw), nil
}

func findCodex() (string, error) {
	if path, err := exec.LookPath("codex"); err == nil {
		return filepath.Abs(path)
	}
	if runtime.GOOS == "darwin" {
		path := "/Applications/ChatGPT.app/Contents/Resources/codex"
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", errors.New("Codex executable not found in PATH")
}

func InstallAndStart(config Config) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	_ = exec.Command(config.Codex, "mcp", "remove", "ra2a").Run()
	if output, err := exec.Command(config.Codex, "mcp", "add", "ra2a", "--", executable, "mcp").CombinedOutput(); err != nil {
		return fmt.Errorf("register Codex MCP: %w: %s", err, strings.TrimSpace(string(output)))
	}
	switch runtime.GOOS {
	case "darwin":
		return installDarwin(executable)
	case "linux":
		return installLinux(executable)
	case "windows":
		return installWindows(executable)
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

func installDarwin(executable string) error {
	home, _ := os.UserHomeDir()
	servicePath := filepath.Join(home, "Library", "LaunchAgents", "com.ra2a.daemon.plist")
	logDir := filepath.Join(home, ".config", "ra2a", "logs")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>com.ra2a.daemon</string>
<key>ProgramArguments</key><array><string>%s</string><string>daemon</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
</dict></plist>`, xmlEscape(executable), xmlEscape(filepath.Join(logDir, "ra2a.log")), xmlEscape(filepath.Join(logDir, "ra2a.err.log")))
	if err := os.WriteFile(servicePath, []byte(plist), 0o600); err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/com.ra2a.daemon").Run()
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if last = exec.Command("launchctl", "bootstrap", domain, servicePath).Run(); last == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if last != nil {
		return fmt.Errorf("start launch agent: %w", last)
	}
	if err := exec.Command("launchctl", "kickstart", "-k", domain+"/com.ra2a.daemon").Run(); err != nil {
		return err
	}
	return exec.Command("launchctl", "print", domain+"/com.ra2a.daemon").Run()
}

func installLinux(executable string) error {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".config", "systemd", "user", "ra2a.service")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	unit := fmt.Sprintf("[Unit]\nDescription=RA2A LAN agent daemon\nAfter=network-online.target\n\n[Service]\nExecStart=%s daemon\nRestart=always\nRestartSec=2\n\n[Install]\nWantedBy=default.target\n", executable)
	if err := os.WriteFile(path, []byte(unit), 0o600); err != nil {
		return err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "--now", "ra2a.service"}, {"--user", "restart", "ra2a.service"}, {"--user", "is-active", "--quiet", "ra2a.service"}} {
		if err := exec.Command("systemctl", args...).Run(); err != nil {
			return err
		}
	}
	return nil
}

func installWindows(executable string) error {
	quoted := strings.ReplaceAll(executable, "'", "''")
	script := fmt.Sprintf(`$a=New-ScheduledTaskAction -Execute '%s' -Argument 'daemon';$t=New-ScheduledTaskTrigger -AtLogOn;$s=New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -StartWhenAvailable;Register-ScheduledTask -TaskName RA2A -Action $a -Trigger $t -Settings $s -Force|Out-Null;Start-ScheduledTask -TaskName RA2A`, quoted)
	return exec.Command("powershell.exe", "-NoProfile", "-Command", script).Run()
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
