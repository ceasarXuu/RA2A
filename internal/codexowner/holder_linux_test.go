//go:build linux

package codexowner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWriterHelper(t *testing.T) {
	path := os.Getenv("RA2A_LOCK_TEST_FILE")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	mode := syscall.LOCK_EX
	if os.Getenv("RA2A_LOCK_TEST_SHARED") == "1" {
		mode = syscall.LOCK_SH
	}
	if err := syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB); err != nil {
		panic(err)
	}
	fmt.Println("locked")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if scanner.Text() == "unlock" {
			if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
				panic(err)
			}
			fmt.Println("unlocked") // File stays open: opener is not a holder.
		}
	}
}

type lockChild struct {
	cmd   *exec.Cmd
	input *os.File
	lines chan string
}

func startLockChild(t *testing.T, path string, shared bool) *lockChild {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWriterHelper$")
	cmd.Env = append(os.Environ(), "RA2A_LOCK_TEST_FILE="+path)
	if shared {
		cmd.Env = append(cmd.Env, "RA2A_LOCK_TEST_SHARED=1")
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	child := &lockChild{cmd: cmd, input: input.(*os.File), lines: make(chan string, 8)}
	go func() {
		defer close(child.lines)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			child.lines <- scanner.Text()
		}
	}()
	t.Cleanup(func() {
		_ = input.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	child.await(t, "locked")
	return child
}

func (child *lockChild) await(t *testing.T, want string) {
	t.Helper()
	select {
	case line := <-child.lines:
		if line != want {
			t.Fatalf("helper output %q, wanted %q", line, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock helper did not respond")
	}
}

func TestReadWriterTracksReleaseAndHandoffWithoutTakingLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thread.lock")
	ctx := context.Background()
	if holder, err := ReadWriter(ctx, path); err != nil || holder != nil {
		t.Fatalf("missing: %+v, %v", holder, err)
	}
	first := startLockChild(t, path, false)
	observed, err := ReadWriter(ctx, path)
	if err != nil || observed == nil {
		t.Fatalf("holder: %+v, %v", observed, err)
	}
	if observed.PID != first.cmd.Process.Pid || observed.StartTicks == 0 || observed.Executable == "" || observed.Inode == 0 {
		t.Fatalf("identity: %+v", observed)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	still, err := ReadWriter(ctx, path)
	if err != nil || still == nil || *still != *observed {
		t.Fatalf("extra opener changed owner: %+v, %v", still, err)
	}
	if _, err := fmt.Fprintln(first.input, "unlock"); err != nil {
		t.Fatal(err)
	}
	first.await(t, "unlocked")
	if holder, err := ReadWriter(ctx, path); err != nil || holder != nil {
		t.Fatalf("released/open residual: %+v, %v", holder, err)
	}
	second := startLockChild(t, path, false)
	start := time.Now()
	current, err := ReadWriter(ctx, path)
	if err != nil || current == nil || current.PID != second.cmd.Process.Pid || current.PID == observed.PID {
		t.Fatalf("handoff: %+v, %v", current, err)
	}
	if current.Inode != observed.Inode {
		t.Fatal("fixture must reuse the same lock inode")
	}
	t.Logf("same inode handoff observed in %s; explicit query, not background refresh SLA", time.Since(start))
	if err := second.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		holder, err := ReadWriter(ctx, path)
		if err == nil && holder == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("exited holder still observed: %+v, %v", holder, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestReadWriterRejectsUnknownStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thread.lock")
	startLockChild(t, path, true)
	if holder, err := ReadWriter(context.Background(), path); err == nil || holder != nil {
		t.Fatalf("shared lock must not be sleep or exclusive ownership: %+v, %v", holder, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadWriter(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query: %v", err)
	}
	if _, err := ReadWriter(context.Background(), filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted as a lock file")
	}
}

func TestReadWriterDetectsLockFileReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thread.lock")
	first := startLockChild(t, path, false)
	old, err := ReadWriter(context.Background(), path)
	if err != nil || old == nil {
		t.Fatalf("first: %+v, %v", old, err)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	second := startLockChild(t, path, false)
	current, err := ReadWriter(context.Background(), path)
	if err != nil || current == nil || current.PID != second.cmd.Process.Pid || current.Inode == old.Inode || current.PID == first.cmd.Process.Pid {
		t.Fatalf("replacement reused old identity: %+v, %v", current, err)
	}
}

func TestParseNativeLockRecords(t *testing.T) {
	for _, tc := range []struct {
		row               string
		pid               int
		relevant, invalid bool
	}{
		{"1: FLOCK ADVISORY WRITE 123 00:0a:42 0 EOF", 123, true, false},
		{"1: -> FLOCK ADVISORY WRITE 456 00:0a:42 0 EOF", 0, false, false},
		{"1: FLOCK ADVISORY WRITE 123 00:0a:43 0 EOF", 0, false, false},
		{"1: FLOCK ADVISORY READ 123 00:0a:42 0 EOF", 0, true, true},
		{"1: POSIX ADVISORY WRITE 123 00:0a:42 0 EOF", 0, true, true},
		{"1: FLOCK ADVISORY WRITE -1 00:0a:42 0 EOF", 0, true, true},
	} {
		pid, relevant, err := parseLock(tc.row, "0:a:42")
		if pid != tc.pid || relevant != tc.relevant || (err != nil) != tc.invalid {
			t.Fatalf("%s: %d/%v/%v", tc.row, pid, relevant, err)
		}
	}
	start, exe, err := processIdentity(os.Getpid())
	if err != nil || start == 0 || !strings.Contains(exe, "codexowner") {
		t.Fatalf("own identity: %d %s %v", start, exe, err)
	}
}
