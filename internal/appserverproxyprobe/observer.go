package appserverproxyprobe

import (
	"bytes"
	"encoding/json"
	"sort"
	"sync"
)

type ClientInfo struct {
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

type Event struct {
	Client          ClientInfo `json:"client"`
	Method          string     `json:"method"`
	ThreadID        string     `json:"threadId"`
	NativeSource    string     `json:"nativeSource,omitempty"`
	Ephemeral       *bool      `json:"ephemeral,omitempty"`
	HasDynamicTools bool       `json:"hasDynamicTools,omitempty"`
	HistoryMode     string     `json:"historyMode,omitempty"`
	ParamKeys       []string   `json:"paramKeys,omitempty"`
}

type pendingRequest struct {
	method          string
	ephemeral       *bool
	hasDynamicTools bool
	historyMode     string
}

type Observer struct {
	mu      sync.Mutex
	client  ClientInfo
	pending map[string]pendingRequest
}

func NewObserver() *Observer {
	return &Observer{pending: make(map[string]pendingRequest)}
}

func (observer *Observer) ObserveClient(payload []byte) *Event {
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ClientInfo   ClientInfo      `json:"clientInfo"`
			ThreadID     string          `json:"threadId"`
			Ephemeral    *bool           `json:"ephemeral"`
			DynamicTools json.RawMessage `json:"dynamicTools"`
			HistoryMode  string          `json:"historyMode"`
		} `json:"params"`
	}
	if json.Unmarshal(payload, &message) != nil {
		return nil
	}

	observer.mu.Lock()
	defer observer.mu.Unlock()
	if message.Method == "initialize" {
		observer.client = message.Params.ClientInfo
		return nil
	}
	if isThreadUseMethod(message.Method) && message.Params.ThreadID != "" {
		return &Event{Client: observer.client, Method: message.Method, ThreadID: message.Params.ThreadID}
	}
	if !isThreadOwnershipMethod(message.Method) || len(message.ID) == 0 {
		return nil
	}
	observer.pending[requestKey(message.ID)] = pendingRequest{
		method:          message.Method,
		ephemeral:       message.Params.Ephemeral,
		hasDynamicTools: len(message.Params.DynamicTools) > 0 && string(message.Params.DynamicTools) != "null",
		historyMode:     message.Params.HistoryMode,
	}
	return nil
}

func (observer *Observer) ObserveServer(payload []byte) *Event {
	var message struct {
		ID     json.RawMessage `json:"id"`
		Result struct {
			Thread struct {
				ID     string `json:"id"`
				Source string `json:"source"`
			} `json:"thread"`
		} `json:"result"`
	}
	if json.Unmarshal(payload, &message) != nil || len(message.ID) == 0 {
		return nil
	}

	observer.mu.Lock()
	defer observer.mu.Unlock()
	key := requestKey(message.ID)
	pending, ok := observer.pending[key]
	if !ok {
		return nil
	}
	delete(observer.pending, key)
	if message.Result.Thread.ID == "" {
		return nil
	}
	return &Event{
		Client:          observer.client,
		Method:          pending.method,
		ThreadID:        message.Result.Thread.ID,
		NativeSource:    message.Result.Thread.Source,
		Ephemeral:       pending.ephemeral,
		HasDynamicTools: pending.hasDynamicTools,
		HistoryMode:     pending.historyMode,
	}
}

func (observer *Observer) ObserveClientShape(payload []byte) *Event {
	var message struct {
		Method string                     `json:"method"`
		Params map[string]json.RawMessage `json:"params"`
	}
	if json.Unmarshal(payload, &message) != nil || !isThreadUseMethod(message.Method) {
		return nil
	}
	keys := make([]string, 0, len(message.Params))
	for key := range message.Params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return &Event{Client: observer.client, Method: message.Method, ParamKeys: keys}
}

func isThreadOwnershipMethod(method string) bool {
	switch method {
	case "thread/start", "thread/resume", "thread/fork":
		return true
	default:
		return false
	}
}

func isThreadUseMethod(method string) bool {
	switch method {
	case "turn/start", "turn/steer", "thread/queue/add", "thread/queue/start":
		return true
	default:
		return false
	}
}

func requestKey(id json.RawMessage) string {
	return string(bytes.TrimSpace(id))
}
