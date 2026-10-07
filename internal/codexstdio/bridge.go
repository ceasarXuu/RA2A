// Package codexstdio is an isolated single-client bridge prototype. It does not
// start, discover or resume backends, and is not wired into production routing.
package codexstdio

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

var ErrNotReady = errors.New("original app initialization has not completed")
var ErrClosed = errors.New("stdio bridge closed; receipt may be unknown")

type pending struct {
	original json.RawMessage
	method   string
	reply    chan map[string]json.RawMessage
}

type Bridge struct {
	appIn, nativeOut      io.ReadCloser
	appOut, nativeIn      io.WriteCloser
	appWrite, nativeWrite sync.Mutex
	mu                    sync.Mutex
	prefix                string
	sequence              uint64
	requests              map[string]pending
	initialized, ready    bool
	done                  chan struct{}
	closeOnce             sync.Once
}

// New takes ownership of four newly created streams; Close must unblock their IO.
// Never give it an existing Desktop process's descriptors.
func New(appInput io.ReadCloser, appOutput io.WriteCloser, nativeOutput io.ReadCloser, nativeInput io.WriteCloser) *Bridge {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic(err)
	}
	return &Bridge{appIn: appInput, appOut: appOutput, nativeOut: nativeOutput, nativeIn: nativeInput,
		prefix: "ra2a-" + hex.EncodeToString(nonce[:]) + "-", requests: make(map[string]pending), done: make(chan struct{})}
}

func (b *Bridge) Close() {
	b.closeOnce.Do(func() {
		close(b.done)
		_ = b.appIn.Close()
		_ = b.nativeOut.Close()
		_ = b.appOut.Close()
		_ = b.nativeIn.Close()
		b.mu.Lock()
		b.requests = make(map[string]pending)
		b.ready = false
		b.mu.Unlock()
	})
}

func (b *Bridge) Run(ctx context.Context) error {
	results := make(chan error, 2)
	go func() { results <- b.pump(ctx, b.appIn, b.fromApp) }()
	go func() { results <- b.pump(ctx, b.nativeOut, b.fromNative) }()
	var err error
	consumed := 0
	select {
	case err = <-results:
		consumed++
	case <-ctx.Done():
		err = ctx.Err()
	case <-b.done:
		err = ErrClosed
	}
	b.Close()
	for ; consumed < 2; consumed++ {
		<-results
	}
	return err
}

func (b *Bridge) pump(ctx context.Context, input io.Reader, relay func(context.Context, []byte) error) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		if err := relay(ctx, scanner.Bytes()); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func (b *Bridge) write(ctx context.Context, writer io.Writer, mutex *sync.Mutex, payload []byte) error {
	mutex.Lock()
	defer mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-b.done:
		return ErrClosed
	default:
	}
	data := append(append([]byte(nil), payload...), '\n')
	n, err := writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

func (b *Bridge) nextID(p pending) string {
	b.sequence++
	id := fmt.Sprintf("%s%d", b.prefix, b.sequence)
	b.requests[id] = p
	return id
}

func (b *Bridge) fromApp(ctx context.Context, payload []byte) error {
	var frame map[string]json.RawMessage
	if json.Unmarshal(payload, &frame) != nil {
		return b.write(ctx, b.nativeIn, &b.nativeWrite, payload)
	}
	var method string
	_ = json.Unmarshal(frame["method"], &method)
	if method != "" && len(frame["id"]) > 0 && string(frame["id"]) != "null" {
		b.mu.Lock()
		id := b.nextID(pending{original: append(json.RawMessage(nil), frame["id"]...), method: method})
		b.mu.Unlock()
		frame["id"], _ = json.Marshal(id)
		payload, _ = json.Marshal(frame)
	}
	if err := b.write(ctx, b.nativeIn, &b.nativeWrite, payload); err != nil {
		return err
	}
	if method == "initialized" {
		b.mu.Lock()
		b.ready = b.initialized
		b.mu.Unlock()
	}
	return nil
}

func (b *Bridge) fromNative(ctx context.Context, payload []byte) error {
	var frame map[string]json.RawMessage
	if json.Unmarshal(payload, &frame) != nil || len(frame["method"]) > 0 {
		return b.write(ctx, b.appOut, &b.appWrite, payload)
	}
	var id string
	if json.Unmarshal(frame["id"], &id) != nil {
		return b.write(ctx, b.appOut, &b.appWrite, payload)
	}
	b.mu.Lock()
	p, found := b.requests[id]
	delete(b.requests, id)
	if found && p.method == "initialize" && len(frame["result"]) > 0 && string(frame["result"]) != "null" && (len(frame["error"]) == 0 || string(frame["error"]) == "null") {
		b.initialized = true
	}
	b.mu.Unlock()
	if found && p.reply != nil {
		p.reply <- frame
		return nil
	}
	if found {
		frame["id"] = p.original
		payload, _ = json.Marshal(frame)
	} else if strings.HasPrefix(id, b.prefix) {
		// Late bridge responses stay private even after timeout; never replay.
		return nil
	}
	return b.write(ctx, b.appOut, &b.appWrite, payload)
}

// Call permits only queries and inputs to existing in-memory threads. Its result
// confirms native receipt, not execution. Cancellation after dispatch is unknown.
func (b *Bridge) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	switch method {
	case "thread/loaded/list", "thread/read", "thread/turns/list", "turn/start", "turn/steer":
	default:
		return nil, fmt.Errorf("bridge method is not permitted: %s", method)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-b.done:
		return nil, ErrClosed
	default:
	}
	b.mu.Lock()
	if !b.ready {
		b.mu.Unlock()
		return nil, ErrNotReady
	}
	reply := make(chan map[string]json.RawMessage, 1)
	id := b.nextID(pending{method: method, reply: reply})
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.requests, id); b.mu.Unlock() }()
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	written := make(chan error, 1)
	go func() { written <- b.write(ctx, b.nativeIn, &b.nativeWrite, payload) }()
	select {
	case err = <-written:
		if err != nil {
			// Cancellation before Write must not disconnect the original client.
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				b.Close()
			}
			return nil, err
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.done:
		return nil, ErrClosed
	}
	select {
	case frame := <-reply:
		if len(frame["error"]) > 0 && string(frame["error"]) != "null" {
			return nil, fmt.Errorf("native %s rejected: %s", method, frame["error"])
		}
		if len(frame["result"]) == 0 || string(frame["result"]) == "null" {
			return nil, errors.New("native response has no receipt")
		}
		return frame["result"], nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.done:
		return nil, ErrClosed
	}
}
