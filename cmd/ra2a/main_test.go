package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/codexhost"
	"github.com/ceasarXuu/RA2A/internal/control"
	"github.com/ceasarXuu/RA2A/internal/desktopipc"
	"github.com/ceasarXuu/RA2A/internal/lannode"
)

func TestDefaultAppServerSocketUsesCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "/state/codex")
	if got := defaultAppServerSocket(); got != "/state/codex/app-server-control/app-server-control.sock" {
		t.Fatalf("socket = %q", got)
	}
}

func TestRunVersionPrintsCurrentVersion(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"version"}, &output, fakeSourceFactory(nil)); err != nil {
		t.Fatalf("version: %v", err)
	}
	if output.String() != "v0.0.4\n" {
		t.Fatalf("version output = %q", output.String())
	}
}

func TestFirstRunPromptsForNameAndStartsBackgroundService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix service fixture")
	}
	home := t.TempDir()
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestExecutable(t, filepath.Join(fakeBin, "codex"), "#!/bin/sh\nexit 0\n")
	writeTestExecutable(t, filepath.Join(fakeBin, "launchctl"), "#!/bin/sh\nexit 0\n")
	writeTestExecutable(t, filepath.Join(fakeBin, "systemctl"), "#!/bin/sh\nexit 0\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	inputPath := filepath.Join(home, "input")
	if err := os.WriteFile(inputPath, []byte("Studio Mac\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	oldStdin := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = oldStdin }()

	var output bytes.Buffer
	if err := run(context.Background(), nil, &output, fakeSourceFactory(nil)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	for _, want := range []string{"设备名称", "name: Studio Mac", "PIN: ", "status: running"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output %q missing %q", output.String(), want)
		}
	}
	config, err := os.ReadFile(filepath.Join(home, ".config", "ra2a", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(config, []byte(`"name":"Studio Mac"`)) {
		t.Fatalf("config = %s", config)
	}
}

func TestFirstRunShowsGeneratedPINBeforeServiceFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix service fixture")
	}
	home := t.TempDir()
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestExecutable(t, filepath.Join(fakeBin, "codex"), "#!/bin/sh\nexit 0\n")
	writeTestExecutable(t, filepath.Join(fakeBin, "launchctl"), "#!/bin/sh\nexit 1\n")
	writeTestExecutable(t, filepath.Join(fakeBin, "systemctl"), "#!/bin/sh\nexit 1\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input, err := os.OpenFile(filepath.Join(home, "input"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	oldStdin := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = oldStdin }()
	var output bytes.Buffer
	if err := run(context.Background(), nil, &output, fakeSourceFactory(nil)); err == nil {
		t.Fatal("first run should fail when service manager fails")
	}
	if !strings.Contains(output.String(), "PIN: ") {
		t.Fatalf("failure output must preserve generated PIN: %q", output.String())
	}
}

func TestRunNamePersistsNewName(t *testing.T) {
	home := configuredOperatorHome(t)
	var output bytes.Buffer
	if err := run(context.Background(), []string{"name", "Render Node"}, &output, fakeSourceFactory(nil)); err != nil {
		t.Fatalf("name: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(home, ".config", "ra2a", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(config, []byte(`"name":"Render Node"`)) || !strings.Contains(output.String(), "status: running") {
		t.Fatalf("config=%s output=%q", config, output.String())
	}
}

func TestRunPinPersistsNewPIN(t *testing.T) {
	home := configuredOperatorHome(t)
	var output bytes.Buffer
	if err := run(context.Background(), []string{"pin", "Z9Y8X7"}, &output, fakeSourceFactory(nil)); err != nil {
		t.Fatalf("pin: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(home, ".config", "ra2a", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(config, []byte(`"pin":"Z9Y8X7"`)) || !strings.Contains(output.String(), "status: running") {
		t.Fatalf("config=%s output=%q", config, output.String())
	}
}

func TestRunRestartReportsRunning(t *testing.T) {
	configuredOperatorHome(t)
	var output bytes.Buffer
	if err := run(context.Background(), []string{"restart"}, &output, fakeSourceFactory(nil)); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if !strings.Contains(output.String(), "status: running") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestConfiguredRunOnlyChecksRunningService(t *testing.T) {
	home := configuredOperatorHome(t)
	serviceCommand := "launchctl"
	if runtime.GOOS == "linux" {
		serviceCommand = "systemctl"
	}
	writeTestExecutable(t, filepath.Join(home, "bin", serviceCommand), "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$HOME/service-calls.log\"\nexit 0\n")
	var output bytes.Buffer
	if err := run(context.Background(), nil, &output, fakeSourceFactory(nil)); err != nil {
		t.Fatalf("configured run: %v", err)
	}
	calls, err := os.ReadFile(filepath.Join(home, "service-calls.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "bootout") || strings.Contains(string(calls), "bootstrap") {
		t.Fatalf("configured run restarted service: %s", calls)
	}
}

func TestRunSetupSupportsNonInteractiveInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix service fixture")
	}
	home := t.TempDir()
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(fakeBin, "codex")
	writeTestExecutable(t, codex, "#!/bin/sh\nexit 0\n")
	writeTestExecutable(t, filepath.Join(fakeBin, "launchctl"), "#!/bin/sh\nexit 0\n")
	writeTestExecutable(t, filepath.Join(fakeBin, "systemctl"), "#!/bin/sh\nexit 0\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var output bytes.Buffer
	err := run(context.Background(), []string{
		"setup", "--pin", "A2B3C4", "--node-id", "device-b", "--name", "Device B", "--codex", codex,
	}, &output, fakeSourceFactory(nil))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(home, ".config", "ra2a", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"nodeId":"device-b"`, `"name":"Device B"`, `"pin":"A2B3C4"`} {
		if !bytes.Contains(config, []byte(want)) {
			t.Fatalf("config %s missing %s", config, want)
		}
	}
}

func TestRunUpdateInstallsChecksummedGitHubRelease(t *testing.T) {
	configuredOperatorHome(t)
	assetName := "ra2a-v0.0.5-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}
	payload := []byte("new-ra2a-binary")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/latest":
			fmt.Fprintf(writer, `{"tag_name":"v0.0.5","assets":[{"name":%q,"browser_download_url":%q},{"name":%q,"browser_download_url":%q}]}`,
				assetName, server.URL+"/binary", assetName+".sha256", server.URL+"/checksum")
		case "/binary":
			_, _ = writer.Write(payload)
		case "/checksum":
			fmt.Fprintf(writer, "%s  %s\n", digest, assetName)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	executable := filepath.Join(t.TempDir(), "ra2a")
	if err := os.WriteFile(executable, []byte("old-ra2a-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RA2A_RELEASE_API", server.URL+"/latest")
	t.Setenv("RA2A_EXECUTABLE", executable)
	var output bytes.Buffer
	if err := run(context.Background(), []string{"update"}, &output, fakeSourceFactory(nil)); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) || !strings.Contains(output.String(), "updated: v0.0.5") {
		t.Fatalf("binary=%q output=%q", got, output.String())
	}
}

func TestRunDaemonLoadsPersistedConfig(t *testing.T) {
	configuredOperatorHome(t)
	t.Setenv("RA2A_CONTROL_ADDRESS", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := run(ctx, []string{"daemon"}, &output, fakeSourceFactory(nil)); err != nil {
		t.Fatalf("daemon: %v", err)
	}
	if !strings.Contains(output.String(), "node=ra2a://test-node status=running") {
		t.Fatalf("output = %q", output.String())
	}
}

func configuredOperatorHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix service fixture")
	}
	home := t.TempDir()
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(fakeBin, "codex")
	writeTestExecutable(t, codex, "#!/bin/sh\nexit 0\n")
	writeTestExecutable(t, filepath.Join(fakeBin, "launchctl"), "#!/bin/sh\nexit 0\n")
	writeTestExecutable(t, filepath.Join(fakeBin, "systemctl"), "#!/bin/sh\nexit 0\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	configDir := filepath.Join(home, ".config", "ra2a")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`{"nodeId":"test-node","name":"Old Name","pin":"A2B3C4","codex":%q}`, codex)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func writeTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

type fakeSessionSource struct {
	sessions []lannode.Session
	messages chan deliveredMessage
}

type deliveredMessage struct {
	target string
	prompt string
}

func (source *fakeSessionSource) ListSessions(context.Context) ([]lannode.Session, error) {
	return source.sessions, nil
}

func (source *fakeSessionSource) Close() error { return nil }

func (source *fakeSessionSource) SendMessage(_ context.Context, target, prompt string) error {
	if source.messages != nil {
		source.messages <- deliveredMessage{target: target, prompt: prompt}
	}
	return nil
}

func fakeSourceFactory(sessions []lannode.Session) sessionSourceFactory {
	return func(context.Context, string, string, io.Writer) (sessionSource, error) {
		return &fakeSessionSource{sessions: sessions}, nil
	}
}

func TestRunSelfTestDiscoversAndCallsLocalNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var output bytes.Buffer

	err := run(ctx, []string{
		"selftest",
		"--pin", "A2B3C4",
		"--id", "cli-selftest-node",
		"--name", "CLI Selftest Node",
	}, &output, fakeSourceFactory([]lannode.Session{{ID: "thread-1", Title: "Test", Status: "idle"}}))
	if err != nil {
		t.Fatalf("run selftest: %v", err)
	}
	for _, want := range []string{"discovered=ra2a://cli-selftest-node", "sessions=1", "selftest=ok"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output %q does not contain %q", output.String(), want)
		}
	}
}

func TestRunSendDiscoversPeerAndDeliversMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var output bytes.Buffer
	messages := make(chan deliveredMessage, 1)
	factory := func(context.Context, string, string, io.Writer) (sessionSource, error) {
		return &fakeSessionSource{messages: messages}, nil
	}

	err := run(ctx, []string{
		"send",
		"--pin", "A2B3C4",
		"--id", "cli-send-node",
		"--peer", "cli-send-node",
		"--session", "thread-target",
		"--message", "hello",
	}, &output, factory)
	if err != nil {
		t.Fatalf("run send: %v", err)
	}
	got := <-messages
	if got.target != "thread-target" || !strings.Contains(got.prompt, "from: ra2a://cli-send-node") || !strings.HasSuffix(got.prompt, "\n\nhello") {
		t.Fatalf("delivered message = %#v", got)
	}
	if !strings.Contains(output.String(), "delivered=ra2a://cli-send-node/thread-target") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunServeStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer

	err := run(ctx, []string{
		"serve",
		"--pin", "A2B3C4",
		"--id", "cli-serve-node",
		"--name", "CLI Serve Node",
		"--control-address", "127.0.0.1:0",
	}, &output, fakeSourceFactory(nil))
	if err != nil {
		t.Fatalf("run serve: %v", err)
	}
	if !strings.Contains(output.String(), "node=ra2a://cli-serve-node status=running") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunMCPExposesProductionTools(t *testing.T) {
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\",\"params\":{}}\n")
	var output bytes.Buffer
	if err := runMCP(context.Background(), []string{"--control-url", "http://127.0.0.1:1"}, input, &output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_targets", "send_message"} {
		if !strings.Contains(output.String(), name) {
			t.Fatalf("output %q missing %q", output.String(), name)
		}
	}
}

func TestSendWithDesktopFallbackOnlyOnActiveWriter(t *testing.T) {
	desktopCalls := 0
	desktop := func(context.Context, string, string) error {
		desktopCalls++
		return nil
	}
	if err := sendWithDesktopFallback(context.Background(), "thread", "hello", func(context.Context, string, string) error {
		return nil
	}, desktop); err != nil || desktopCalls != 0 {
		t.Fatalf("managed success err=%v desktopCalls=%d", err, desktopCalls)
	}
	want := errors.New("managed unavailable")
	if err := sendWithDesktopFallback(context.Background(), "thread", "hello", func(context.Context, string, string) error {
		return want
	}, desktop); !errors.Is(err, want) || desktopCalls != 0 {
		t.Fatalf("managed failure err=%v desktopCalls=%d", err, desktopCalls)
	}
	if err := sendWithDesktopFallback(context.Background(), "thread", "hello", func(context.Context, string, string) error {
		return codexhost.ErrSessionBusy
	}, desktop); err != nil || desktopCalls != 1 {
		t.Fatalf("writer fallback err=%v desktopCalls=%d", err, desktopCalls)
	}
}

func TestSendWithDesktopFallbackPreservesBusyWhenIPCFails(t *testing.T) {
	err := sendWithDesktopFallback(context.Background(), "thread", "hello", func(context.Context, string, string) error {
		return codexhost.ErrSessionBusy
	}, func(context.Context, string, string) error {
		return errors.New("IPC unavailable")
	})
	if !errors.Is(err, codexhost.ErrSessionBusy) || !strings.Contains(err.Error(), "IPC unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestSendWithDesktopFallbackPreservesUnknownDelivery(t *testing.T) {
	err := sendWithDesktopFallback(context.Background(), "thread", "hello", func(context.Context, string, string) error {
		return codexhost.ErrSessionBusy
	}, func(context.Context, string, string) error {
		return &desktopipc.DeliveryUnknownError{Cause: context.DeadlineExceeded}
	})
	if !errors.Is(err, control.ErrDeliveryUnknown) || errors.Is(err, codexhost.ErrSessionBusy) {
		t.Fatalf("error = %v", err)
	}
}
