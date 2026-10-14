package codexcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const fakeCodexFixtureSuffix = ".fixture.json"

type fakeCodexResult struct {
	Output   string
	ExitCode int
}

type fakeCodexFixture struct {
	Default    fakeCodexResult
	ScopedHome string
	Scoped     fakeCodexResult
}

// A linked/copied test executable handles only the fake daemon probe. Its
// adjacent fixture avoids shell quoting and cannot fall back to a real Codex.
func TestMain(m *testing.M) {
	payload, err := os.ReadFile(os.Args[0] + fakeCodexFixtureSuffix)
	if os.IsNotExist(err) {
		if len(os.Args) > 1 && os.Args[1] == "app-server" {
			fmt.Fprintln(os.Stderr, "fake Codex fixture is missing")
			os.Exit(2)
		}
		os.Exit(m.Run())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(os.Args) != 4 || os.Args[1] != "app-server" || os.Args[2] != "daemon" || os.Args[3] != "version" {
		fmt.Fprintln(os.Stderr, "fake Codex accepts only app-server daemon version")
		os.Exit(2)
	}
	var fixture fakeCodexFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	result := fixture.Default
	if fixture.ScopedHome != "" && os.Getenv("CODEX_HOME") == fixture.ScopedHome {
		result = fixture.Scoped
	}
	if result.Output != "" {
		fmt.Fprintln(os.Stdout, result.Output)
	}
	os.Exit(result.ExitCode)
}

func writeFakeCodexFixture(t *testing.T, fixture fakeCodexFixture) string {
	t.Helper()
	// A race-built helper has no concurrent test work; its default one-second
	// exit delay would distort daemon-probe and receipt deadlines.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	// Spaces and parentheses exercise native argument passing on Windows too.
	directory := filepath.Join(t.TempDir(), "fake Codex (fixture)")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "codex")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(executable, path); err != nil {
		copyTestExecutable(t, executable, path)
	}
	payload, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+fakeCodexFixtureSuffix, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func copyTestExecutable(t *testing.T, source, target string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestFakeCodexFixtureRejectsUnexpectedArguments(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing-fixture=%v", missing), func(t *testing.T) {
			path := writeFakeCodexFixture(t, fakeCodexFixture{Default: fakeCodexResult{Output: "must not run"}})
			args := []string{"-test.run=^$"}
			if missing {
				if err := os.Remove(path + fakeCodexFixtureSuffix); err != nil {
					t.Fatal(err)
				}
				args = []string{"app-server", "daemon", "version"}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, path, args...).Output()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 2 || len(output) != 0 {
				t.Fatalf("invalid invocation must not enter the test suite: output=%q err=%v", output, err)
			}
		})
	}
}
