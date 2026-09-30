package ochost

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fakeServerSource = `import http.server, sys

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.end_headers()
        self.wfile.write(b"[]")

    def log_message(self, *args):
        pass

http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
`

// stopSupervisedServer kills the shared server a test started.
//
// host.Close only detaches this client: the server is a shared resource that
// deliberately outlives whoever started it, which is the behaviour these tests
// exist to prove. Nothing else stops it, so without an explicit kill every run
// leaves a server process and its port behind -- that is how ten orphans
// accumulated in a single day.
func stopSupervisedServer(t *testing.T, host *Host) {
	t.Helper()
	host.mu.Lock()
	process := host.cmd.Process
	host.mu.Unlock()
	if process == nil {
		return
	}
	_ = process.Kill()
	_, _ = process.Wait()
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func startStubServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("[]"))
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return "http://" + listener.Addr().String()
}

func TestPortOfRequiresLoopbackWithPort(t *testing.T) {
	if _, err := portOf("http://127.0.0.1:4099"); err != nil {
		t.Fatalf("loopback URL with port must parse: %v", err)
	}
	for _, invalid := range []string{
		"http://example.com:4099",
		"http://127.0.0.1",
		"http://127.0.0.1:abc",
		"",
		"http://127.0.0.1:0",
	} {
		if _, err := portOf(invalid); err == nil {
			t.Fatalf("URL %q must be rejected", invalid)
		}
	}
}

