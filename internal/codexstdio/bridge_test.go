package codexstdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

type fixture struct {
	b                       *Bridge
	app, native             io.WriteCloser
	appFrames, nativeFrames chan map[string]json.RawMessage
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	appIn, app := io.Pipe()
	appRead, appOut := io.Pipe()
	nativeOut, native := io.Pipe()
	nativeRead, nativeIn := io.Pipe()
	f := &fixture{b: New(appIn, appOut, nativeOut, nativeIn), app: app, native: native,
		appFrames: make(chan map[string]json.RawMessage, 32), nativeFrames: make(chan map[string]json.RawMessage, 32)}
	pump := func(r io.Reader, frames chan map[string]json.RawMessage) {
		defer close(frames)
		s := bufio.NewScanner(r)
		for s.Scan() {
			var frame map[string]json.RawMessage
			if json.Unmarshal(s.Bytes(), &frame) == nil {
				frames <- frame
			}
		}
	}
	go pump(appRead, f.appFrames)
	go pump(nativeRead, f.nativeFrames)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.b.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		f.b.Close()
		_ = app.Close()
		_ = native.Close()
		_ = appRead.Close()
		_ = nativeRead.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("bridge did not terminate")
		}
	})
	return f
}

func frame(t *testing.T, source <-chan map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	select {
	case message, ok := <-source:
		if !ok {
			t.Fatal("stream closed")
		}
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("missing protocol frame")
	}
	return nil
}

func send(t *testing.T, output io.Writer, message any) {
	t.Helper()
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = output.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) initialize(t *testing.T) {
	t.Helper()
	params := map[string]any{"clientInfo": map[string]any{"name": "fake-app", "version": "fixture"}, "capabilities": map[string]any{"experimentalApi": true, "optOutNotificationMethods": []string{"irrelevant"}}}
	send(t, f.app, map[string]any{"id": 17, "method": "initialize", "params": params})
	init := frame(t, f.nativeFrames)
	var actual any
	_ = json.Unmarshal(init["params"], &actual)
	expected, _ := json.Marshal(params)
	var want any
	_ = json.Unmarshal(expected, &want)
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("initialization capabilities changed")
	}
	if string(init["id"]) == "17" {
		t.Fatal("app request ID not separated")
	}
	send(t, f.native, map[string]any{"id": init["id"], "result": map[string]any{"codexHome": "fixture"}})
	if string(frame(t, f.appFrames)["id"]) != "17" {
		t.Fatal("app response lost original ID")
	}
	send(t, f.app, map[string]any{"method": "initialized", "params": map[string]any{}})
	if string(frame(t, f.nativeFrames)["method"]) != `"initialized"` {
		t.Fatal("original initialized notification lost")
	}
	deadline := time.Now().Add(time.Second)
	for {
		f.b.mu.Lock()
		ready := f.b.ready
		f.b.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge not ready after original handshake")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestOriginalHandshakeOwnIDsAndServerRequestRemainIndependent(t *testing.T) {
	f := newFixture(t)
	f.initialize(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		r, err := f.b.Call(ctx, "turn/start", map[string]any{"threadId": "existing", "input": []any{}})
		if err == nil && string(r) != `{"turn":{"id":"accepted"}}` {
			err = errors.New("wrong native receipt")
		}
		result <- err
	}()
	own := frame(t, f.nativeFrames)
	// The App can choose an ID identical to a bridge request's wire ID.
	send(t, f.app, map[string]any{"id": own["id"], "method": "thread/read", "params": map[string]any{"threadId": "existing"}})
	appRequest := frame(t, f.nativeFrames)
	if string(appRequest["id"]) == string(own["id"]) {
		t.Fatal("request namespaces collided")
	}
	// Bidirectional JSON-RPC IDs may overlap; a method identifies a server request.
	send(t, f.native, map[string]any{"id": own["id"], "method": "item/commandExecution/requestApproval", "params": map[string]any{"command": "fixture"}})
	approval := frame(t, f.appFrames)
	if string(approval["method"]) != `"item/commandExecution/requestApproval"` {
		t.Fatal("approval swallowed as bridge response")
	}
	send(t, f.app, map[string]any{"id": approval["id"], "result": map[string]any{"decision": "accept"}})
	answer := frame(t, f.nativeFrames)
	if string(answer["id"]) != string(approval["id"]) {
		t.Fatal("server response ID changed")
	}
	send(t, f.native, map[string]any{"id": appRequest["id"], "result": map[string]any{"thread": "original"}})
	appReply := frame(t, f.appFrames)
	if string(appReply["id"]) != string(own["id"]) {
		t.Fatal("original App ID not restored")
	}
	send(t, f.native, map[string]any{"id": own["id"], "result": map[string]any{"turn": map[string]any{"id": "accepted"}}})
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	send(t, f.native, map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "existing"}})
	if string(frame(t, f.appFrames)["method"]) != `"turn/started"` {
		t.Fatal("notification not forwarded")
	}
}

