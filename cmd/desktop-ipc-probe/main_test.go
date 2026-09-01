package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/desktopipc"
)

type fakeStarter struct {
	threadID string
	text     string
	message  string
}

func (fake *fakeStarter) StartTurn(
	_ context.Context,
	threadID string,
	text string,
	messageID string,
) (desktopipc.TurnResult, error) {
	fake.threadID = threadID
	fake.text = text
	fake.message = messageID
	return desktopipc.TurnResult{TurnID: "turn-1"}, nil
}

func TestRunRequiresExplicitWritePermission(t *testing.T) {
	err := run(context.Background(), &fakeStarter{}, options{
		threadID: "thread-1",
		message:  "hello",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "allow-write") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunStartsDesktopOwnedTurn(t *testing.T) {
	starter := &fakeStarter{}
	var output bytes.Buffer
	err := run(context.Background(), starter, options{
		threadID:   "thread-1",
		message:    "hello",
		messageID:  "message-1",
		allowWrite: true,
	}, &output)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if starter.threadID != "thread-1" || starter.text != "hello" || starter.message != "message-1" {
		t.Fatalf("starter = %#v", starter)
	}
	if !strings.Contains(output.String(), `"turnId": "turn-1"`) {
		t.Fatalf("output = %s", output.String())
	}
}
