package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutoApprovalFollowsFocusAndIgnoresPreviousSession(t *testing.T) {
	var focus atomic.Value
	focus.Store("ses_old")
	send := make(chan string)
	answered := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/event" {
			answered <- r.URL.Path
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		for {
			select {
			case event := <-send:
				fmt.Fprintf(w, "data: %s\n\n", event)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan error, 1)
	go approveCurrentRequests(ctx, server.URL, func() string { return focus.Load().(string) }, ready, &strings.Builder{})
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	focus.Store("ses_resumed")
	send <- `{"type":"permission.asked","properties":{"sessionID":"ses_old","id":"old"}}`
	send <- `{"type":"permission.v2.asked","properties":{"sessionID":"ses_resumed","id":"new"}}`
	select {
	case path := <-answered:
		if path != "/api/session/ses_resumed/permission/new/reply" {
			t.Fatalf("approved wrong session: %s", path)
		}
	case <-time.After(time.Second):
		t.Fatal("resumed session permission not answered")
	}
	focus.Store("")
	send <- `{"type":"permission.asked","properties":{"sessionID":"ses_resumed","id":"unfocused"}}`
	select {
	case path := <-answered:
		t.Fatalf("unfocused session auto-approved: %s", path)
	case <-time.After(30 * time.Millisecond):
	}
}
