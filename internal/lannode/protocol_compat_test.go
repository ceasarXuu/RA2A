package lannode

import (
	"encoding/json"
	"testing"
)

// These fixtures pin the wire contract in both directions so a mixed-version LAN
// never silently drops or mis-routes an endpoint.
const (
	legacySessionsJSON = `{"sessions":[` +
		`{"id":"thread-old","title":"legacy","status":"idle"},` +
		`{"id":"thread-old-2","title":"legacy busy","status":"active"}]}`

	versionedSessionsJSON = `{"protocolVersion":1,"sessions":[` +
		`{"id":"app-1","title":"codex app","status":"idle","agent":"codex-app",` +
		`"capabilities":["receiveText","replyAddress"]},` +
		`{"id":"cli-1","title":"codex cli","status":"busy","agent":"codex-cli",` +
		`"capabilities":["receiveText","steerActiveTurn"]}]}`
)

func TestReadLegacySessionsResponse(t *testing.T) {
	var response sessionsResponse
	if err := json.Unmarshal([]byte(legacySessionsJSON), &response); err != nil {
		t.Fatalf("a v0.0.14 peer response must still decode: %v", err)
	}
	if len(response.Sessions) != 2 {
		t.Fatalf("expected both legacy sessions, got %+v", response.Sessions)
	}
	if response.Sessions[0].ID != "thread-old" || response.Sessions[0].Status != "idle" {
		t.Fatalf("legacy fields must be preserved, got %+v", response.Sessions[0])
	}
	if response.Sessions[0].Agent != "" || response.Sessions[0].Capabilities != nil {
		t.Fatalf("absent agent metadata must stay absent, got %+v", response.Sessions[0])
	}
	if response.ProtocolVersion != 0 {
		t.Fatalf("a legacy peer reports no protocol version, got %d", response.ProtocolVersion)
	}
}

func TestReadVersionedSessionsResponse(t *testing.T) {
	var response sessionsResponse
	if err := json.Unmarshal([]byte(versionedSessionsJSON), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version must be reported, got %d", response.ProtocolVersion)
	}
	byID := map[string]Session{}
	for _, session := range response.Sessions {
		byID[session.ID] = session
	}
	if byID["cli-1"].Agent != "codex-cli" {
		t.Fatalf("agent type must survive the wire, got %+v", byID["cli-1"])
	}
	if len(byID["cli-1"].Capabilities) != 2 {
		t.Fatalf("capabilities must survive the wire, got %+v", byID["cli-1"].Capabilities)
	}
	if byID["app-1"].Status != "idle" || byID["cli-1"].Status != "busy" {
		t.Fatalf("status must survive the wire, got %+v", response.Sessions)
	}
}

// An older node parses our response with a struct that has no agent,
// capabilities or protocolVersion fields. Unknown fields must therefore be
// ignorable, otherwise a new node would break every old peer.
func TestVersionedResponseStaysParsableByLegacyShape(t *testing.T) {
	type legacySession struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	type legacyResponse struct {
		Sessions []legacySession `json:"sessions"`
	}
	payload, err := json.Marshal(sessionsResponse{
		ProtocolVersion: ProtocolVersion,
		Sessions: []Session{
			{ID: "app-1", Title: "codex app", Status: "idle", Agent: "codex-app",
				Capabilities: []string{"receiveText"}},
			{ID: "cli-1", Title: "codex cli", Status: "busy", Agent: "codex-cli",
				Capabilities: []string{"receiveText", "steerActiveTurn"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded legacyResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("a legacy peer must be able to parse our response: %v", err)
	}
	if len(decoded.Sessions) != 2 {
		t.Fatalf("legacy peer must see both sessions, got %+v", decoded.Sessions)
	}
	if decoded.Sessions[0].ID != "app-1" || decoded.Sessions[1].ID != "cli-1" {
		t.Fatalf("legacy peer must see stable ids, got %+v", decoded.Sessions)
	}
	if decoded.Sessions[1].Status != "busy" {
		t.Fatalf("legacy peer must still see the status field, got %+v", decoded.Sessions[1])
	}
}

// Unknown agent types and unknown capabilities coming from a future peer must
// not break an older node's parsing.
func TestUnknownAgentMetadataIsTolerated(t *testing.T) {
	future := `{"protocolVersion":2,"sessions":[` +
		`{"id":"x-1","title":"future","status":"idle","agent":"future-agent",` +
		`"capabilities":["receiveText","someFutureCapability"]}]}`
	var response sessionsResponse
	if err := json.Unmarshal([]byte(future), &response); err != nil {
		t.Fatalf("future metadata must not break decoding: %v", err)
	}
	if response.Sessions[0].Agent != "future-agent" {
		t.Fatalf("unknown agent type must be carried verbatim, got %+v", response.Sessions[0])
	}
	if len(response.Sessions[0].Capabilities) != 2 {
		t.Fatalf("unknown capability must be carried verbatim, got %+v", response.Sessions[0])
	}
	if response.ProtocolVersion != 2 {
		t.Fatalf("a future peer version must not be rewritten, got %d", response.ProtocolVersion)
	}
}

func TestMessageEnvelopeWireCompatibility(t *testing.T) {
	legacy := `{"targetSessionId":"thread-1","text":"hi","source":"ra2a://node/thread-0","messageId":"m1"}`
	var message Message
	if err := json.Unmarshal([]byte(legacy), &message); err != nil {
		t.Fatalf("a legacy message must still decode: %v", err)
	}
	if message.TargetSessionID != "thread-1" || message.MessageID != "m1" {
		t.Fatalf("legacy message fields must be preserved, got %+v", message)
	}
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped Message
	if err := json.Unmarshal(payload, &roundTripped); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if roundTripped != message {
		t.Fatalf("message must round trip unchanged: %+v vs %+v", roundTripped, message)
	}
}
