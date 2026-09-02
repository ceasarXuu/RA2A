package appserverproxyprobe

import (
	"encoding/json"
	"testing"
)

func TestObserverCorrelatesClientWithStartedThread(t *testing.T) {
	observer := NewObserver()

	if event := observer.ObserveClient([]byte(`{"id":0,"method":"initialize","params":{"clientInfo":{"name":"codex_cli_rs","title":"Codex CLI","version":"0.152.1"}}}`)); event != nil {
		t.Fatalf("initialize event = %#v", event)
	}
	if event := observer.ObserveClient([]byte(`{"id":7,"method":"thread/start","params":{"threadSource":"user"}}`)); event != nil {
		t.Fatalf("request event = %#v", event)
	}
	event := observer.ObserveServer([]byte(`{"id":7,"result":{"thread":{"id":"thread-1","source":"vscode"}}}`))
	if event == nil {
		t.Fatal("expected correlated event")
	}
	if event.Client.Name != "codex_cli_rs" || event.Method != "thread/start" || event.ThreadID != "thread-1" {
		t.Fatalf("event = %#v", event)
	}
	if event.NativeSource != "vscode" {
		t.Fatalf("native source = %q", event.NativeSource)
	}
}

func TestObserverCorrelatesStringRequestID(t *testing.T) {
	observer := NewObserver()
	observer.ObserveClient([]byte(`{"id":"resume-1","method":"thread/resume","params":{"threadId":"thread-2"}}`))

	event := observer.ObserveServer([]byte(`{"id":"resume-1","result":{"thread":{"id":"thread-2","source":"vscode"}}}`))
	if event == nil || event.Method != "thread/resume" || event.ThreadID != "thread-2" {
		t.Fatalf("event = %#v", event)
	}
}

func TestObserverPreservesSafeThreadStartMetadata(t *testing.T) {
	observer := NewObserver()
	observer.ObserveClient([]byte(`{"id":7,"method":"thread/start","params":{"ephemeral":true,"historyMode":"paginated","dynamicTools":[{"name":"task"}],"input":[{"text":"secret"}]}}`))

	event := observer.ObserveServer([]byte(`{"id":7,"result":{"thread":{"id":"thread-1","source":"vscode"}}}`))
	if event == nil || event.Ephemeral == nil || !*event.Ephemeral || !event.HasDynamicTools || event.HistoryMode != "paginated" {
		t.Fatalf("event = %#v", event)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"client":{},"method":"thread/start","threadId":"thread-1","nativeSource":"vscode","ephemeral":true,"hasDynamicTools":true,"historyMode":"paginated"}` {
		t.Fatalf("payload = %s", payload)
	}
}

func TestObserverMarksThreadUsedByClientWithoutRecordingInput(t *testing.T) {
	for _, method := range []string{"turn/start", "turn/steer", "thread/queue/add", "thread/queue/start"} {
		t.Run(method, func(t *testing.T) {
			observer := NewObserver()
			observer.ObserveClient([]byte(`{"id":0,"method":"initialize","params":{"clientInfo":{"name":"codex-tui"}}}`))

			event := observer.ObserveClient([]byte(`{"id":9,"method":"` + method + `","params":{"threadId":"thread-1","input":[{"type":"text","text":"secret"}]}}`))
			if event == nil {
				t.Fatal("expected thread ownership event")
			}
			if event.Client.Name != "codex-tui" || event.Method != method || event.ThreadID != "thread-1" {
				t.Fatalf("event = %#v", event)
			}
			payload, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			want := `{"client":{"name":"codex-tui"},"method":"` + method + `","threadId":"thread-1"}`
			if string(payload) != want {
				t.Fatalf("payload = %s", payload)
			}
		})
	}
}

func TestObserverIgnoresUnrelatedAndInvalidMessages(t *testing.T) {
	observer := NewObserver()
	for _, payload := range [][]byte{
		[]byte(`not-json`),
		[]byte(`{"id":1,"method":"turn/start","params":{}}`),
		[]byte(`{"method":"thread/started","params":{"thread":{"id":"broadcast"}}}`),
	} {
		if event := observer.ObserveClient(payload); event != nil {
			t.Fatalf("client event for %q = %#v", payload, event)
		}
		if event := observer.ObserveServer(payload); event != nil {
			t.Fatalf("server event for %q = %#v", payload, event)
		}
	}
}

func TestObserverReportsTurnRequestShapeWithoutValues(t *testing.T) {
	observer := NewObserver()
	observer.ObserveClient([]byte(`{"id":0,"method":"initialize","params":{"clientInfo":{"name":"codex-tui"}}}`))
	event := observer.ObserveClientShape([]byte(`{"id":9,"method":"turn/start","params":{"thread_id":"thread-1","input":[{"text":"secret"}]}}`))
	if event == nil || event.Method != "turn/start" {
		t.Fatalf("event = %#v", event)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"client":{"name":"codex-tui"},"method":"turn/start","threadId":"","paramKeys":["input","thread_id"]}` {
		t.Fatalf("payload = %s", payload)
	}
}

func TestEventMarshalsWithoutMessageContents(t *testing.T) {
	event := Event{
		Client: ClientInfo{Name: "codex_cli_rs", Title: "Codex CLI", Version: "0.152.1"},
		Method: "thread/start", ThreadID: "thread-1", NativeSource: "vscode",
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"client":{"name":"codex_cli_rs","title":"Codex CLI","version":"0.152.1"},"method":"thread/start","threadId":"thread-1","nativeSource":"vscode"}`
	if string(payload) != want {
		t.Fatalf("payload = %s", payload)
	}
}
