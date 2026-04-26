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
	ev, ok := ParseEvent(started)
	if !ok || ev.Type != aime.EventToolCall || ev.ToolCall == nil || ev.ToolCall.Name != "apply_patch" {
		t.Fatalf("tool event = %#v, %v", ev, ok)
	}

	commandDone := rawJSON(t, `{"method":"item/completed","params":{"item":{"type":"commandExecution","command":"go test ./...","exitCode":0}}}`)
	ev, ok = ParseEvent(commandDone)
	if !ok || ev.Activity == nil || ev.Activity.Type != aime.ActivityCommand {
		t.Fatalf("command event = %#v, %v", ev, ok)
	}
	if got := ev.Activity.Parameters; len(got) != 2 || got[0] != "go test ./..." || got[1] != "0" {
		t.Fatalf("command params = %#v", got)
	}

	done := rawJSON(t, `{"method":"turn/completed","params":{"turn":{"status":"completed"}}}`)
	ev, ok = ParseEvent(done)
	if !ok || ev.Type != aime.EventDone {
		t.Fatalf("done event = %#v, %v", ev, ok)
	}
}
