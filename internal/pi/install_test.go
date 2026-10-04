package pi

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInstallIsIdempotentAndPreservesPiConfiguration(t *testing.T) {
	isolatedPi(t)
	root := os.Getenv("PI_CODING_AGENT_DIR")
	files := map[string]string{
		"auth.json":                   `{"provider":"secret"}`,
		"settings.json":               `{"extensions":["custom"]}`,
		"extensions/custom.mjs":       "export default () => {};\n",
		"extensions/nested/other.mjs": "// unrelated extension\n",
	}
	for relative, content := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	target, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "extensions", "ra2a.mjs"); target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
	if !bytes.Equal(readTestFile(t, target), Extension) {
		t.Fatal("installed extension differs from embedded artifact")
	}
	oldTime := time.Unix(1600000000, 0)
	if err := os.Chtimes(target, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	again, err := Install()
	if err != nil || again != target {
		t.Fatalf("repeat install = %q, %v", again, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(oldTime) {
		t.Fatal("unchanged extension was rewritten")
	}
	for relative, content := range files {
		if got := string(readTestFile(t, filepath.Join(root, relative))); got != content {
			t.Fatalf("%s modified: %q", relative, got)
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".pi")); !os.IsNotExist(err) {
		t.Fatalf("custom PI_CODING_AGENT_DIR unexpectedly touched default directory: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "custom.mjs" && entry.Name() != "nested" && entry.Name() != "ra2a.mjs" {
			t.Fatalf("unexpected installation residue: %s", entry.Name())
		}
	}
}

func TestInstallUpdatesOnlyOwnedExtension(t *testing.T) {
	isolatedPi(t)
	directory := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "extensions")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "ra2a.mjs")
	if err := os.WriteFile(target, []byte("// RA2A Pi bridge: old version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readTestFile(t, target), Extension) {
		t.Fatal("owned older extension was not updated")
	}
}

func TestInstallRefusesUnownedExtension(t *testing.T) {
	isolatedPi(t)
	directory := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "extensions")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "ra2a.mjs")
	original := []byte("// another author's extension\nexport default () => {};\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(); err == nil {
		t.Fatal("unowned extension replacement accepted")
	}
	if !bytes.Equal(readTestFile(t, target), original) {
		t.Fatal("unowned extension was modified")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("refused installation created files: %+v, %v", entries, err)
	}
}

func TestInstallDefaultsToIsolatedHome(t *testing.T) {
	isolatedPi(t)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	target, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "extensions", "ra2a.mjs"); target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
	if !bytes.Equal(readTestFile(t, target), Extension) {
		t.Fatal("default installation content differs")
	}
}

func TestUninstallRemovesOnlyOwnedExtension(t *testing.T) {
	isolatedPi(t)
	root := os.Getenv("PI_CODING_AGENT_DIR")
	files := map[string]string{
		"auth.json":                   `{"provider":"secret"}`,
		"settings.json":               `{"extensions":["custom"]}`,
		"extensions/custom.mjs":       "export default () => {};\n",
		"extensions/nested/other.mjs": "// unrelated extension\n",
	}
	for relative, content := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	target, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ExtensionPath()
	if err != nil || resolved != target {
		t.Fatalf("ExtensionPath = %q, %v; installed %q", resolved, err, target)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("owned extension still exists: %v", err)
	}
	for relative, content := range files {
		if got := string(readTestFile(t, filepath.Join(root, relative))); got != content {
			t.Fatalf("%s modified: %q", relative, got)
		}
	}
	if err := Uninstall(); err != nil {
		t.Fatalf("repeat uninstall: %v", err)
	}
}

func TestUninstallRefusesUnownedExtension(t *testing.T) {
	isolatedPi(t)
	target, err := ExtensionPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("// another author's extension\nexport default () => {};\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(); err == nil {
		t.Fatal("unowned extension removal accepted")
	}
	if !bytes.Equal(readTestFile(t, target), original) {
		t.Fatal("unowned extension was modified or removed")
	}
}

func TestUninstallAbsentExtensionIsIdempotent(t *testing.T) {
	isolatedPi(t)
	target, err := ExtensionPath()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Uninstall(); err != nil {
			t.Fatalf("absent uninstall %d: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Dir(target)); !os.IsNotExist(err) {
		t.Fatalf("uninstall created extension directory: %v", err)
	}
}