// Adopting a server the user already started is what makes the shared-server
// arrangement feel seamless: RA2A must never spawn a rival.
func TestStartAdoptsAnAlreadyRunningServer(t *testing.T) {
	url := startStubServer(t)
	host, err := Start(context.Background(), Config{
		Executable: "definitely-not-a-real-binary", URL: url,
		ReadinessTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("an already healthy server must be adopted, got %v", err)
	}
	if host.Owned() {
		t.Fatal("adopting must not spawn a second server")
	}
	if err := host.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestStartRefusesToFightAnOccupiedPort(t *testing.T) {
	port := freePort(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	defer occupied.Close()
	probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = Start(probeCtx, Config{
		Executable:       "definitely-not-a-real-binary",
		URL:              "http://127.0.0.1:" + strconv.Itoa(port),
		ReadinessTimeout: time.Second, RestartDelay: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("an occupied port without a healthy server must be refused")
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("the refusal must be ErrBusy, got %v", err)
	}
}

func TestReachableDetectsHealthyAndUnhealthy(t *testing.T) {
	url := startStubServer(t)
	if !Reachable(context.Background(), url) {
		t.Fatal("a healthy server must be reported reachable")
	}
	// A port with nothing bound on it must not be reported reachable.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && Reachable(context.Background(), url) {
		time.Sleep(50 * time.Millisecond)
	}
	if Reachable(context.Background(), "http://127.0.0.1:"+strconv.Itoa(freePort(t))) {
		t.Fatal("a port with no server must not be reported reachable")
	}
}

// The user's TUI depends on this server, so an unexpected exit must be repaired
// rather than left silently broken.
func TestSupervisedServerRestartsAfterAnUnexpectedExit(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		if _, err := os.Stat("/usr/local/bin/python3"); err != nil {
			t.Skip("no python3 available")
		}
	}
	port := freePort(t)
	directory := t.TempDir()
	counter := filepath.Join(directory, "starts")
	server := filepath.Join(directory, "server.py")
	if err := os.WriteFile(server, []byte(fakeServerSource), 0o600); err != nil {
		t.Fatalf("write fake server: %v", err)
	}
	script := filepath.Join(directory, "fake-opencode")
	body := "#!/bin/sh\necho x >> " + counter + "\nexec python3 " + server + " " + strconv.Itoa(port) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake opencode: %v", err)
	}
	host, err := Start(context.Background(), Config{
		Executable: script, URL: "http://127.0.0.1:" + strconv.Itoa(port),
		RestartDelay: 100 * time.Millisecond, ReadinessTimeout: 10 * time.Second,
		OwnerPath: filepath.Join(directory, "owner.json"),
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer host.Close()
	t.Cleanup(func() { stopSupervisedServer(t, host) })
	if !host.Owned() {
		t.Fatal("the host must own a spawned server")
	}
	if data, err := os.ReadFile(counter); err != nil || strings.Count(string(data), "x") != 1 {
		t.Fatalf("expected exactly one start, got %q err=%v", string(data), err)
	}
	host.mu.Lock()
	process := host.cmd.Process
	host.mu.Unlock()
	if process == nil {
		t.Fatal("no supervised process")
	}
	if err := process.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(counter)
		if err == nil && strings.Count(string(data), "x") >= 2 {
			if !Reachable(context.Background(), "http://127.0.0.1:"+strconv.Itoa(port)) {
				t.Fatal("server must be reachable again after restart")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the supervised server was not restarted after an unexpected exit")
}

func TestCloseIsIdempotentAndClearsTheOwnerRecord(t *testing.T) {
	url := startStubServer(t)
	ownerPath := filepath.Join(t.TempDir(), "owner.json")
	host, err := Start(context.Background(), Config{URL: url, OwnerPath: ownerPath,
		Executable: "unused", ReadinessTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := host.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := host.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("owner record must be cleared, got %v", err)
	}
}

// The server is a shared resource: RA2A and every attached TUI depend on it, so
// closing one client must not take it down for the others.
func TestServerOutlivesTheClientThatStartedIt(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 available")
	}
	port := freePort(t)
	directory := t.TempDir()
	server := filepath.Join(directory, "server.py")
	if err := os.WriteFile(server, []byte(fakeServerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(directory, "fake-opencode")
	body := "#!/bin/sh\nexec " + python + " " + server + " " + strconv.Itoa(port) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(directory, "owner.json")
	clientCtx, cancelClient := context.WithCancel(context.Background())
	host, err := Start(clientCtx, Config{
		Executable: script, URL: "http://127.0.0.1:" + strconv.Itoa(port),
		RestartDelay: 50 * time.Millisecond, ReadinessTimeout: 10 * time.Second,
		OwnerPath: ownerPath,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { stopSupervisedServer(t, host) })
	// The client that started it goes away, exactly like a TUI being closed.
	cancelClient()
	host.mu.Lock()
	process := host.cmd.Process
	host.mu.Unlock()
	if process == nil {
		t.Fatal("no supervised process")
	}
	_ = process.Signal(syscall.SIGTERM)
	_ = process.Signal(syscall.SIGINT)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if Reachable(context.Background(), "http://127.0.0.1:"+strconv.Itoa(port)) {
			// Still serving: the shared server must have survived the client.
			_ = host.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = host.Close()
	t.Fatal("the shared server must not die with the client that started it")
}

// RA2A owns the shared server, so stop/exit must reclaim it. A server the user
// started themselves has no RA2A owner record and must be left alone.
func TestCleanupSharedReclaimsOnlyServersRa2AStarted(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	if err := CleanupShared(missing); err != nil {
		t.Fatalf("cleanup with no owner record must be a no-op, got %v", err)
	}
	if err := CleanupShared(""); err != nil {
		t.Fatalf("cleanup with no owner path must be a no-op, got %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("cleanup must not create an owner record, got %v", err)
	}
	// An owner record pointing at a dead process is simply cleaned up.
	ownerPath := filepath.Join(t.TempDir(), "owner.json")
	record := ownerRecord{PID: 999999, URL: "http://127.0.0.1:4099"}
	data, _ := json.Marshal(record)
	if err := os.WriteFile(ownerPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupShared(ownerPath); err != nil {
		t.Fatalf("cleanup of a stale record: %v", err)
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("a stale owner record must be removed, got %v", err)
	}
}
