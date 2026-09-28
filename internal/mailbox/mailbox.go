// Package mailbox provides an agent-agnostic, model-call-free message channel
// between RA2A nodes.
//
// The delivery model of an adapted agent always costs one model turn: a message
// becomes a turn inside a live agent. That makes it unusable as a coordination
// channel, and it makes it impossible for a harness or an un-adapted agent to
// participate in the mesh at all. A mailbox is node infrastructure rather than
// an adapter: it has no conversation semantics, no ownership and no model call.
//
// Trust model, stated plainly: a peer node can write into this node's mailbox,
// but only this node can read it out. Reading requires the same loopback control
// access that delivery already requires. This is not a new security boundary.
package mailbox

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// Segment marks the address namespace reserved for mailboxes.
	Segment = "mailbox"
	// maxPerRecipient bounds on-disk growth; the oldest messages are dropped.
	maxPerRecipient = 500
	// maxTextBytes bounds a single message body.
	maxTextBytes = 64 * 1024
)

var recipientPattern = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,128}$`)

var (
	ErrBadAddress  = errors.New("malformed mailbox address")
	ErrBadMessage  = errors.New("mailbox message is not deliverable")
	ErrUnknownBox  = errors.New("no such mailbox")
	ErrMailboxFull = errors.New("mailbox is full")
)

// Message is one stored delivery. It records who sent it so a recipient can
// attribute replies without the mailbox knowing anything about either agent.
type Message struct {
	ID         string    `json:"id"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Text       string    `json:"text"`
	SentAt     time.Time `json:"sentAt"`
	Read       bool      `json:"read"`
	ReadAt     time.Time `json:"readAt,omitempty"`
	SourceNode string    `json:"sourceNode,omitempty"`
}

type persisted struct {
	Message
	ReadAtUnix int64 `json:"readAtUnix,omitempty"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

func OpenStore(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("mailbox store root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create mailbox store: %w", err)
	}
	return &Store{root: root}, nil
}

func (store *Store) Root() string { return store.root }

// Address builds the opaque mailbox address for a recipient on a node.
func Address(nodeID, recipient string) string {
	return "ra2a://" + nodeID + "/" + Segment + "/" + recipient
}

// ParseAddress recognises mailbox addresses. The second return value reports
// whether the address belongs to this namespace at all, so callers can fall
// through to normal endpoint routing without parsing the rest of the address.
func ParseAddress(address string) (nodeID, recipient string, ok bool, err error) {
	if !strings.HasPrefix(address, "ra2a://") {
		return "", "", false, nil
	}
	rest := strings.TrimPrefix(address, "ra2a://")
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return "", "", false, nil
	}
	if parts[0] == "" || parts[1] != Segment {
		return "", "", false, nil
	}
	if !recipientPattern.MatchString(parts[2]) {
		return "", "", true, ErrBadAddress
	}
	return parts[0], parts[2], true, nil
}

func ValidRecipient(recipient string) bool { return recipientPattern.MatchString(recipient) }

// Deliver stores one message. It performs no agent call of any kind.
func (store *Store) Deliver(recipient, from, text, id string, now time.Time) (Message, error) {
	if !ValidRecipient(recipient) {
		return Message{}, fmt.Errorf("%w: invalid recipient", ErrBadMessage)
	}
	if strings.TrimSpace(text) == "" {
		return Message{}, ErrBadMessage
	}
	if len(text) > maxTextBytes {
		return Message{}, fmt.Errorf("%w: text exceeds %d bytes", ErrBadMessage, maxTextBytes)
	}
	if id == "" {
		return Message{}, fmt.Errorf("%w: message id is required", ErrBadMessage)
	}
	if from == "" {
		from = "unknown"
	}
	message := Message{ID: id, From: from, To: recipient, Text: text, SentAt: now.UTC()}

	store.mu.Lock()
	defer store.mu.Unlock()
	path := store.path(recipient)
	existing, err := readAll(path)
	if err != nil {
		return Message{}, err
	}
	// A repeated message id is an idempotent re-delivery, not a second message:
	// a sender that retries after an uncertain result must not double-post.
	for _, previous := range existing {
		if previous.ID == message.ID {
			return previous.Message, nil
		}
	}
	updated := append(existing, persisted{Message: message})
	if len(updated) > maxPerRecipient {
		updated = updated[len(updated)-maxPerRecipient:]
	}
	if err := writeAll(path, updated); err != nil {
		return Message{}, err
	}
	return message, nil
}

// Read returns up to limit messages for a recipient. Unless peek is set the
// returned messages are marked read, so an agent can poll without reprocessing.
func (store *Store) Read(recipient string, limit int, peek bool) ([]Message, int, error) {
	if !ValidRecipient(recipient) {
		return nil, 0, ErrBadAddress
	}
	if limit <= 0 || limit > maxPerRecipient {
		limit = 50
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	path := store.path(recipient)
	messages, err := readAll(path)
	if err != nil {
		return nil, 0, err
	}
	pending := 0
	for _, stored := range messages {
		if !stored.Read {
			pending++
		}
	}
	if peek || pending == 0 {
		unread := unreadWithin(messages, limit)
		return unread, pending, nil
	}
	// Capture the batch before it is marked read: a consuming read must return
	// exactly the messages it consumed.
	consumed := unreadWithin(messages, limit)
	now := time.Now().UTC()
	updated := make([]persisted, 0, len(messages))
	delivered := make(map[string]bool, len(consumed))
	for _, message := range consumed {
		delivered[message.ID] = true
	}
	for _, stored := range messages {
		if !stored.Read && delivered[stored.ID] {
			stored.Read = true
			stored.ReadAt = now
			stored.ReadAtUnix = now.UnixNano()
		}
		updated = append(updated, stored)
	}
	if err := writeAll(path, updated); err != nil {
		return nil, 0, err
	}
	return consumed, pending, nil
}

// Recipients lists the mailboxes that exist on this node.
func (store *Store) Recipients() ([]string, error) {
	entries, err := os.ReadDir(store.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".jsonl"))
	}
	sort.Strings(names)
	return names, nil
}

func unreadWithin(messages []persisted, limit int) []Message {
	var unread []Message
	for i := len(messages) - 1; i >= 0 && len(unread) < limit; i-- {
		if messages[i].Read {
			continue
		}
		unread = append(unread, messages[i].Message)
	}
	// Return oldest first so a recipient processes messages in arrival order.
	for left, right := 0, len(unread)-1; left < right; left, right = left+1, right-1 {
		unread[left], unread[right] = unread[right], unread[left]
	}
	return unread
}

func (store *Store) path(recipient string) string {
	return filepath.Join(store.root, recipient+".jsonl")
}

func readAll(path string) ([]persisted, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open mailbox: %w", err)
	}
	defer file.Close()
	var messages []persisted
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxTextBytes+4096)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var stored persisted
		if err := json.Unmarshal(line, &stored); err != nil {
			continue
		}
		if stored.ReadAtUnix > 0 {
			stored.ReadAt = time.Unix(0, stored.ReadAtUnix).UTC()
		}
		messages = append(messages, stored)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read mailbox: %w", err)
	}
	return messages, nil
}

func writeAll(path string, messages []persisted) error {
	temporary := path + ".new"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("write mailbox: %w", err)
	}
	encoder := json.NewEncoder(file)
	for _, message := range messages {
		if err := encoder.Encode(message); err != nil {
			_ = file.Close()
			_ = os.Remove(temporary)
			return fmt.Errorf("encode mailbox message: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("close mailbox: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace mailbox: %w", err)
	}
	return nil
}
