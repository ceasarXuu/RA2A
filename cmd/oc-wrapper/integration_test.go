package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

type synchronizedLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (log *synchronizedLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.text.Write(data)
}

func (log *synchronizedLog) Contains(marker string) bool {
	log.mu.Lock()
	defer log.mu.Unlock()
	return strings.Contains(log.text.String(), marker)
}

func (log *synchronizedLog) String() string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.text.String()
}

// Run with OPENCODE_TEST_INTEGRATION_BINARY set to a native opencode executable.
// Uses an isolated home and port: it never touches the operator's live server.
func TestNativeOpenCodeSessionAndPermissionReply(t *testing.T) {
	binary := os.Getenv("OPENCODE_TEST_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set OPENCODE_TEST_INTEGRATION_BINARY to run against a native OpenCode server")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, binary, "serve", "--pure", "--port", fmt.Sprint(port), "--hostname", "127.0.0.1")
	command.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "config"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"))
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := (&http.Client{Timeout: time.Second}).Get(baseURL + "/session")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if time.Now().After(deadline) {
		t.Fatal("isolated native OpenCode server did not start")
	}
	sessionID, _, err := ocsession.Select(ctx, baseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	var log synchronizedLog
	go approveRequests(ctx, baseURL, sessionID, ready, &log)
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/session/"+sessionID+"/permission", strings.NewReader(`{"action":"external_directory","resources":["/tmp/opencode-ra2a-external/*"]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("native permission request: %v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("native permission request returned %s", response.Status)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if log.Contains("opencode_permission_auto_approved") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("native OpenCode did not accept the per-session permission reply: response=%s logs=%s", body, log.String())
}
