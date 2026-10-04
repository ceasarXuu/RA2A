package operator

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/pi"
)

func TestPiOnlyConfigAndDiscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, "agent"))
	binary := filepath.Join(home, "pi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := os.WriteFile(binary, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := Config{NodeID: "pi-only", Name: "Pi only", PIN: "ABC123", Pi: binary}
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", home)
	if got := DetectHarnesses(config); got.Pi != binary {
		t.Fatalf("Pi path lost: %q", got.Pi)
	}
	if _, err := pi.Install(); err != nil {
		t.Fatal(err)
	}
	path, err := pi.ExtensionPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
