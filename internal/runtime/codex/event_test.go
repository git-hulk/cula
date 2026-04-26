package codex

import (
	"encoding/json"
	"testing"

	aime "github.com/git-hulk/aime/pkg"
)

func rawJSON(t *testing.T, s string) json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return raw
}

func TestParseEventStructuredActivity(t *testing.T) {
	started := rawJSON(t, `{"method":"item/started","params":{"item":{"id":"call-1","type":"functionCall","name":"apply_patch","arguments":"{\"path\":\"types.go\"}"}}}`)
	events := ParseEvent(started)
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(events), events)
	}
	if events[0].Activity == nil || events[0].Activity.Type != aime.ActivityToolCall {
		t.Fatalf("activity = %#v", events[0].Activity)
	}
	if got := events[0].Activity.Parameters; len(got) != 2 || got[0] != "apply_patch" || got[1] != "types.go" {
		t.Fatalf("activity params = %#v", got)
	}
	if events[1].ToolCall == nil || events[1].ToolCall.Name != "apply_patch" {
		t.Fatalf("tool call = %#v", events[1].ToolCall)
	}

	commandDone := rawJSON(t, `{"method":"item/completed","params":{"item":{"type":"commandExecution","command":"go test ./...","exitCode":0}}}`)
	events = ParseEvent(commandDone)
	if len(events) != 1 || events[0].Activity == nil || events[0].Activity.Type != aime.ActivityCommand {
		t.Fatalf("command events = %#v", events)
	}
	if got := events[0].Activity.Parameters; len(got) != 2 || got[0] != "go test ./..." || got[1] != "0" {
		t.Fatalf("command params = %#v", got)
	}

	done := rawJSON(t, `{"method":"turn/completed","params":{"turn":{"status":"completed"}}}`)
	events = ParseEvent(done)
	if len(events) != 1 || events[0].Type != aime.EventDone {
		t.Fatalf("done events = %#v", events)
	}
}
