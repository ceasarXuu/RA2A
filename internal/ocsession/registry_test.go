package ocsession

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLeaseOnlyPublishesLiveAttachedSession(t *testing.T) {
	directory := t.TempDir()
	if Active(directory)["ses_attached"] {
		t.Fatal("unattached session must not be published")
	}
	release, err := Register(directory, "ses_attached")
	if err != nil {
		t.Fatal(err)
	}
	if !Active(directory)["ses_attached"] {
		t.Fatal("attached session must be published")
	}
	release()
	if Active(directory)["ses_attached"] {
		t.Fatal("exited TUI session must not be published")
	}
	if err := os.WriteFile(filepath.Join(directory, "ses_stale.999999"), []byte(`{"sessionID":"ses_stale","pid":999999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if Active(directory)["ses_stale"] {
		t.Fatal("dead owner process must not publish a session")
	}
}

func TestFocusOwnersShareDirectoryWithoutChangingEachOthersLease(t *testing.T) {
	child := exec.Command(os.Args[0], "-test.run=^TestFocusOwnerHelper$")
	child.Env = append(os.Environ(), "RA2A_TEST_FOCUS_OWNER=1")
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = child.Wait() })
	dir := t.TempDir()
	write := func(pid int, session string) {
		data, _ := json.Marshal(Lease{SessionID: session, PID: pid, Expires: time.Now().Add(time.Minute).UnixMilli()})
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("attachment.%d", pid)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(os.Getpid(), "ses_first")
	write(child.Process.Pid, "ses_second")
	if active := Active(dir); len(active) != 2 || !active["ses_first"] || !active["ses_second"] {
		t.Fatalf("missing live owners: %v", active)
	}
	write(os.Getpid(), "ses_resumed")
	if active := Active(dir); len(active) != 2 || active["ses_first"] || !active["ses_resumed"] || !active["ses_second"] {
		t.Fatalf("switch changed other owner: %v", active)
	}
}

func TestFocusOwnerHelper(t *testing.T) {
	if os.Getenv("RA2A_TEST_FOCUS_OWNER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestFocusLeaseSwitchWithdrawsOldSessionAndExpires(t *testing.T) {
	directory := t.TempDir()
	path := FocusPath(directory)
	write := func(session string, expires int64) {
		t.Helper()
		data, _ := json.Marshal(Lease{SessionID: session, PID: os.Getpid(), Expires: expires})
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("ses_old", time.Now().Add(time.Second).UnixMilli())
	if !Active(directory)["ses_old"] || Current(directory) != "ses_old" {
		t.Fatal("focused session absent")
	}
	write("ses_resumed", time.Now().Add(time.Second).UnixMilli())
	active := Active(directory)
	if active["ses_old"] || !active["ses_resumed"] || Current(directory) != "ses_resumed" {
		t.Fatalf("wrong focus after resume: %v", active)
	}
	write("", time.Now().Add(time.Second).UnixMilli())
	if len(Active(directory)) != 0 || Current(directory) != "" {
		t.Fatal("home route published endpoint")
	}
	write("ses_resumed", time.Now().Add(-time.Second).UnixMilli())
	if len(Active(directory)) != 0 || Current(directory) != "" {
		t.Fatal("lost TUI tracking retained endpoint")
	}
	write("ses_resumed", 0)
	if len(Active(directory)) != 0 {
		t.Fatal("dynamic focus accepted without heartbeat")
	}
	if err := os.WriteFile(path, []byte(`{"sessionID":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(Active(directory)) != 0 {
		t.Fatal("partial record published endpoint")
	}
}
