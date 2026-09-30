package operator

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

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

// DetectHarnesses retains runnable configured paths, drops missing binaries,
// and discovers newly installed harnesses on the next normal install/restart.
func DetectHarnesses(config Config) Config {
	if config.Codex != "" && !runnableHarness(config.Codex) {
		config.Codex = ""
	}
	if config.OpenCode != "" && !runnableHarness(config.OpenCode) {
		config.OpenCode = ""
	}
	if config.Codex == "" {
		config.Codex, _ = findCodex()
	}
	if config.OpenCode == "" {
		if path, err := exec.LookPath("opencode"); err == nil {
			config.OpenCode, _ = filepath.Abs(path)
		}
	}
	return config
}

func runnableHarness(path string) bool {
	if strings.ContainsAny(path, `/\`) {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0)
	}
	_, err := exec.LookPath(path)
	return err == nil
}

func InstallAndStart(config Config) error {
	config = DetectHarnesses(config)
	if err := Save(config); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if config.Codex != "" {
		_ = exec.Command(config.Codex, "mcp", "remove", "ra2a").Run()
		if output, err := exec.Command(config.Codex, "mcp", "add", "ra2a", "--", executable, "mcp").CombinedOutput(); err != nil {
			return fmt.Errorf("register Codex MCP: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	// OpenCode agents must be able to send without editing their config.
	if config.OpenCode != "" {
		if err := RegisterOpenCodeMCP(executable); err != nil {
			return fmt.Errorf("register OpenCode MCP: %w", err)
		}
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
