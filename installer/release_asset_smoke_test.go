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

// Release-asset smoke uses actual cross-compiled binaries instead of the small
// shell stubs in the ordinary installer fixtures. Supply the asset directory
// built by the Release workflow with RA2A_RELEASE_SMOKE_ASSETS.
func TestActualReleaseAssetsInstallBothDetectedHarnesses(t *testing.T) {
	assets := os.Getenv("RA2A_RELEASE_SMOKE_ASSETS")
	if assets == "" || runtime.GOOS != "linux" {
		t.Skip("set RA2A_RELEASE_SMOKE_ASSETS on Linux to verify real Release binaries")
	}
	home := t.TempDir()
	fakeBin := filepath.Join(home, "native")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex", "opencode"} {
		writeExecutable(t, filepath.Join(fakeBin, name), "#!/bin/sh\nprintf 'native "+name+"\\n'\n")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asset := strings.TrimPrefix(request.URL.Path, "/download/v0.0.16/")
		checksum := strings.HasSuffix(asset, ".sha256")
		asset = strings.TrimSuffix(asset, ".sha256")
		if strings.Contains(asset, "/") || !strings.Contains(asset, "-v0.0.16-") {
			http.NotFound(writer, request)
			return
		}
		content, err := os.ReadFile(filepath.Join(assets, asset))
		if err != nil {
			http.NotFound(writer, request)
			return
		}
		if checksum {
			fmt.Fprintf(writer, "%x  %s\n", sha256.Sum256(content), asset)
			return
		}
		_, _ = writer.Write(content)
	}))
	defer server.Close()
	command := exec.Command("sh", "../install-remote.sh")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+fakeBin+":/usr/bin:/bin",
		"RA2A_RELEASE_ROOT="+server.URL, "RA2A_VERSION=v0.0.16", "NO_PROXY=127.0.0.1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install real Release assets: %v\n%s", err, output)
	}
	bin := filepath.Join(home, ".local", "bin")
	for name, want := range map[string]string{"ra2a": "v0.0.16", "codex": "native codex", "opencode": "native opencode"} {
		run := exec.Command(filepath.Join(bin, name), "--version")
		if name == "ra2a" {
			run = exec.Command(filepath.Join(bin, name), "version")
		}
		run.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+fakeBin+":/usr/bin:/bin")
		output, err := run.CombinedOutput()
		if err != nil || !strings.Contains(string(output), want) {
			t.Fatalf("installed %s did not run: %v %s", name, err, output)
		}
	}
}
