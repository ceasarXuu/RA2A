package mailbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "mailbox"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}

func TestParseAddressRecognisesOnlyMailboxNamespace(t *testing.T) {
	node, recipient, ok, err := ParseAddress(Address("node-a", "harness-1"))
	if !ok || err != nil {
		t.Fatalf("mailbox address must parse: ok=%v err=%v", ok, err)
	}
	if node != "node-a" || recipient != "harness-1" {
		t.Fatalf("unexpected parse: %q %q", node, recipient)
	}
	for _, address := range []string{
		"ra2a://node-a/thread-1",
		"ra2a://node-a",
		"http://node-a/mailbox/x",
		"ra2a://node-a/mailbox",
		"ra2a://node-a/inbox/x",
	} {
		if _, _, ok, _ := ParseAddress(address); ok {
			t.Fatalf("address %q must not be treated as a mailbox", address)
		}
	}
	if _, _, ok, err := ParseAddress("ra2a://node-a/mailbox/bad recipient"); !ok || err == nil {
		t.Fatalf("a mailbox address with an invalid recipient must report an error, got ok=%v err=%v", ok, err)
	}
}

func TestDeliverCostsNoAgentCallAndPersists(t *testing.T) {
	store := newStore(t)
	now := time.Now()
	if _, err := store.Deliver("harness-1", "ra2a://node-b/thread-9", "hello", "m1", now); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	reopened, err := OpenStore(store.Root())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	messages, pending, err := reopened.Read("harness-1", 10, false)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pending != 1 || len(messages) != 1 {
		t.Fatalf("expected one pending message, got pending=%d messages=%+v", pending, messages)
	}
	if messages[0].Text != "hello" || messages[0].From != "ra2a://node-b/thread-9" {
		t.Fatalf("message content must round trip, got %+v", messages[0])
	}
	if messages[0].Read {
		t.Fatal("a non-peek read must mark messages read")
	}
}

func TestReadIsIdempotentAndPeekDoesNotConsume(t *testing.T) {
	store := newStore(t)
	if _, err := store.Deliver("bob", "someone", "one", "m1", time.Now()); err != nil {
		t.Fatal(err)
	}
	peeking, pending, err := store.Read("bob", 10, true)
	if err != nil || pending != 1 || len(peeking) != 1 {
		t.Fatalf("peek must report without consuming: pending=%d err=%v", pending, err)
	}
	again, pending, err := store.Read("bob", 10, true)
	if err != nil || pending != 1 || len(again) != 1 {
		t.Fatalf("repeated peek must still see the message: pending=%d err=%v", pending, err)
	}
	consumed, pending, err := store.Read("bob", 10, false)
	if err != nil || pending != 1 || len(consumed) != 1 {
		t.Fatalf("consuming read failed: pending=%d err=%v", pending, err)
	}
	empty, pending, err := store.Read("bob", 10, false)
	if err != nil || pending != 0 || len(empty) != 0 {
		t.Fatalf("a consumed message must not reappear: pending=%d err=%v", pending, err)
	}
}

func TestRepeatedMessageIDIsIdempotent(t *testing.T) {
	store := newStore(t)
	now := time.Now()
	if _, err := store.Deliver("bob", "someone", "payload", "same-id", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Deliver("bob", "someone", "payload", "same-id", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	messages, pending, err := store.Read("bob", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 1 || len(messages) != 1 {
		t.Fatalf("a retried delivery must not double-post, got pending=%d messages=%+v", pending, messages)
	}
}

func TestDeliverRejectsUndeliverableMessages(t *testing.T) {
	store := newStore(t)
	cases := []struct {
		name      string
		recipient string
		text      string
		id        string
	}{
		{"bad recipient", "has space", "text", "m1"},
		{"empty recipient", "", "text", "m1"},
		{"empty text", "bob", "   ", "m1"},
		{"missing id", "bob", "text", ""},
	}
	for _, testCase := range cases {
		if _, err := store.Deliver(testCase.recipient, "x", testCase.text, testCase.id, time.Now()); err == nil {
			t.Fatalf("%s must be rejected", testCase.name)
		}
	}
	if _, err := store.Deliver("bob", "x", strings.Repeat("a", maxTextBytes+1), "m1", time.Now()); err == nil {
		t.Fatal("oversized text must be rejected")
	}
}

func TestRetentionDropsOldestBeyondLimit(t *testing.T) {
	store := newStore(t)
	now := time.Now()
	total := maxPerRecipient + 25
	for i := 0; i < total; i++ {
		if _, err := store.Deliver("bob", "x", "payload", fmt.Sprintf("m%d", i), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("deliver %d: %v", i, err)
		}
	}
	raw, err := readAll(store.path("bob"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != maxPerRecipient {
		t.Fatalf("store must be bounded, got %d", len(raw))
	}
	if raw[0].ID != "m25" {
		t.Fatalf("oldest messages must be dropped first, got first=%q", raw[0].ID)
	}
}

func TestRecipientsListsExistingBoxesOnly(t *testing.T) {
	store := newStore(t)
	if _, err := store.Deliver("bob", "x", "a", "m1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Deliver("alice", "x", "a", "m2", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.Root(), "stray"), 0o700); err != nil {
		t.Fatal(err)
	}
	names, err := store.Recipients()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "alice" || names[1] != "bob" {
		t.Fatalf("unexpected recipients: %+v", names)
	}
}

func TestStoreFilesArePrivateToTheUser(t *testing.T) {
	store := newStore(t)
	if _, err := store.Deliver("bob", "x", "secret", "m1", time.Now()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path("bob"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mailbox files must stay private, got %v", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("mailbox store must stay private, got %v", dirInfo.Mode().Perm())
	}
}
