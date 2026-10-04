package pi

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
)

// Native opt-in is separate from unit tests; it never discovers user credentials.
func TestNativePiExtension(t *testing.T) {
	packagePath := os.Getenv("RA2A_PI_TEST_PACKAGE")
	if packagePath == "" {
		t.Skip("set RA2A_PI_TEST_PACKAGE to an installed Pi 1.0 package")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	extension, err := filepath.Abs("extension.mjs")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs("native_fixture.mjs")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "APPDATA="+filepath.Join(home, "app"), "LOCALAPPDATA="+filepath.Join(home, "local"), "CODEX_HOME="+filepath.Join(home, "codex"), "PI_CODING_AGENT_DIR="+filepath.Join(home, "pi"), "RA2A_PI_SESSION_DIR="+filepath.Join(home, "leases"), "XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"), "XDG_DATA_HOME="+filepath.Join(home, "data"), "PI_OFFLINE=1")
	command := exec.Command(node, fixture, packagePath, extension, "external")
	command.Dir = home
	command.Env = env
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("native start %v: %s", err, stderr.String())
	}
	t.Log(ready)
	adapter := New("fixture-node", filepath.Join(home, "leases"))
	defer adapter.Close()
	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("native endpoints %v %v", endpoints, err)
	}
	address := endpoints[0].Address
	envelope := agentbridge.MessageEnvelope{ID: "go-adapter", ProtocolVersion: agentbridge.ProtocolVersion, SourceAddress: "ra2a://peer/source", TargetAddress: address.String(), Text: "native adapter fixture", CreatedAt: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	receipt := adapter.Deliver(ctx, address, envelope)
	if !receipt.Delivered() {
		t.Fatalf("real Pi delivery: %+v", receipt)
	}
	_, _ = stdin.Write([]byte("continue\n"))
	_ = stdin.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Wait(); err != nil {
		t.Fatalf("Pi SDK fixture %v\n%s\n%s", err, output, stderr.String())
	}
	t.Log(string(output))
	binary := os.Getenv("RA2A_PI_TEST_BINARY")
	if binary == "" || runtime.GOOS == "windows" {
		return
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("native_tui_fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	tuiHome := t.TempDir()
	command = exec.Command(python, script, binary, extension, tuiHome)
	command.Dir = tuiHome
	command.Env = env
	output, err = command.CombinedOutput()
	if err != nil {
		t.Fatalf("native Pi PTY: %v\n%s", err, output)
	}
	t.Log(string(output))
}