func TestNoSecondInitializeAndNoBridgeResumeOrStartThread(t *testing.T) {
	f := newFixture(t)
	if _, err := f.b.Call(context.Background(), "turn/start", nil); !errors.Is(err, ErrNotReady) {
		t.Fatalf("pre-initialization input: %v", err)
	}
	f.initialize(t)
	for _, method := range []string{"initialize", "initialized", "thread/start", "thread/resume", "thread/fork", "thread/inject_items"} {
		if _, err := f.b.Call(context.Background(), method, nil); err == nil {
			t.Fatalf("permitted %s", method)
		}
	}
	select {
	case f := <-f.nativeFrames:
		t.Fatalf("forbidden method reached native: %+v", f)
	default:
	}
}

func TestLostReceiptDoesNotReplayOrLeakLateResponse(t *testing.T) {
	f := newFixture(t)
	f.initialize(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.b.Call(ctx, "turn/steer", map[string]any{"threadId": "existing", "expectedTurnId": "active"})
		done <- err
	}()
	request := frame(t, f.nativeFrames)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost receipt: %v", err)
	}
	send(t, f.native, map[string]any{"id": request["id"], "result": map[string]any{"turnId": "active"}})
	send(t, f.native, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "existing"}})
	if string(frame(t, f.appFrames)["method"]) != `"item/started"` {
		t.Fatal("late bridge response leaked to App")
	}
	select {
	case f := <-f.nativeFrames:
		t.Fatalf("unknown request replayed: %+v", f)
	default:
	}
}

func TestRejectedOrMalformedReceiptAndClosedBackend(t *testing.T) {
	for _, value := range []any{map[string]any{"error": map[string]any{"code": -32600, "message": "thread not loaded"}}, map[string]any{}, map[string]any{"result": nil}} {
		f := newFixture(t)
		f.initialize(t)
		done := make(chan error, 1)
		go func() {
			_, err := f.b.Call(context.Background(), "turn/start", map[string]any{"threadId": "unloaded"})
			done <- err
		}()
		request := frame(t, f.nativeFrames)
		reply := value.(map[string]any)
		reply["id"] = request["id"]
		send(t, f.native, reply)
		if err := <-done; err == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
	f := newFixture(t)
	f.initialize(t)
	done := make(chan error, 1)
	go func() { _, err := f.b.Call(context.Background(), "turn/start", nil); done <- err }()
	frame(t, f.nativeFrames)
	_ = f.native.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("closed backend: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("closed backend left receipt pending")
	}
	if _, err := f.b.Call(context.Background(), "turn/start", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("input after closure: %v", err)
	}
}

func TestCancellationWhileNativeWriteIsBlocked(t *testing.T) {
	f := newFixture(t)
	f.initialize(t)
	f.b.nativeWrite.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := f.b.Call(ctx, "turn/start", nil)
	f.b.nativeWrite.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write cancellation: %v", err)
	}
	// A later App request proves the writer lock has drained the canceled writer.
	send(t, f.app, map[string]any{"id": 99, "method": "thread/read", "params": map[string]any{}})
	request := frame(t, f.nativeFrames)
	if string(request["method"]) != `"thread/read"` {
		t.Fatal("canceled queued write reached native")
	}
}
