package codexcli

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCheckManagedHostAcceptsHealthyHost(t *testing.T) {
	server := newFakeAppServer(t)
	home := t.TempDir()
	server.codexHome = home

	if err := CheckManagedHost(context.Background(), server.socketPath, home); err != nil {
		t.Fatalf("healthy host rejected: %v", err)
	}
}

func TestCheckManagedHostRejectsForeignCodexHome(t *testing.T) {
	server := newFakeAppServer(t)
	server.codexHome = t.TempDir()
	callerHome := t.TempDir()

	err := CheckManagedHost(context.Background(), server.socketPath, callerHome)
	if err == nil || !strings.Contains(err.Error(), "codex home") {
		t.Fatalf("expected codex home mismatch, got %v", err)
	}
}

func TestCheckManagedHostRejectsUnreachableBackend(t *testing.T) {
	server := newFakeAppServer(t)
	home := t.TempDir()
	server.codexHome = home
	// This is the observed failure of a host whose proxy environment is
	// missing: the socket accepts the session, but the account read cannot
	// reach the backend.
	server.accountReadErr = "failed to fetch codex rate limits: error sending request for url (https://chatgpt.com/backend-api/wham/usage)"

	err := CheckManagedHost(context.Background(), server.socketPath, home)
	if err == nil || !strings.Contains(err.Error(), "read account") {
		t.Fatalf("expected account read failure, got %v", err)
	}
}

func TestCheckManagedHostTimesOutOnSilentHost(t *testing.T) {
	server := newFakeAppServer(t)
	home := t.TempDir()
	server.codexHome = home
	server.accountReadSilent = true

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := CheckManagedHost(ctx, server.socketPath, home)
	if err == nil {
		t.Fatal("expected timeout error for a silent host")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("gate did not respect the caller deadline: %s", elapsed)
	}
}

func TestCheckManagedHostRejectsUnreachableSocket(t *testing.T) {
	if err := CheckManagedHost(context.Background(), "", ""); err == nil {
		t.Fatal("expected error for an empty socket path")
	}
	dead := newFakeAppServer(t)
	socketPath := dead.socketPath
	_ = dead.listener.Close()
	if err := CheckManagedHost(context.Background(), socketPath, t.TempDir()); err == nil {
		t.Fatal("expected error for an unreachable socket")
	}
}
