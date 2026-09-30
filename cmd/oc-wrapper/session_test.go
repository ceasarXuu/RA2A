package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

func TestSelectSessionPinsNewAndContinuedSession(t *testing.T) {
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var created bool
	expectedDirectory := directory
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("directory") != expectedDirectory {
			t.Errorf("wrong directory: %s", request.URL)
		}
		switch request.Method {
		case http.MethodPost:
			created = true
			_, _ = writer.Write([]byte(`{"id":"ses_new"}`))
		case http.MethodGet:
			_ = json.NewEncoder(writer).Encode([]ocsession.Session{
				{ID: "ses_old", Directory: directory},
				{ID: "ses_recent", Directory: directory, Time: struct {
					Updated int64 `json:"updated"`
				}{Updated: 42}},
			})
		}
	}))
	defer server.Close()
	ctx := context.Background()
	id, args, err := ocsession.Select(ctx, server.URL, []string{"--mini"})
	if err != nil || id != "ses_new" || !reflect.DeepEqual(args, []string{"--mini", "--session", "ses_new"}) || !created {
		t.Fatalf("new session: id=%q args=%v err=%v created=%v", id, args, err, created)
	}
	id, args, err = ocsession.Select(ctx, server.URL, []string{"--continue", "--mini"})
	if err != nil || id != "ses_recent" || !reflect.DeepEqual(args, []string{"--mini", "--session", "ses_recent"}) {
		t.Fatalf("continue: id=%q args=%v err=%v", id, args, err)
	}
	id, args, err = ocsession.Select(ctx, server.URL, []string{"--session", "ses_given"})
	if err != nil || id != "ses_given" || !reflect.DeepEqual(args, []string{"--session", "ses_given"}) {
		t.Fatalf("explicit: id=%q args=%v err=%v", id, args, err)
	}
	expectedDirectory = filepath.Join(directory, "project-demo")
	id, args, err = ocsession.Select(ctx, server.URL, []string{"project-demo", "--mini"})
	if err != nil || id != "ses_new" || !reflect.DeepEqual(args, []string{"--dir", expectedDirectory, "--mini", "--session", "ses_new"}) {
		t.Fatalf("project TUI: id=%q args=%v err=%v", id, args, err)
	}
}

func TestApproveRequestsOnlyAnswersAttachedSession(t *testing.T) {
	var mu sync.Mutex
	var answered []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/event" {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.Write([]byte("data: {\"type\":\"permission.asked\",\"properties\":{\"sessionID\":\"ses_other\",\"id\":\"per_other\"}}\n\n" +
				"data: {\"type\":\"permission.asked\",\"properties\":{\"sessionID\":\"ses_attached\",\"id\":\"per_expected\"}}\n\n" +
				"data: {\"type\":\"permission.v2.asked\",\"properties\":{\"sessionID\":\"ses_attached\",\"id\":\"per_v2\"}}\n\n"))
			writer.(http.Flusher).Flush()
			<-request.Context().Done()
			return
		}
		mu.Lock()
		answered = append(answered, request.URL.Path)
		mu.Unlock()
		var payload map[string]any
		_ = json.NewDecoder(request.Body).Decode(&payload)
		if payload["reply"] != "once" {
			t.Errorf("unexpected permission reply: %v", payload)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan error, 1)
	go approveRequests(ctx, server.URL, "ses_attached", ready, &strings.Builder{})
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := append([]string(nil), answered...)
		mu.Unlock()
		if len(got) >= 2 {
			cancel()
			if !reflect.DeepEqual(got, []string{"/permission/per_expected/reply", "/api/session/ses_attached/permission/per_v2/reply"}) {
				t.Fatalf("wrong sessions approved: %v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	t.Fatal("attached session permission was not approved")
}
